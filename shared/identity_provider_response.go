package shared

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	"github.com/redis-developer/go-redis-entraid/internal"
	"github.com/redis-developer/go-redis-entraid/token"
)

const (
	// ResponseTypeAuthResult is the type of the auth result.
	ResponseTypeAuthResult = "AuthResult"
	// ResponseTypeAccessToken is the type of the access token.
	ResponseTypeAccessToken = "AccessToken"
	// ResponseTypeRawToken is the type of the response when you have a raw string.
	ResponseTypeRawToken = "RawToken"
)

// IdentityProviderResponseParser is an interface that defines the methods for parsing the identity provider response.
// It is used to parse the response from the identity provider and extract the token.
// If not provided, the default implementation will be used.
type IdentityProviderResponseParser interface {
	ParseResponse(response IdentityProviderResponse) (*token.Token, error)
}

// IdentityProviderResponse is an interface that defines the methods for an identity provider authentication result.
// It is used to get the type of the authentication result, the authentication result itself (can be AuthResult or AccessToken),
type IdentityProviderResponse interface {
	// Type returns the type of the auth result
	Type() string
	AuthResult() public.AuthResult
	AccessToken() azcore.AccessToken
	RawToken() string
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
