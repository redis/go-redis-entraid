package identity

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/redis/go-redis-entraid/shared"
)

// DefaultAzureIdentityProviderOptions represents the options for the DefaultAzureIdentityProvider.
type DefaultAzureIdentityProviderOptions struct {
	// AzureOptions is the options used to configure the Azure identity provider.
	AzureOptions *azidentity.DefaultAzureCredentialOptions
	// Scopes is the list of scopes used to request a token from the identity provider.
	Scopes []string

	// credFactory is a factory for creating the default Azure credential.
	// This is used for testing purposes, to allow mocking the credential creation.
	// If not provided, the default implementation - azidentity.NewDefaultAzureCredential will be used
	credFactory credFactory
}

type credFactory interface {
	NewDefaultAzureCredential(options *azidentity.DefaultAzureCredentialOptions) (azureCredential, error)
}

type azureCredential interface {
	GetToken(ctx context.Context, options policy.TokenRequestOptions) (azcore.AccessToken, error)
}

type defaultCredFactory struct{}

func (d *defaultCredFactory) NewDefaultAzureCredential(options *azidentity.DefaultAzureCredentialOptions) (azureCredential, error) {
	return azidentity.NewDefaultAzureCredential(options)
}

type DefaultAzureIdentityProvider struct {
	options     *azidentity.DefaultAzureCredentialOptions
	credFactory credFactory
	scopes      []string
}

// NewDefaultAzureIdentityProvider creates a new DefaultAzureIdentityProvider.
func NewDefaultAzureIdentityProvider(opts DefaultAzureIdentityProviderOptions) (*DefaultAzureIdentityProvider, error) {
	if opts.Scopes == nil {
		opts.Scopes = []string{RedisScopeDefault}
	}

	return &DefaultAzureIdentityProvider{
		options:     opts.AzureOptions,
		scopes:      opts.Scopes,
		credFactory: opts.credFactory,
	}, nil
}

// RequestToken requests a token from the Azure Default Identity provider.
// It returns the token, the expiration time, and an error if any.
func (a *DefaultAzureIdentityProvider) RequestToken(ctx context.Context) (shared.IdentityProviderResponse, error) {
	credFactory := a.credFactory
	if credFactory == nil {
		credFactory = &defaultCredFactory{}
	}
	cred, err := credFactory.NewDefaultAzureCredential(a.options)
	if err != nil {
		return nil, fmt.Errorf("failed to create default azure credential: %w", err)
	}

	token, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: a.scopes})
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	return shared.NewIDPResponse(shared.ResponseTypeAccessToken, &token)
}
