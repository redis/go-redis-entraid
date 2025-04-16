package manager

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis-developer/go-redis-entraid/shared"
	"github.com/redis-developer/go-redis-entraid/token"
)

const (
	DefaultExpirationRefreshRatio        = 0.7
	DefaultRetryOptionsMaxAttempts       = 3
	DefaultRetryOptionsInitialDelayMs    = 1000
	DefaultRetryOptionsBackoffMultiplier = 2.0
	DefaultRetryOptionsMaxDelayMs        = 10000
)

// defaultIsRetryable is a function that checks if the error is retriable.
// It takes an error as an argument and returns a boolean value.
// The function checks if the error is a net.Error and if it is a timeout or temporary error.
// Returns true for nil errors.
var defaultIsRetryable = func(err error) bool {
	if err == nil {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		// Check for timeout first as it's more specific
		if netErr.Timeout() {
			return true
		}
		// For temporary errors, we'll use a more modern approach
		var tempErr interface{ Temporary() bool }
		if errors.As(err, &tempErr) {
			return tempErr.Temporary()
		}
	}

	return errors.Is(err, os.ErrDeadlineExceeded)
}

// defaultRetryOptionsOr returns the default retry options if the provided options are not set.
// It sets the maximum number of attempts, initial delay, maximum delay, and backoff multiplier.
// The default values are 3 attempts, 1000 ms initial delay, 10000 ms maximum delay, and 2.0 backoff multiplier.
// The values can be overridden by the user.
func defaultRetryOptionsOr(retryOptions RetryOptions) RetryOptions {
	if retryOptions.IsRetryable == nil {
		retryOptions.IsRetryable = defaultIsRetryable
	}

	if retryOptions.MaxAttempts <= 0 {
		retryOptions.MaxAttempts = DefaultRetryOptionsMaxAttempts
	}
	if retryOptions.InitialDelayMs == 0 {
		retryOptions.InitialDelayMs = DefaultRetryOptionsInitialDelayMs
	}
	if retryOptions.BackoffMultiplier == 0 {
		retryOptions.BackoffMultiplier = DefaultRetryOptionsBackoffMultiplier
	}
	if retryOptions.MaxDelayMs == 0 {
		retryOptions.MaxDelayMs = DefaultRetryOptionsMaxDelayMs
	}
	return retryOptions
}

// defaultIdentityProviderResponseParserOr returns the default token parser if the provided token parser is not set.
// It sets the default token parser to the defaultIdentityProviderResponseParser function.
// The default token parser is used to parse the raw token and return a Token object.
func defaultIdentityProviderResponseParserOr(idpResponseParser shared.IdentityProviderResponseParser) shared.IdentityProviderResponseParser {
	if idpResponseParser == nil {
		return entraidIdentityProviderResponseParser
	}
	return idpResponseParser
}

func defaultTokenManagerOptionsOr(options TokenManagerOptions) TokenManagerOptions {
	options.RetryOptions = defaultRetryOptionsOr(options.RetryOptions)
	options.IdentityProviderResponseParser = defaultIdentityProviderResponseParserOr(options.IdentityProviderResponseParser)
	if options.ExpirationRefreshRatio == 0 {
		options.ExpirationRefreshRatio = DefaultExpirationRefreshRatio
	}
	return options
}

type defaultIdentityProviderResponseParser struct{}

// ParseResponse parses the response from the identity provider and extracts the token.
// It takes an IdentityProviderResponse as an argument and returns a Token and an error if any.
// The IdentityProviderResponse contains the raw token and the expiration time.
func (*defaultIdentityProviderResponseParser) ParseResponse(response shared.IdentityProviderResponse) (*token.Token, error) {
	if response == nil {
		return nil, fmt.Errorf("identity provider response cannot be nil")
	}

	var username, password, rawToken string
	var expiresOn time.Time
	now := time.Now().UTC()

	switch response.Type() {
	case shared.ResponseTypeAuthResult:
		authResult := response.AuthResult()
		if authResult.ExpiresOn.IsZero() {
			return nil, fmt.Errorf("auth result expiration time is not set")
		}
		if authResult.IDToken.Oid == "" {
			return nil, fmt.Errorf("auth result OID is empty")
		}
		rawToken = authResult.IDToken.RawToken
		username = authResult.IDToken.Oid
		password = rawToken
		expiresOn = authResult.ExpiresOn.UTC()

	case shared.ResponseTypeRawToken, shared.ResponseTypeAccessToken:
		tokenStr := response.RawToken()

		if response.Type() == shared.ResponseTypeAccessToken {
			accessToken := response.AccessToken()
			if accessToken.Token == "" {
				return nil, fmt.Errorf("access token value is empty")
			}
			tokenStr = accessToken.Token
			expiresOn = accessToken.ExpiresOn.UTC()
		}

		if tokenStr == "" {
			return nil, fmt.Errorf("raw token is empty")
		}

		claims := struct {
			jwt.RegisteredClaims
			Oid string `json:"oid,omitempty"`
		}{}

		// Parse the token to extract claims, but note that signature verification
		// should be handled by the identity provider
		_, _, err := jwt.NewParser().ParseUnverified(tokenStr, &claims)
		if err != nil {
			return nil, fmt.Errorf("failed to parse JWT token: %w", err)
		}

		if claims.Oid == "" {
			return nil, fmt.Errorf("JWT token does not contain OID claim")
		}

		rawToken = tokenStr
		username = claims.Oid
		password = rawToken

		if expiresOn.IsZero() && claims.ExpiresAt != nil {
			expiresOn = claims.ExpiresAt.UTC()
		}

	default:
		return nil, fmt.Errorf("unsupported response type: %s", response.Type())
	}

	if expiresOn.IsZero() {
		return nil, fmt.Errorf("token expiration time is not set")
	}

	if expiresOn.Before(now) {
		return nil, fmt.Errorf("token has expired at %s (current time: %s)", expiresOn, now)
	}

	// Create the token with consistent time reference
	return token.New(
		username,
		password,
		rawToken,
		expiresOn,
		now,
		int64(time.Until(expiresOn).Seconds()),
	), nil
}
