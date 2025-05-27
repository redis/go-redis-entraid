package identity

import (
	"context"
	"crypto"
	"crypto/x509"
	"fmt"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/confidential"
	"github.com/redis/go-redis-entraid/shared"
)

// ConfidentialIdentityProviderOptions represents the options for the confidential identity provider.
type ConfidentialIdentityProviderOptions struct {
	// ClientID is the client ID used to authenticate with the identity provider.
	ClientID string

	// CredentialsType is the type of credentials used to authenticate with the identity provider.
	// This can be either "ClientSecret" or "ClientCertificate".
	CredentialsType string

	// ClientSecret is the client secret used to authenticate with the identity provider.
	ClientSecret string

	// ClientCert is the client certificate used to authenticate with the identity provider.
	ClientCert []*x509.Certificate
	// ClientPrivateKey is the private key used to authenticate with the identity provider.
	ClientPrivateKey crypto.PrivateKey

	// Scopes is the list of scopes used to request a token from the identity provider.
	Scopes []string

	// Authority is the authority used to authenticate with the identity provider.
	Authority AuthorityConfiguration

	// confidentialCredFactory is a factory for creating the confidential credential.
	// This is used for testing purposes, to allow mocking the credential creation.
	confidentialCredFactory confidentialCredFactory
}

// ConfidentialIdentityProvider represents a confidential identity provider.
type ConfidentialIdentityProvider struct {
	// clientID is the client ID used to authenticate with the identity provider.
	clientID string

	// credential is the credential used to authenticate with the identity provider.
	credential confidential.Credential

	// scopes is the list of scopes used to request a token from the identity provider.
	scopes []string

	// client confidential is the client used to request a token from the identity provider.
	client confidentialTokenClient
}

// confidentialCredFacotory is a factory for creating the confidential credential.
// Introduced for testing purposes. This allows mocking the credential creation, default behavior is to use the confidential.NewCredFromSecret and confidential.NewCredFromCert methods.
type confidentialCredFactory interface {
	NewCredFromSecret(clientSecret string) (confidential.Credential, error)
	NewCredFromCert(clientCert []*x509.Certificate, clientPrivateKey crypto.PrivateKey) (confidential.Credential, error)
}

// confidentialTokenClient is an interface that defines the methods for a confidential token client.
// It is used to acquire a token using the client credentials.
// Introduced for testing purposes. This allows mocking the token client, default behavior is to use the
// client returned by confidential.New method.
type confidentialTokenClient interface {
	// AcquireTokenByCredential acquires a token using the client credentials.
	// It returns the token and an error if any.
	AcquireTokenByCredential(ctx context.Context, scopes []string, opts ...confidential.AcquireByCredentialOption) (confidential.AuthResult, error)
}

type defaultConfidentialCredFactory struct{}

func (d *defaultConfidentialCredFactory) NewCredFromSecret(clientSecret string) (confidential.Credential, error) {
	return confidential.NewCredFromSecret(clientSecret)
}

func (d *defaultConfidentialCredFactory) NewCredFromCert(clientCert []*x509.Certificate, clientPrivateKey crypto.PrivateKey) (confidential.Credential, error) {
	return confidential.NewCredFromCert(clientCert, clientPrivateKey)
}

// NewConfidentialIdentityProvider creates a new confidential identity provider.
// It is used to configure the identity provider when requesting a token.
// It is used to specify the client ID, tenant ID, and scopes for the identity.
// It is also used to specify the type of credentials used to authenticate with the identity provider.
// The credentials can be either a client secret or a client certificate.
// The authority is used to authenticate with the identity provider.
func NewConfidentialIdentityProvider(opts ConfidentialIdentityProviderOptions) (*ConfidentialIdentityProvider, error) {
	var credential confidential.Credential
	var credFactory confidentialCredFactory
	var authority string
	var err error

	if opts.ClientID == "" {
		return nil, fmt.Errorf("client ID is required")
	}

	if opts.CredentialsType != ClientSecretCredentialType && opts.CredentialsType != ClientCertificateCredentialType {
		return nil, fmt.Errorf("invalid credentials type")
	}

	// Get the authority from the authority configuration.
	authority, err = opts.Authority.getAuthority()
	if err != nil {
		return nil, fmt.Errorf("failed to get authority: %w", err)
	}

	credFactory = &defaultConfidentialCredFactory{}
	if opts.confidentialCredFactory != nil {
		credFactory = opts.confidentialCredFactory
	}

	switch opts.CredentialsType {
	case ClientSecretCredentialType:
		// ClientSecretCredentialType is the type of credentials that uses a client secret to authenticate.
		if opts.ClientSecret == "" {
			return nil, fmt.Errorf("client secret is required when using client secret credentials")
		}

		credential, err = credFactory.NewCredFromSecret(opts.ClientSecret)
		if err != nil {
			return nil, fmt.Errorf("failed to create client secret credential: %w", err)
		}
	case ClientCertificateCredentialType:
		// ClientCertificateCredentialType is the type of credentials that uses a client certificate to authenticate.
		if len(opts.ClientCert) == 0 {
			return nil, fmt.Errorf("non-empty client certificate is required when using client certificate credentials")
		}
		if opts.ClientPrivateKey == nil {
			return nil, fmt.Errorf("client private key is required when using client certificate credentials")
		}
		credential, err = credFactory.NewCredFromCert(opts.ClientCert, opts.ClientPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create client certificate credential: %w", err)
		}
	}

	client, err := confidential.New(authority, opts.ClientID, credential)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	if opts.Scopes == nil {
		opts.Scopes = []string{RedisScopeDefault}
	}

	return &ConfidentialIdentityProvider{
		clientID:   opts.ClientID,
		credential: credential,
		scopes:     opts.Scopes,
		client:     &client,
	}, nil
}

// RequestToken requests a token from the identity provider.
// It returns the identity provider response, including the auth result.
func (c *ConfidentialIdentityProvider) RequestToken(ctx context.Context) (shared.IdentityProviderResponse, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client is not initialized")
	}

	result, err := c.client.AcquireTokenByCredential(ctx, c.scopes)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire token: %w", err)
	}

	return shared.NewIDPResponse(shared.ResponseTypeAuthResult, &result)
}
