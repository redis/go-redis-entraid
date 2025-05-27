package shared

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	"github.com/redis/go-redis-entraid/internal"
	"github.com/redis/go-redis-entraid/token"
)

const (
	// ResponseTypeAuthResult is the type of the auth result.
	ResponseTypeAuthResult = "AuthResult"
	// ResponseTypeAccessToken is the type of the access token.
	ResponseTypeAccessToken = "AccessToken"
	// ResponseTypeRawToken is the type of the response when you have a raw string.
	ResponseTypeRawToken = "RawToken"
)

var ErrInvalidIDPResponse = internal.ErrInvalidIDPResponse
var ErrAuthResultNotFound = internal.ErrAuthResultNotFound
var ErrAccessTokenNotFound = internal.ErrAccessTokenNotFound
var ErrRawTokenNotFound = internal.ErrRawTokenNotFound

// IdentityProviderResponseParser is an interface that defines the methods for parsing the identity provider response.
// It is used to parse the response from the identity provider and extract the token.
// If not provided, the default implementation will be used.
type IdentityProviderResponseParser interface {
	ParseResponse(response IdentityProviderResponse) (*token.Token, error)
}

// IdentityProviderResponse is an interface that defines the
// type method for the identity provider response. It is used to
// identify the type of response returned by the identity provider.
// The type can be either AuthResult, AccessToken, or RawToken. You can
// use this interface to check the type of the response and handle it accordingly.
// Available response types are:
// - ResponseTypeAuthResult: For Microsoft Authentication Library AuthResult
// - ResponseTypeAccessToken: For Azure SDK AccessToken
// - ResponseTypeRawToken: For raw token strings
type IdentityProviderResponse interface {
	// Type returns the type of identity provider response
	Type() string
	// RawToken returns the raw token string.
	// Returns ErrRawTokenNotFound if the raw token is not set.
	RawToken() (string, error)
}

// AuthResultIDPResponse is an interface that defines the method for getting the auth result.
// Returns ErrAuthResultNotFound if the auth result is not set.
type AuthResultIDPResponse interface {
	IdentityProviderResponse
	// AuthResult returns the Microsoft Authentication Library AuthResult.
	// Returns ErrAuthResultNotFound if the auth result is not set.
	AuthResult() (public.AuthResult, error)
}

// AccessTokenIDPResponse is an interface that defines the method for getting the access token.
// Returns ErrAccessTokenNotFound if the access token is not set.
type AccessTokenIDPResponse interface {
	IdentityProviderResponse
	// AccessToken returns the Azure SDK AccessToken.
	// Returns ErrAccessTokenNotFound if the access token is not set.
	AccessToken() (azcore.AccessToken, error)
}

type RawTokenIDPResponse interface {
	IdentityProviderResponse
}

// IdentityProvider is an interface that defines the methods for an identity provider.
// It is used to request a token for authentication.
// The identity provider is responsible for providing the raw authentication token.
type IdentityProvider interface {
	// RequestToken requests a token from the identity provider.
	// The context is passed to the request to allow for cancellation and timeouts.
	// It returns the token, the expiration time, and an error if any.
	RequestToken(ctx context.Context) (IdentityProviderResponse, error)
}

// NewIDPResponse creates a new auth result based on the type provided.
// It returns an IdentityProviderResponse interface.
// Type can be either AuthResult, AccessToken, or RawToken.
// Second argument is the result of the type provided in the first argument.
func NewIDPResponse(responseType string, result interface{}) (IdentityProviderResponse, error) {
	return internal.NewIDPResp(responseType, result)
}
