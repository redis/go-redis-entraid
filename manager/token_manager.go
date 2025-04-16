package manager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis-developer/go-redis-entraid/internal"
	"github.com/redis-developer/go-redis-entraid/shared"
	"github.com/redis-developer/go-redis-entraid/token"
)

// TokenManagerOptions is a struct that contains the options for the TokenManager.
type TokenManagerOptions struct {
	// ExpirationRefreshRatio is the ratio of the token expiration time to refresh the token.
	// It is used to determine when to refresh the token.
	// The value should be between 0 and 1.
	// For example, if the expiration time is 1 hour and the ratio is 0.75,
	// the token will be refreshed after 45 minutes. (the token is refreshed when 75% of its lifetime has passed)
	//
	// default: 0.7
	ExpirationRefreshRatio float64
	// LowerRefreshBound is the lower bound for the refresh time
	// Represents the minimum time before token expiration to trigger a refresh.
	// This value sets a fixed lower bound for when a token refresh should occur, regardless
	// of the token's total lifetime.
	//
	// default: 0 (no lower bound, refresh based on ExpirationRefreshRatio)
	LowerRefreshBound time.Duration

	// IdentityProviderResponseParser is an optional object that implements the IdentityProviderResponseParser interface.
	// It is used to parse the response from the identity provider and extract the token.
	// If not provided, the default implementation will be used.
	// The objects ParseResponse method will be called to parse the response and return the token.
	//
	// required: false
	// default: defaultIdentityProviderResponseParser
	IdentityProviderResponseParser shared.IdentityProviderResponseParser
	// RetryOptions is a struct that contains the options for retrying the token request.
	// It contains the maximum number of attempts, initial delay, maximum delay, and backoff multiplier.
	//
	// The default values are 3 attempts, 1000 ms initial delay, 10000 ms maximum delay, and 2.0 backoff multiplier.
	RetryOptions RetryOptions

	// RequestTimeout is the timeout for the request to the identity provider.
	RequestTimeout time.Duration
}

// RetryOptions is a struct that contains the options for retrying the token request.
type RetryOptions struct {
	// IsRetryable is a function that checks if the error is retriable.
	// It takes an error as an argument and returns a boolean value.
	//
	// default: defaultRetryableFunc
	IsRetryable func(err error) bool
	// MaxAttempts is the maximum number of attempts to retry the token request.
	//
	// default: 3
	MaxAttempts int
	// InitialDelay is the initial delay before retrying the token request.
	//
	// default: 1 second
	InitialDelay time.Duration
	// MaxDelay is the maximum delay between retry attempts.
	//
	// default: 10 seconds
	MaxDelay time.Duration
	// BackoffMultiplier is the multiplier for the backoff delay.
	// default: 2.0
	BackoffMultiplier float64
}

// TokenManager is an interface that defines the methods for managing tokens.
// It provides methods to get a token and start the token manager.
// The TokenManager is responsible for obtaining and refreshing the token.
// It is typically used in conjunction with an IdentityProvider to obtain the token.
type TokenManager interface {
	// GetToken returns the token for authentication.
	// It takes a boolean value forceRefresh as an argument.
	GetToken(forceRefresh bool) (*token.Token, error)
	// Start starts the token manager and returns a channel that will receive updates.
	Start(listener TokenListener) (StopFunc, error)
	// Stop stops the token manager and releases any resources.
	Stop() error
}

// StopFunc is a function that stops the token manager.
type StopFunc func() error

// TokenListener is an interface that contains the methods for receiving updates from the token manager.
// The token manager will call the listener's OnTokenNext method with the updated token.
// If an error occurs, the token manager will call the listener's OnTokenError method with the error.
type TokenListener interface {
	// OnNext is called when the token is updated.
	OnNext(t *token.Token)
	// OnError is called when an error occurs.
	OnError(err error)
}

// entraidIdentityProviderResponseParser is the default implementation of the IdentityProviderResponseParser interface.
var entraidIdentityProviderResponseParser shared.IdentityProviderResponseParser = &defaultIdentityProviderResponseParser{}

