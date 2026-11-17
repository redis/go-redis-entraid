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

	// Cache, shared request and lifecycle selection are atomic. No identity
	// provider, parser, listener or wait runs under this lock.
	tokenRWLock       *sync.RWMutex
	acquire           *tokenAcquisition
	nextDemand        time.Time     // earliest retry from a due cache read
	changed           chan struct{} // current scheduler's cache version; closed on rotation or stop
	allowExpiredToken bool

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

	// ctx identifies the owner generation of a shared request.
	ctx       context.Context
	ctxCancel context.CancelFunc

	// requestTimeout is the timeout for the request to the identity provider.
	requestTimeout time.Duration
}

type tokenAcquisition struct {
	done   chan struct{}
	token  *token.Token
	err    error
	owner  context.Context
	force  bool
	demand bool // a foreground reader needs this request, even after unsubscribe
}

var errLifecycleRetired = fmt.Errorf("token acquisition lifecycle retired: %w", context.Canceled)

// GetTokenWithContext lets a caller stop waiting for a shared token acquisition.
// Incompatible refresh intent and lifecycle generations wait for the single
// active worker to finish, then start their own eligible acquisition.
func (e *entraidTokenManager) GetTokenWithContext(ctx context.Context, forceRefresh bool) (*token.Token, error) {
	return e.getToken(ctx, forceRefresh, nil)
}

// A periodic caller supplies the cache version it scheduled against. Rotation
// or stop invalidates that decision, including between a timer firing and the
// worker being installed. Foreground reads have no such precondition.
func (e *entraidTokenManager) getToken(ctx context.Context, forceRefresh bool, scheduled <-chan struct{}) (*token.Token, error) {
	var owner context.Context
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		e.tokenRWLock.Lock()
		if scheduled != nil && scheduled != e.changed {
			e.tokenRWLock.Unlock()
			return nil, nil // let the loop reschedule or exit
		}
		if owner != nil && owner != e.ctx {
			e.tokenRWLock.Unlock()
			return nil, errLifecycleRetired
		}
		owner = e.ctx
		flight := e.acquire
		current := e.token
		if !forceRefresh && current != nil && (e.allowExpiredToken || validToken(current) != nil) {
			if owner != nil && e.durationToRenewal(current) == 0 {
				if flight == nil && !time.Now().Before(e.nextDemand) {
					flight = e.startAcquisition(true)
				}
				if flight != nil {
					flight.demand = true
				}
			}
			e.tokenRWLock.Unlock()
			return current, nil
		}
		if owner == nil {
			e.tokenRWLock.Unlock()
			return nil, errLifecycleRetired
		}
		if flight == nil {
			flight = e.startAcquisition(forceRefresh)
		}
		compatible := flight.owner == owner && (!forceRefresh || flight.force)
		if compatible && scheduled == nil {
			flight.demand = true
		}
		e.tokenRWLock.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-scheduled:
			return nil, nil
		case <-flight.done:
			if !compatible {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return flight.token, flight.err
		}
	}
}

func (e *entraidTokenManager) GetToken(forceRefresh bool) (*token.Token, error) {
	return e.GetTokenWithContext(context.Background(), forceRefresh)
}

// Called with tokenRWLock held. Even a retired request keeps its slot until
// it exits; custom identity providers and parsers need not support concurrency.
func (e *entraidTokenManager) startAcquisition(force bool) *tokenAcquisition {
	flight := &tokenAcquisition{done: make(chan struct{}), force: force, owner: e.ctx}
	e.acquire = flight
	go func() {
		ctx, cancel := context.WithTimeout(flight.owner, e.requestTimeout)
		defer cancel()
		t, err := e.requestToken(ctx)
		e.tokenRWLock.Lock()
		defer e.tokenRWLock.Unlock()
		if flight.owner != e.ctx {
			t, err = nil, errLifecycleRetired
		} else {
			if err == nil {
				e.token = t
				if e.changed != nil {
					close(e.changed)
					e.changed = make(chan struct{})
				}
			}
			if force {
				switch {
				case err != nil:
					e.nextDemand = time.Now().Add(max(e.retryOptions.InitialDelay, e.retryOptions.MaxDelay))
				case e.durationToRenewal(t) == 0:
					e.nextDemand = time.Now().Add(e.retryOptions.InitialDelay)
				default:
					e.nextDemand = time.Time{}
				}
			}
		}
		flight.token, flight.err = t, err
		e.acquire = nil
		close(flight.done)
	}()
	return flight
}

func (e *entraidTokenManager) requestToken(ctx context.Context) (t *token.Token, err error) {
	defer func() {
		if r := recover(); r != nil {
			t, err = nil, fmt.Errorf("token acquisition panic: %v", r)
		}
	}()
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
	return t, nil
}

// currentTokenMinValidity is how long the current token must still be valid to
// be handed out when a request for a new one failed, so a caller does not get
// a token that expires before it can be used.
const currentTokenMinValidity = time.Second

