package manager

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis-entraid/internal"
	"github.com/redis/go-redis-entraid/shared"
	"github.com/redis/go-redis-entraid/token"
)

const RefreshRationPrecision = 10000

// entraidTokenManager is a struct that implements the TokenManager interface.
type entraidTokenManager struct {
	// idp is the identity provider used to obtain the token.
	idp shared.IdentityProvider

	// token is the authentication token for the user which should be kept in memory if valid.
	token *token.Token

	// tokenRWLock is a read-write lock used to protect the token from concurrent access.
	tokenRWLock *sync.RWMutex

	// identityProviderResponseParser is the parser used to parse the response from the identity provider.
	// It`s ParseResponse method will be called to parse the response and return the token.
	identityProviderResponseParser shared.IdentityProviderResponseParser

	// retryOptions is a struct that contains the options for retrying the token request.
	// It contains the maximum number of attempts, initial delay, maximum delay, and backoff multiplier.
	// The default values are 3 attempts, 1000 ms initial delay, 10000 ms maximum delay, and 2.0 backoff multiplier.
	// The values can be overridden by the user.
	retryOptions RetryOptions

	// listener is the single listener for the token manager.
	// It is used to receive updates from the token manager.
	// The token manager will call the listener's OnNext method with the updated token.
	// If an error occurs, the token manager will call the listener's OnError method with the error.
	// if listener is set, Start will fail
	listener TokenListener

	// startGate serializes Start/Stop while allowing context-aware startup waits.
	startGate chan struct{}

	// expirationRefreshRatio is the ratio of the token expiration time to refresh the token.
	// It is used to determine when to refresh the token.
	// The value should be between 0 and 1.
	// For example, if the expiration time is 1 hour and the ratio is 0.75,
	// the token will be refreshed after 45 minutes. (the token is refreshed when 75% of its lifetime has passed)
	expirationRefreshRatio float64

	// lowerBoundDuration is the lower bound for the refresh time in time.Duration.
	lowerBoundDuration time.Duration

	// closedChan is a channel that is closedChan when the token manager is closedChan.
	// It is used to signal the token manager to stop requesting tokens.
	closedChan chan struct{}

	// ctxMu protects the lifecycle context snapshot used by acquisition workers.
	ctxMu      sync.RWMutex
	ctx        context.Context
	ctxCancel  context.CancelFunc
	generation uint64

	// requestTimeout is the timeout for the request to the identity provider.
	requestTimeout time.Duration

	acquireMu sync.Mutex
	acquire   *tokenAcquisition
}

type tokenAcquisition struct {
	done       chan struct{}
	token      *token.Token
	current    *token.Token // valid cached token when the request failed
	err        error
	force      bool
	generation uint64
}

// GetTokenWithContext lets a caller stop waiting for a shared token acquisition.
// Incompatible refresh intent and lifecycle generations wait for the single
// active worker to finish, then start their own eligible acquisition.
func (e *entraidTokenManager) GetTokenWithContext(ctx context.Context, forceRefresh bool) (*token.Token, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		e.ctxMu.RLock()
		generation := e.generation
		e.ctxMu.RUnlock()
		e.acquireMu.Lock()
		flight := e.acquire
		if flight == nil {
			flight = &tokenAcquisition{done: make(chan struct{}), force: forceRefresh, generation: generation}
			e.acquire = flight
			go func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						flight.token = nil
						flight.err = fmt.Errorf("token acquisition panic: %v", recovered)
					}
					e.acquireMu.Lock()
					if e.acquire == flight {
						e.acquire = nil
					}
					close(flight.done)
					e.acquireMu.Unlock()
				}()
				flight.token, flight.current, flight.err = e.getTokenForGeneration(flight.force, flight.generation)
			}()
			e.acquireMu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-flight.done:
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if flight.generation != generation {
					continue
				}
				return tokenOrCurrent(forceRefresh, flight.token, flight.current, flight.err)
			}
		}
		// A forced caller never joins non-forced work. A non-forced caller may
		// share a forced flight, since its result is at least as fresh as requested.
		compatible := flight.generation == generation && (!forceRefresh || flight.force)
		e.acquireMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-flight.done:
			if !compatible {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return tokenOrCurrent(forceRefresh, flight.token, flight.current, flight.err)
		}
	}
}

func (e *entraidTokenManager) GetToken(forceRefresh bool) (*token.Token, error) {
	e.ctxMu.RLock()
	generation := e.generation
	e.ctxMu.RUnlock()
	t, current, err := e.getTokenForGeneration(forceRefresh, generation)
	return tokenOrCurrent(forceRefresh, t, current, err)
}

// tokenOrCurrent keeps a caller that did not force a refresh on the current
// token when the identity provider request failed, so new connections can
// authenticate while the identity provider is unavailable. Forced refreshes,
// such as renewals, get the error.
func tokenOrCurrent(forceRefresh bool, t, current *token.Token, err error) (*token.Token, error) {
	if err != nil && !forceRefresh && current != nil {
		return current, nil
	}
	return t, err
}

