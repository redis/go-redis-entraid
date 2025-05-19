package manager

import (
	"context"
	"fmt"
	"time"

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
	//
	// default: 30 seconds
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
	// Start starts the token manager and returns a stopper function to stop the token manager
	Start(listener TokenListener) (StopFunc, error)
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
