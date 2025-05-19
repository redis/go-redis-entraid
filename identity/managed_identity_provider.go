package identity

import (
	"context"
	"errors"
	"fmt"

	mi "github.com/AzureAD/microsoft-authentication-library-for-go/apps/managedidentity"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	"github.com/redis-developer/go-redis-entraid/shared"
)

// ManagedIdentityClient is an interface that defines the methods for a managed identity client.
// It is used to acquire a token using the managed identity.
type ManagedIdentityClient interface {
	// AcquireToken acquires a token using the managed identity.
	// It returns the token and an error if any.
	AcquireToken(ctx context.Context, resource string, opts ...mi.AcquireTokenOption) (public.AuthResult, error)
}

// ManagedIdentityProviderOptions represents the options for the managed identity provider.
// It is used to configure the identity provider when requesting a token.
type ManagedIdentityProviderOptions struct {
	// UserAssignedClientID is the client ID of the user assigned identity.
	// This is used to identify the identity when requesting a token.
	UserAssignedClientID string
	// ManagedIdentityType is the type of managed identity.
	// This can be either SystemAssigned or UserAssigned.
	ManagedIdentityType string
	// Scopes is a list of scopes that the identity has access to.
	// This is used to specify the permissions that the identity has when requesting a token.
	Scopes []string
}

// ManagedIdentityProvider represents a managed identity provider.
type ManagedIdentityProvider struct {
	// userAssignedClientID is the client ID of the user assigned identity.
	// This is used to identify the identity when requesting a token.
	userAssignedClientID string

	// managedIdentityType is the type of managed identity.
	// This can be either SystemAssigned or UserAssigned.
	managedIdentityType string

	// scopes is a list of scopes that the identity has access to.
	// This is used to specify the permissions that the identity has when requesting a token.
	scopes []string

	// client is the managed identity client used to request a token.
	client ManagedIdentityClient
}

// realManagedIdentityClient is a wrapper around the real mi.Client that implements our interface
type realManagedIdentityClient struct {
	client ManagedIdentityClient
}

func (c *realManagedIdentityClient) AcquireToken(ctx context.Context, resource string, opts ...mi.AcquireTokenOption) (public.AuthResult, error) {
	return c.client.AcquireToken(ctx, resource, opts...)
}

// NewManagedIdentityProvider creates a new managed identity provider for Azure with managed identity.
// It is used to configure the identity provider when requesting a token.
func NewManagedIdentityProvider(opts ManagedIdentityProviderOptions) (*ManagedIdentityProvider, error) {
	var client ManagedIdentityClient

	if opts.ManagedIdentityType != SystemAssignedIdentity && opts.ManagedIdentityType != UserAssignedIdentity {
		return nil, errors.New("invalid managed identity type")
	}

	switch opts.ManagedIdentityType {
	case SystemAssignedIdentity:
		// SystemAssignedIdentity is the type of identity that is automatically managed by Azure.
		// This type of identity is automatically created and managed by Azure.
		// It is used to authenticate the identity when requesting a token.
		miClient, err := mi.New(mi.SystemAssigned())
		if err != nil {
			return nil, fmt.Errorf("couldn't create managed identity client: %w", err)
		}
		client = &realManagedIdentityClient{client: miClient}
	case UserAssignedIdentity:
		// UserAssignedIdentity is required to be specified when using a user assigned identity.
		if opts.UserAssignedClientID == "" {
			return nil, errors.New("user assigned client ID is required when using user assigned identity")
		}
		// UserAssignedIdentity is the type of identity that is managed by the user.
		miClient, err := mi.New(mi.UserAssignedClientID(opts.UserAssignedClientID))
		if err != nil {
			return nil, fmt.Errorf("couldn't create managed identity client: %w", err)
		}
		client = &realManagedIdentityClient{client: miClient}
	}

	return &ManagedIdentityProvider{
		userAssignedClientID: opts.UserAssignedClientID,
		managedIdentityType:  opts.ManagedIdentityType,
		scopes:               opts.Scopes,
		client:               client,
	}, nil
}

// RequestToken requests a token from the managed identity provider.
// It returns IdentityProviderResponse, which contains the Acc and the expiration time.
func (m *ManagedIdentityProvider) RequestToken(ctx context.Context) (shared.IdentityProviderResponse, error) {
	if m.client == nil {
		return nil, errors.New("managed identity client is not initialized")
	}

	// default resource is RedisResource == "https://redis.azure.com"
	// if no scopes are provided, use the default resource
	// if scopes are provided, use the first scope as the resource
	resource := RedisResource
	if len(m.scopes) > 0 {
		resource = m.scopes[0]
	}
	// acquire token using the managed identity client
	// the resource is the URL of the resource that the identity has access to
	authResult, err := m.client.AcquireToken(ctx, resource)
	if err != nil {
		return nil, fmt.Errorf("couldn't acquire token: %w", err)
	}

	return shared.NewIDPResponse(shared.ResponseTypeAuthResult, &authResult)
}