// getTokenForGeneration returns the cached token or requests a new one. When
// the request fails while the cached token has not expired, it also returns
// the cached token as current.
func (e *entraidTokenManager) getTokenForGeneration(forceRefresh bool, generation uint64) (t, current *token.Token, err error) {
	e.tokenRWLock.RLock()
	// check if the token is nil and if it is not expired
	t = e.token
	duration := e.durationToRenewal(t)
	if !forceRefresh && t != nil && duration > 0 {
		e.tokenRWLock.RUnlock()
		return t, nil, nil
	}
	e.tokenRWLock.RUnlock()

	// start the context early,
	// since at heavy concurrent load
	// locks may take some time to acquire
	e.ctxMu.RLock()
	baseCtx := e.ctx
	currentGeneration := e.generation
	e.ctxMu.RUnlock()
	if generation != currentGeneration {
		return nil, nil, fmt.Errorf("token acquisition lifecycle retired")
	}
	ctx, ctxCancel := context.WithTimeout(baseCtx, e.requestTimeout)
	defer ctxCancel()

	// Upgrade to write lock for token update
	e.tokenRWLock.Lock()
	defer e.tokenRWLock.Unlock()

	// Double-check pattern to avoid unnecessary token refresh
	t = e.token
	duration = e.durationToRenewal(t)
	if !forceRefresh && t != nil && duration > 0 {
		return t, nil, nil
	}

	cached := t
	// Request a new token from the identity provider
	idpResult, err := e.idp.RequestToken(ctx)
	if err != nil {
		return nil, validToken(cached), fmt.Errorf("failed to request token from idp: %w", err)
	}

	t, err = e.identityProviderResponseParser.ParseResponse(idpResult)
	if err != nil {
		return nil, validToken(cached), fmt.Errorf("failed to parse token: %w", err)
	}

	if t == nil {
		return nil, validToken(cached), fmt.Errorf("failed to get token: token is nil")
	}

	// Hold the lifecycle read lock through publication so Stop cannot retire
	// this generation between the check and storing its token.
	e.ctxMu.RLock()
	defer e.ctxMu.RUnlock()
	if generation != e.generation {
		return nil, nil, fmt.Errorf("token acquisition lifecycle retired")
	}
	// Store the token
	e.token = t
	// Return the token - no need to copy since it's immutable
	return t, nil, nil
}

// validToken returns t if it has not expired, otherwise nil.
func validToken(t *token.Token) *token.Token {
	if t != nil && time.Now().Before(t.ExpirationOn()) {
		return t
	}
	return nil
}

// Start starts the token manager and returns cancelFunc to stop the token manager.
// It takes a TokenListener as an argument, which is used to receive updates.
// The token manager will call the listener's OnNext method with the updated token.
// If an error occurs, the token manager will call the listener's OnError method with the error.
func (e *entraidTokenManager) Start(listener TokenListener) (StopFunc, error) {
	return e.StartWithContext(context.Background(), listener)
}

// StartWithContext starts the refresh loop while allowing the caller to abandon
// the initial shared token acquisition. A canceled attempt never registers its listener.
func (e *entraidTokenManager) StartWithContext(ctx context.Context, listener TokenListener) (StopFunc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case e.startGate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-e.startGate }()
	if e.listener != nil {
		return nil, ErrTokenManagerAlreadyStarted
	}

	// Ensure a prior refresh loop has stopped before creating a replacement.
	if e.closedChan != nil && !internal.IsClosed(e.closedChan) {
		close(e.closedChan)
	}

	e.ctxMu.Lock()
	// A canceled Start leaves shared work owned by this lifecycle. Do not replace
	// its cancel handle until Stop has retired it.
	if e.ctx == nil || e.ctx.Err() != nil {
		e.ctx, e.ctxCancel = context.WithCancel(context.Background())
	}
	e.ctxMu.Unlock()

	// Make sure there is a token in memory before starting the loop. The
	// acquisition worker is shared and is not canceled when this caller leaves.
	_, err := e.GetTokenWithContext(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	e.closedChan = make(chan struct{})
	e.listener = listener

	go func(listener TokenListener, closed <-chan struct{}) {
		// Add panic recovery to prevent crashes
		defer func() {
			if r := recover(); r != nil {
				// Attempt to notify listener of panic, but don't panic again if that fails
				func() {
					defer func() { _ = recover() }()
					listener.OnError(fmt.Errorf("token manager goroutine panic: %v", r))
				}()
			}
		}()
		maxDelay := e.retryOptions.MaxDelay
		initialDelay := e.retryOptions.InitialDelay

		for {
			e.tokenRWLock.RLock()
			timeToRenewal := e.durationToRenewal(e.token)
			e.tokenRWLock.RUnlock()
			select {
			case <-closed:
				return
			case <-time.After(timeToRenewal):
				if timeToRenewal == 0 {
					// Token was requested immediately, guard against infinite loop
					select {
					case <-closed:
						return
					case <-time.After(initialDelay):
						// continue to attempt
					}
				}

				// Token is about to expire, refresh it
				delay := initialDelay
				for i := 0; i < e.retryOptions.MaxAttempts; i++ {
					t, err := e.GetToken(true)
					if err == nil {
						listener.OnNext(t)
						break
					}

					// check if err is retriable
					if e.retryOptions.IsRetryable(err) {
						if i == e.retryOptions.MaxAttempts-1 {
							// last attempt, call OnError and start a new round after maxDelay
							listener.OnError(fmt.Errorf("max attempts reached: %w", err))
							select {
							case <-closed:
								return
							case <-time.After(maxDelay):
							}
							break
						}

						// Exponential backoff
						if delay < maxDelay {
							delay = time.Duration(float64(delay) * e.retryOptions.BackoffMultiplier)
						}
						if delay > maxDelay {
							delay = maxDelay
						}

						select {
						case <-closed:
							return
						case <-time.After(delay):
							// continue to next attempt
						}
					} else {
						// not retriable, but the identity provider may recover:
						// call OnError and start a new round after maxDelay
						listener.OnError(err)
						select {
						case <-closed:
							return
						case <-time.After(maxDelay):
						}
						break
					}
				}
			}
		}
	}(listener, e.closedChan)

	return e.stop, nil
}

