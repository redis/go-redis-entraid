package manager

import (
	"context"
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

	// lock locks the listener to prevent concurrent access.
	lock *sync.Mutex

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

	// context is the context used to request the token from the identity provider.
	ctx context.Context

	// ctxCancel is the cancel function for the context.
	ctxCancel context.CancelFunc

	// requestTimeout is the timeout for the request to the identity provider.
	requestTimeout time.Duration
}

func (e *entraidTokenManager) GetToken(forceRefresh bool) (*token.Token, error) {
	e.tokenRWLock.RLock()
	// check if the token is nil and if it is not expired
	t := e.token
	duration := e.durationToRenewal(t)
	if !forceRefresh && t != nil && duration > 0 {
		e.tokenRWLock.RUnlock()
		return t, nil
	}
	e.tokenRWLock.RUnlock()

	// start the context early,
	// since at heavy concurrent load
	// locks may take some time to acquire
	ctx, ctxCancel := context.WithTimeout(e.ctx, e.requestTimeout)
	defer ctxCancel()

	// Upgrade to write lock for token update
	e.tokenRWLock.Lock()
	defer e.tokenRWLock.Unlock()

	// Double-check pattern to avoid unnecessary token refresh
	t = e.token
	duration = e.durationToRenewal(t)
	if !forceRefresh && t != nil && duration > 0 {
		return t, nil
	}

	// Request a new token from the identity provider
	idpResult, err := e.idp.RequestToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to request token from idp: %w", err)
	}

	t, err = e.identityProviderResponseParser.ParseResponse(idpResult)
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if t == nil {
		return nil, fmt.Errorf("failed to get token: token is nil")
	}

	// Store the token
	e.token = t
	// Return the token - no need to copy since it's immutable
	return t, nil
}

// Start starts the token manager and returns cancelFunc to stop the token manager.
// It takes a TokenListener as an argument, which is used to receive updates.
// The token manager will call the listener's OnNext method with the updated token.
// If an error occurs, the token manager will call the listener's OnError method with the error.
func (e *entraidTokenManager) Start(listener TokenListener) (StopFunc, error) {
	e.lock.Lock()
	defer e.lock.Unlock()
	if e.listener != nil {
		return nil, ErrTokenManagerAlreadyStarted
	}

	if e.closedChan != nil && !internal.IsClosed(e.closedChan) {
		// there is a hanging goroutine that is waiting for the closedChan to be closed
		// if the closedChan is not nil and not closed, close it
		close(e.closedChan)
	}

	ctx, ctxCancel := context.WithCancel(context.Background())
	e.ctx = ctx
	e.ctxCancel = ctxCancel

	// make sure there is token in memory before starting the loop
	_, err := e.GetToken(false)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
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
							// last attempt, call OnError
							listener.OnError(fmt.Errorf("max attempts reached: %w", err))
							return
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
						// not retriable
						listener.OnError(err)
						return
					}
				}
			}
		}
	}(listener, e.closedChan)

	return e.stop, nil
}

// stop closes the token manager and releases any resources.
func (e *entraidTokenManager) stop() (err error) {
	e.lock.Lock()
	defer e.lock.Unlock()
	defer func() {
		// recover from panic and return the error
		if r := recover(); r != nil {
			err = fmt.Errorf("failed to stop token manager: %s", r)
		}
	}()

	if e.ctxCancel != nil {
		e.ctxCancel()
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
