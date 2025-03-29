package entraid

import (
	"fmt"

	"github.com/redis-developer/go-redis-entraid/identity"
	"github.com/redis-developer/go-redis-entraid/manager"
	"github.com/redis-developer/go-redis-entraid/shared"
	"github.com/redis/go-redis/v9/auth"
)

// CredentialsProviderOptions is a struct that holds the options for the credentials provider.
// It is used to configure the streaming credentials provider when requesting a token with a token manager.
type CredentialsProviderOptions struct {
	// ClientID is the client ID of the identity.
	// This is used to identify the identity when requesting a token.
	ClientID string

	// TokenManagerOptions is the options for the token manager.
	// This is used to configure the token manager when requesting a token.
	TokenManagerOptions manager.TokenManagerOptions

	// tokenManagerFactory is a private field that can be injected from within the package.
	// It is used to create a token manager for the credentials provider.
	tokenManagerFactory func(shared.IdentityProvider, manager.TokenManagerOptions) (manager.TokenManager, error)
}

// defaultTokenManagerFactory is the default implementation of the token manager factory.
// It creates a new token manager using the provided identity provider and options.
func defaultTokenManagerFactory(provider shared.IdentityProvider, options manager.TokenManagerOptions) (manager.TokenManager, error) {
	return manager.NewTokenManager(provider, options)
}

// getTokenManagerFactory returns the token manager factory to use.
// If no factory is provided, it returns the default implementation.
func (o *CredentialsProviderOptions) getTokenManagerFactory() func(shared.IdentityProvider, manager.TokenManagerOptions) (manager.TokenManager, error) {
	if o.tokenManagerFactory == nil {
		return defaultTokenManagerFactory
	}
	return o.tokenManagerFactory
}

// Managed identity type

// ManagedIdentityCredentialsProviderOptions is a struct that holds the options for the managed identity credentials provider.
type ManagedIdentityCredentialsProviderOptions struct {
	// CredentialsProviderOptions is the options for the credentials provider.
	// This is used to configure the credentials provider when requesting a token.
	// It is used to specify the client ID, tenant ID, and scopes for the identity.
	CredentialsProviderOptions

	// ManagedIdentityProviderOptions is the options for the managed identity provider.
	// This is used to configure the managed identity provider when requesting a token.
	identity.ManagedIdentityProviderOptions
}

// NewManagedIdentityCredentialsProvider creates a new streaming credentials provider for managed identity.
// It uses the provided options to configure the provider.
// Use this when you want either a system assigned identity or a user assigned identity.
// The system assigned identity is automatically managed by Azure and does not require any additional configuration.
// The user assigned identity is a separate resource that can be managed independently.
func NewManagedIdentityCredentialsProvider(options ManagedIdentityCredentialsProviderOptions) (auth.StreamingCredentialsProvider, error) {
	// Create a new identity provider using the managed identity type.
	idp, err := identity.NewManagedIdentityProvider(options.ManagedIdentityProviderOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot create managed identity provider: %w", err)
	}

	// Create a new token manager using the identity provider.
	tokenManager, err := options.getTokenManagerFactory()(idp, options.TokenManagerOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot create token manager: %w", err)
	}
	// Create a new credentials provider using the token manager.
	credentialsProvider, err := NewCredentialsProvider(tokenManager, options.CredentialsProviderOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot create credentials provider: %w", err)
	}

	return credentialsProvider, nil
}

// ConfidentialCredentialsProviderOptions is a struct that holds the options for the confidential credentials provider.
// It is used to configure the credentials provider when requesting a token.
type ConfidentialCredentialsProviderOptions struct {
	// CredentialsProviderOptions is the options for the credentials provider.
	// This is used to configure the credentials provider when requesting a token.
	CredentialsProviderOptions

	// ConfidentialIdentityProviderOptions is the options for the confidential identity provider.
	// This is used to configure the identity provider when requesting a token.
	identity.ConfidentialIdentityProviderOptions
}

// NewConfidentialCredentialsProvider creates a new confidential credentials provider.
// It uses client id and client credentials to authenticate with the identity provider.
// The client credentials can be either a client secret or a client certificate.
func NewConfidentialCredentialsProvider(options ConfidentialCredentialsProviderOptions) (auth.StreamingCredentialsProvider, error) {
	// Create a new identity provider using the client ID and client credentials.
	idp, err := identity.NewConfidentialIdentityProvider(options.ConfidentialIdentityProviderOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot create confidential identity provider: %w", err)
	}

	// Create a new token manager using the identity provider.
	tokenManager, err := options.getTokenManagerFactory()(idp, options.TokenManagerOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot create token manager: %w", err)
	}

	// Create a new credentials provider using the token manager.
	credentialsProvider, err := NewCredentialsProvider(tokenManager, options.CredentialsProviderOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot create credentials provider: %w", err)
	}
	return credentialsProvider, nil
}

// DefaultAzureCredentialsProviderOptions is a struct that holds the options for the default azure credentials provider.
// It is used to configure the credentials provider when requesting a token.
type DefaultAzureCredentialsProviderOptions struct {
	CredentialsProviderOptions
	identity.DefaultAzureIdentityProviderOptions
}

// NewDefaultAzureCredentialsProvider creates a new default azure credentials provider.
// It uses the default azure identity provider to authenticate with the identity provider.
// The default azure identity provider is a special type of identity provider that uses the default azure identity to authenticate.
// It is used to authenticate with the identity provider when requesting a token.
func NewDefaultAzureCredentialsProvider(options DefaultAzureCredentialsProviderOptions) (auth.StreamingCredentialsProvider, error) {
	// Create a new identity provider using the default azure identity type.
	idp, err := identity.NewDefaultAzureIdentityProvider(options.DefaultAzureIdentityProviderOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot create default azure identity provider: %w", err)
	}

	// Create a new token manager using the identity provider.
	tokenManager, err := options.getTokenManagerFactory()(idp, options.TokenManagerOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot create token manager: %w", err)
	}

	// Create a new credentials provider using the token manager.
	credentialsProvider, err := NewCredentialsProvider(tokenManager, options.CredentialsProviderOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot create credentials provider: %w", err)
	}
	return credentialsProvider, nil
}