// NewTokenManager creates a new TokenManager.
// It takes an IdentityProvider and TokenManagerOptions as arguments and returns a TokenManager interface.
// The IdentityProvider is used to obtain the token, and the TokenManagerOptions contains options for the TokenManager.
// The TokenManager is responsible for managing the token and refreshing it when necessary.
func NewTokenManager(idp shared.IdentityProvider, options TokenManagerOptions) (TokenManager, error) {
	if options.ExpirationRefreshRatio < 0 || options.ExpirationRefreshRatio > 1 {
		return nil, fmt.Errorf("expiration refresh ratio must be between 0 and 1")
	}
	options = defaultTokenManagerOptionsOr(options)

	if idp == nil {
		return nil, fmt.Errorf("identity provider is required")
	}

	ctx, ctxCancel := context.WithCancel(context.Background())
	return &entraidTokenManager{
		idp:                            idp,
		token:                          nil,
		closedChan:                     nil,
		ctx:                            ctx,
		ctxCancel:                      ctxCancel,
		expirationRefreshRatio:         options.ExpirationRefreshRatio,
		lowerBoundDuration:             options.LowerRefreshBound,
		identityProviderResponseParser: options.IdentityProviderResponseParser,
		retryOptions:                   options.RetryOptions,
		requestTimeout:                 options.RequestTimeout,
	}, nil
}

// entraidTokenManager is a struct that implements the TokenManager interface.
type entraidTokenManager struct {
	// idp is the identity provider used to obtain the token.
	idp shared.IdentityProvider

	// token is the authentication token for the user which should be kept in memory if valid.
	token *token.Token

	// tokenRWLock is a read-write lock used to protect the token from concurrent access.
	tokenRWLock sync.RWMutex

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
	lock sync.Mutex

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

	if !forceRefresh && e.token != nil && time.Now().Add(e.lowerBoundDuration).Before(e.token.ExpirationOn()) {
		t := e.token
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
	if !forceRefresh && e.token != nil && time.Now().Add(e.lowerBoundDuration).Before(e.token.ExpirationOn()) {
		return e.token, nil
	}

	// Request a new token from the identity provider
	idpResult, err := e.idp.RequestToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to request token from idp: %w", err)
	}

	t, err := e.identityProviderResponseParser.ParseResponse(idpResult)
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
//
// Note: The initial token is delivered synchronously.
// The TokenListener will receive the token immediately, before the token manager goroutine starts.
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

	t, err := e.GetToken(true)
	if err != nil {
		go listener.OnError(err)
		return nil, fmt.Errorf("failed to start token manager: %w", err)
	}

	// Deliver initial token synchronously
	listener.OnNext(t)

	e.closedChan = make(chan struct{})
	e.listener = listener

	go func(listener TokenListener, closed <-chan struct{}) {
		maxDelay := e.retryOptions.MaxDelay
		initialDelay := e.retryOptions.InitialDelay

		for {
			timeToRenewal := e.durationToRenewal()
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

	return e.Stop, nil
}

// Stop closes the token manager and releases any resources.
func (e *entraidTokenManager) Stop() error {
	e.lock.Lock()
	defer e.lock.Unlock()

	if e.closedChan == nil || e.listener == nil {
		return ErrTokenManagerAlreadyStopped
	}

	e.ctxCancel()
	e.listener = nil
	close(e.closedChan)

	return nil
}

// durationToRenewal calculates the duration to the next token renewal.
// It returns the duration to the next token renewal based on the expiration refresh ratio and the lower bound duration.
// If the token is nil, it returns 0.
// If the time till expiration is less than the lower bound duration, it returns 0 to renew the token now.
func (e *entraidTokenManager) durationToRenewal() time.Duration {
	e.tokenRWLock.RLock()
	if e.token == nil {
		e.tokenRWLock.RUnlock()
		return 0
	}

	timeTillExpiration := time.Until(e.token.ExpirationOn())
	e.tokenRWLock.RUnlock()

	// if the timeTillExpiration is less than the lower bound (or 0), return 0 to renew the token NOW
	if timeTillExpiration <= e.lowerBoundDuration || timeTillExpiration <= 0 {
		return 0
	}

	// Calculate the time to renew the token based on the expiration refresh ratio
	// Since timeTillExpiration is guarded by the lower bound, we can safely multiply it by the ratio
	// and assume the duration is a positive number
	duration := time.Duration(float64(timeTillExpiration) * e.expirationRefreshRatio)

	// if the duration will take us past the lower bound, return the duration to lower bound
	if timeTillExpiration-e.lowerBoundDuration < duration {
		return timeTillExpiration - e.lowerBoundDuration
	}

	// return the calculated duration
	return duration
}