// Stop retires both active renewal and acquisition from an abandoned startup.
// Unlike an individual Start stopper, owner cleanup is idempotent.
func (e *entraidTokenManager) Stop() error {
	err := e.stop()
	if errors.Is(err, ErrTokenManagerAlreadyStopped) {
		return nil
	}
	return err
}

// stop closes the token manager and releases any resources.
func (e *entraidTokenManager) stop() (err error) {
	e.startGate <- struct{}{}
	defer func() { <-e.startGate }()
	defer func() {
		// recover from panic and return the error
		if r := recover(); r != nil {
			err = fmt.Errorf("failed to stop token manager: %s", r)
		}
	}()

	e.ctxMu.Lock()
	ctxCancel := e.ctxCancel
	e.generation++
	e.ctxMu.Unlock()
	if ctxCancel != nil {
		ctxCancel()
	}

	if e.closedChan == nil || e.listener == nil {
		return ErrTokenManagerAlreadyStopped
	}

	e.listener = nil

	// Safely close the channel - only close if not already closed
	if !internal.IsClosed(e.closedChan) {
		close(e.closedChan)
	}

	return nil
}

// durationToRenewal calculates the duration to the next token renewal.
// It returns the duration to the next token renewal based on the expiration refresh ratio and the lower bound duration.
// If the token is nil, it returns 0.
// If the time till expiration is less than the lower bound duration, it returns 0 to renew the token now.
func (e *entraidTokenManager) durationToRenewal(t *token.Token) time.Duration {
	// Fast path: nil token check
	if t == nil {
		return 0
	}

	// Get current time in milliseconds (UTC)
	nowMillis := time.Now().UnixMilli()

	// Get expiration time in milliseconds
	expMillis := t.ExpirationOn().UnixMilli()

	// Fast path: token already expired
	if expMillis <= nowMillis {
		return 0
	}

	// Calculate time until expiration in milliseconds
	timeTillExpiration := expMillis - nowMillis

	// Get lower bound in milliseconds
	lowerBoundMillis := e.lowerBoundDuration.Milliseconds()

	// Fast path: time until expiration is less than lower bound
	if timeTillExpiration <= lowerBoundMillis {
		return 0
	}

	// Calculate refresh time using integer math with higher precision
	// example tests use 0.001, which would be lost with lower precision
	// Example:
	// ttlMillis = 10000
	// e.expirationRefreshRatio = 0.001
	//   - with int math and 100 precision: 10000 * (0.001*100) = 0ms
	//   - with int math and 10000 precision: 10000 * (0.001*10000) = 100ms
	precision := int64(RefreshRationPrecision)
	receivedAtMillis := t.ReceivedAt().UnixMilli()
	ttlMillis := t.TTL() // Already in milliseconds
	refreshRatioInt := int64(e.expirationRefreshRatio * float64(precision))
	refreshMillis := ttlMillis * refreshRatioInt / precision
	refreshTimeMillis := receivedAtMillis + refreshMillis

	// Calculate time until refresh
	timeUntilRefresh := refreshTimeMillis - nowMillis

	// Fast path: refresh time is in the past
	if timeUntilRefresh <= 0 {
		return 0
	}

	// Calculate time until lower bound
	timeUntilLowerBound := timeTillExpiration - lowerBoundMillis

	// If refresh would occur after lower bound, use time until lower bound
	if timeUntilRefresh > timeUntilLowerBound {
		return time.Duration(timeUntilLowerBound) * time.Millisecond
	}

	// Otherwise use time until refresh
	return time.Duration(timeUntilRefresh) * time.Millisecond
}