// validToken returns t if it stays valid for currentTokenMinValidity,
// otherwise nil.
func validToken(t *token.Token) *token.Token {
	if t != nil && time.Until(t.ExpirationOn()) > currentTokenMinValidity {
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
	return e.start(ctx, listener, false)
}

// StartSubscriptions pauses only periodic work when the last connection leaves.
func (e *entraidTokenManager) StartSubscriptions(ctx context.Context, listener TokenListener) (internal.StopRenewal, error) {
	stop, err := e.start(ctx, listener, true)
	return internal.StopRenewal(stop), err
}

func (e *entraidTokenManager) start(ctx context.Context, listener TokenListener, keepDemand bool) (StopFunc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case e.startGate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-e.startGate }()
	if e.closedChan != nil && !internal.IsClosed(e.closedChan) {
		return nil, ErrTokenManagerAlreadyStarted
	}

	e.tokenRWLock.Lock()
	// A canceled Start leaves shared work owned by this lifecycle. Do not replace
	// its cancel handle until Stop has retired it.
	if e.ctx == nil || e.ctx.Err() != nil {
		e.ctx, e.ctxCancel = context.WithCancel(context.Background())
	}
	e.tokenRWLock.Unlock()

	// Make sure there is a token in memory before starting the loop. The
	// acquisition worker is shared and is not canceled when this caller leaves.
	initial, err := e.GetTokenWithContext(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	closed := make(chan struct{})
	e.closedChan, e.listener = closed, listener
	e.tokenRWLock.Lock()
	e.changed = make(chan struct{})
	seen := e.changed
	if initial != e.token {
		seen = nil // rotation between the initial read and starting this loop
	}
	e.tokenRWLock.Unlock()

	go func() {
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
		// roundDelay is the wait before a new round after a failed one. It is at
		// least initialDelay, so a small or negative maxDelay cannot make the
		// loop send requests and errors without a pause.
		roundDelay := max(maxDelay, initialDelay)
		// renewalDelay is the wait before renewing a token that is already due:
		// initialDelay, or roundDelay after a failed round
		renewalDelay := initialDelay

	rounds:
		for {
			e.tokenRWLock.RLock()
			t, changed := e.token, e.changed
			timeToRenewal := e.durationToRenewal(t)
			if timeToRenewal == 0 {
				timeToRenewal = max(renewalDelay, time.Until(e.nextDemand))
			}
			e.tokenRWLock.RUnlock()
			if internal.IsClosed(closed) {
				return
			}
			if seen != changed {
				seen, renewalDelay = changed, initialDelay
				listener.OnNext(t)
				continue
			}
			select {
			case <-closed:
				return
			case <-changed:
				continue
			case <-time.After(timeToRenewal):
				renewalDelay = initialDelay

				// Token is about to expire, refresh it
				delay := initialDelay
				for i := 0; i < e.retryOptions.MaxAttempts; i++ {
					_, err := e.getToken(context.Background(), true, changed)
					if err == nil || internal.IsClosed(closed) {
						break // next iteration observes the published token or stop
					}

					// check if err is retriable
					if e.retryOptions.IsRetryable(err) {
						if i == e.retryOptions.MaxAttempts-1 {
							// last attempt, call OnError and start a new round after roundDelay
							listener.OnError(fmt.Errorf("max attempts reached: %w", err))
							renewalDelay = roundDelay
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
						case <-changed:
							continue rounds
						case <-time.After(delay):
							// continue to next attempt
						}
					} else {
						// not retriable, but the identity provider may recover:
						// call OnError and start a new round after roundDelay
						listener.OnError(err)
						renewalDelay = roundDelay
						break
					}
				}
			}
		}
	}()

	return func() error { return e.stopRun(closed, keepDemand) }, nil
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
func (e *entraidTokenManager) stop() error { return e.stopRun(nil, false) }

func (e *entraidTokenManager) stopRun(closed <-chan struct{}, keepDemand bool) (err error) {
	e.startGate <- struct{}{}
	defer func() { <-e.startGate }()
	defer func() {
		// recover from panic and return the error
		if r := recover(); r != nil {
			err = fmt.Errorf("failed to stop token manager: %s", r)
		}
	}()

	e.tokenRWLock.Lock()
	defer e.tokenRWLock.Unlock()
	if closed != nil && (closed != e.closedChan || e.changed == nil) {
		return ErrTokenManagerAlreadyStopped
	}
	if !keepDemand || (e.acquire != nil && !e.acquire.demand) {
		if e.ctxCancel != nil {
			e.ctxCancel()
		}
		e.ctx, e.ctxCancel = nil, nil
		if keepDemand {
			e.ctx, e.ctxCancel = context.WithCancel(context.Background())
		} else {
			e.nextDemand = time.Time{}
		}
	}
	if e.changed == nil {
		return ErrTokenManagerAlreadyStopped
	}
	close(e.closedChan)
	close(e.changed)
	e.changed, e.listener = nil, nil

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
