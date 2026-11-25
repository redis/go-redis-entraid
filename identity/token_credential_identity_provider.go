package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/redis/go-redis-entraid/shared"
)

// ErrNewCredentialRequired is returned when TokenCredentialIdentityProviderOptions has no NewCredential.
var ErrNewCredentialRequired = errors.New("NewCredential is required")

// TokenCredentialIdentityProviderOptions represents the options for the TokenCredentialIdentityProvider.
type TokenCredentialIdentityProviderOptions struct {
	// NewCredential returns the credential used to request a token, for example
	// an azidentity.WorkloadIdentityCredential for a specific client ID.
	// It is called on every token request, like the DefaultAzureIdentityProvider
	// does. A long-lived credential keeps its own token cache and can return a
	// cached token when the token manager asks for a refresh.
	// Required.
	NewCredential func() (azcore.TokenCredential, error)
	// Scopes is the list of scopes used to request a token from the identity provider.
	// Default: [RedisScopeDefault]
	Scopes []string
}

// TokenCredentialIdentityProvider requests tokens from any azcore.TokenCredential.
type TokenCredentialIdentityProvider struct {
	newCredential func() (azcore.TokenCredential, error)
	scopes        []string
}

// NewTokenCredentialIdentityProvider creates a new TokenCredentialIdentityProvider.
func NewTokenCredentialIdentityProvider(opts TokenCredentialIdentityProviderOptions) (*TokenCredentialIdentityProvider, error) {
	if opts.NewCredential == nil {
		return nil, ErrNewCredentialRequired
	}
	if opts.Scopes == nil {
		opts.Scopes = []string{RedisScopeDefault}
	}

	return &TokenCredentialIdentityProvider{
		newCredential: opts.NewCredential,
		scopes:        opts.Scopes,
	}, nil
}

// RequestToken requests a token from the credential returned by NewCredential.
// It returns the token, the expiration time, and an error if any.
func (p *TokenCredentialIdentityProvider) RequestToken(ctx context.Context) (shared.IdentityProviderResponse, error) {
	cred, err := p.newCredential()
	if err != nil {
		return nil, fmt.Errorf("failed to create token credential: %w", err)
	}

	token, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: p.scopes})
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	return shared.NewIDPResponse(shared.ResponseTypeAccessToken, &token)
}
