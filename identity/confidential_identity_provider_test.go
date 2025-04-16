package identity

import (
	"context"
	"crypto/x509"
	"fmt"
	"testing"
	"time"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/confidential"
	"github.com/redis-developer/go-redis-entraid/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNewConfidentialIdentityProvider(t *testing.T) {
	t.Run("base", func(t *testing.T) {
		t.Parallel()
		opts := ConfidentialIdentityProviderOptions{
			ClientID:        "client-id",
			CredentialsType: "ClientSecret",
			ClientSecret:    "client-secret",
			Scopes:          []string{"scope1", "scope2"},
			Authority:       AuthorityConfiguration{},
		}
		provider, err := NewConfidentialIdentityProvider(opts)
		if err != nil {
			t.Errorf("NewConfidentialIdentityProvider() error = %v", err)
			return
		}
		if provider == nil {
			t.Errorf("NewConfidentialIdentityProvider() provider = nil")
			return
		}
	})

	t.Run("with client certificate", func(t *testing.T) {
		t.Parallel()
		credFactory := &mockConfidentialCredentialFactory{}
		opts := ConfidentialIdentityProviderOptions{
			ClientID:                "client-id",
			CredentialsType:         "ClientCertificate",
			ClientCert:              []*x509.Certificate{},
			ClientPrivateKey:        "private-key",
			Scopes:                  []string{"scope1", "scope2"},
			Authority:               AuthorityConfiguration{},
			confidentialCredFactory: credFactory,
		}
		credFactory.On("NewCredFromCert", opts.ClientCert, opts.ClientPrivateKey).Return(confidential.Credential{}, nil)
		provider, err := NewConfidentialIdentityProvider(opts)
		// confidential.New will fail since the credentials are invalid
		assert.ErrorContains(t, err, "failed to create client:")
		assert.Nil(t, provider)
	})

	t.Run("with failing client certificate", func(t *testing.T) {
		t.Parallel()
		opts := ConfidentialIdentityProviderOptions{
			ClientID:         "client-id",
			CredentialsType:  "ClientCertificate",
			ClientCert:       []*x509.Certificate{},
			ClientPrivateKey: "private-key",
			Scopes:           []string{"scope1", "scope2"},
			Authority:        AuthorityConfiguration{},
		}
		// invalid certificate should fail
		provider, err := NewConfidentialIdentityProvider(opts)
		assert.ErrorContains(t, err, "failed to create client certificate credential:")
		assert.Nil(t, provider)
	})

	t.Run("with invalid credentials type", func(t *testing.T) {
		t.Parallel()
		opts := ConfidentialIdentityProviderOptions{
			ClientID:        "client-id",
			CredentialsType: "invalid-credentials-type",
			ClientSecret:    "client-secret",
			Scopes:          []string{"scope1", "scope2"},
			Authority:       AuthorityConfiguration{},
		}
		provider, err := NewConfidentialIdentityProvider(opts)
		if err == nil {
			t.Errorf("NewConfidentialIdentityProvider() error = nil, want error")
			return
		}
		if provider != nil {
			t.Errorf("NewConfidentialIdentityProvider() provider = %v, want nil", provider)
			return
		}
	})

	t.Run("with missing client id", func(t *testing.T) {
		t.Parallel()
		opts := ConfidentialIdentityProviderOptions{
			CredentialsType: "ClientSecret",
		}
		provider, err := NewConfidentialIdentityProvider(opts)
		if err == nil {
			t.Errorf("NewConfidentialIdentityProvider() error = nil, want error")
			return
		}
		if provider != nil {
			t.Errorf("NewConfidentialIdentityProvider() provider = %v, want nil", provider)
			return
		}
	})

	t.Run("with bad authority type", func(t *testing.T) {
		t.Parallel()
		opts := ConfidentialIdentityProviderOptions{
			ClientID:        "client-id",
			CredentialsType: "ClientSecret",
			ClientSecret:    "client-secret",
			Scopes:          []string{"scope1", "scope2"},
			Authority:       AuthorityConfiguration{AuthorityType: "bad-authority-type"},
		}
		provider, err := NewConfidentialIdentityProvider(opts)
		if err == nil {
			t.Errorf("NewConfidentialIdentityProvider() error = nil, want error")
			return
		}
		if provider != nil {
			t.Errorf("NewConfidentialIdentityProvider() provider = %v, want nil", provider)
			return
		}
	})
	t.Run("with missing client secret", func(t *testing.T) {
		t.Parallel()
		opts := ConfidentialIdentityProviderOptions{
			ClientID:        "client-id",
			CredentialsType: "ClientSecret",
			Scopes:          []string{"scope1", "scope2"},
		}
		provider, err := NewConfidentialIdentityProvider(opts)
		if err == nil {
			t.Errorf("NewConfidentialIdentityProvider() error = nil, want error")
			return
		}
		if provider != nil {
			t.Errorf("NewConfidentialIdentityProvider() provider = %v, want nil", provider)
			return
		}
	})

	t.Run("with credentials from secret error", func(t *testing.T) {
		t.Parallel()
		credFactory := &mockConfidentialCredentialFactory{}
		opts := ConfidentialIdentityProviderOptions{
			ClientID:                "client-id",
			CredentialsType:         "ClientSecret",
			ClientSecret:            "client-secret",
			Scopes:                  []string{"scope1", "scope2"},
			Authority:               AuthorityConfiguration{},
			confidentialCredFactory: credFactory,
		}
		credFactory.On("NewCredFromSecret", "client-secret").Return(confidential.Credential{}, fmt.Errorf("error creating credential"))
		provider, err := NewConfidentialIdentityProvider(opts)
		if err == nil {
			t.Errorf("NewConfidentialIdentityProvider() error = nil, want error")
			return
		}
		if provider != nil {
			t.Errorf("NewConfidentialIdentityProvider() provider = %v, want nil", provider)
			return
		}
		credFactory.AssertExpectations(t)
	})

	t.Run("empty certificate", func(t *testing.T) {
		t.Parallel()
		opts := ConfidentialIdentityProviderOptions{
			ClientID:         "client-id",
			CredentialsType:  "ClientCertificate",
			ClientCert:       nil,
			ClientPrivateKey: "private key",
			Scopes:           []string{"scope1", "scope2"},
			Authority:        AuthorityConfiguration{},
		}
		provider, err := NewConfidentialIdentityProvider(opts)
		if err == nil {
			t.Errorf("NewConfidentialIdentityProvider() error = nil, want error")
			return
		}
		if provider != nil {
			t.Errorf("NewConfidentialIdentityProvider() provider = %v, want nil", provider)
			return
		}
	})

	t.Run("empty private key", func(t *testing.T) {
		t.Parallel()
		opts := ConfidentialIdentityProviderOptions{
			ClientID:         "client-id",
			CredentialsType:  "ClientCertificate",
			ClientCert:       []*x509.Certificate{},
			ClientPrivateKey: nil,
			Scopes:           []string{"scope1", "scope2"},
			Authority:        AuthorityConfiguration{},
		}
		provider, err := NewConfidentialIdentityProvider(opts)
		if err == nil {
			t.Errorf("NewConfidentialIdentityProvider() error = nil, want error")
			return
		}
		if provider != nil {
			t.Errorf("NewConfidentialIdentityProvider() provider = %v, want nil", provider)
			return
		}
	})
	t.Run("validate default scopes", func(t *testing.T) {
		t.Parallel()
		opts := ConfidentialIdentityProviderOptions{
			ClientID:        "client-id",
			CredentialsType: "ClientSecret",
			ClientSecret:    "client-secret",
			Authority:       AuthorityConfiguration{},
		}
		provider, err := NewConfidentialIdentityProvider(opts)
		if err != nil {
			t.Errorf("NewConfidentialIdentityProvider() error = %v", err)
			return
		}
		if provider == nil {
			t.Errorf("NewConfidentialIdentityProvider() provider = nil")
			return
		}
		if len(provider.scopes) == 0 {
			t.Errorf("NewConfidentialIdentityProvider() provider.Scopes = %v, want non-empty", provider.scopes)
			return
		}
		assert.Contains(t, provider.scopes, RedisScopeDefault)
	})
}

func TestConfidentialIdentityProvider_RequestToken(t *testing.T) {
	t.Run("with mock client", func(t *testing.T) {
		t.Parallel()
		mClient := &mockConfidentialTokenClient{}

		opts := ConfidentialIdentityProviderOptions{
			ClientID:        "client-id",
			CredentialsType: "ClientSecret",
			ClientSecret:    "client-secret",
			Authority: AuthorityConfiguration{
				AuthorityType: AuthorityTypeCustom,
				Authority:     "https://test-authority.dev/test",
			},
		}
		provider, err := NewConfidentialIdentityProvider(opts)
		if err != nil {
			t.Errorf("NewConfidentialIdentityProvider() error = %v", err)
			return
		}
		if provider == nil {
			t.Errorf("NewConfidentialIdentityProvider() provider = nil")
			return
		}
		expiresOn := time.Now().Add(time.Hour)
		provider.client = mClient
		mClient.On("AcquireTokenByCredential", mock.Anything, mock.Anything).
			Return(confidential.AuthResult{
				ExpiresOn: expiresOn,
			}, nil)
		token, err := provider.RequestToken(context.Background())
		if err != nil {
			t.Errorf("RequestToken() error = %v", err)
			return
		}
		assert.NotEmpty(t, token, "RequestToken() token should not be empty")
		assert.Equal(t, token.Type(), shared.ResponseTypeAuthResult, "RequestToken() token type should be AuthResult")
		assert.Equal(t, token.AuthResult().ExpiresOn, expiresOn, "RequestToken() token expiration should match")
	})
	t.Run("with error", func(t *testing.T) {
		t.Parallel()
		mClient := &mockConfidentialTokenClient{}

		opts := ConfidentialIdentityProviderOptions{
			ClientID:        "client-id",
			CredentialsType: "ClientSecret",
			ClientSecret:    "client-secret",
			Authority: AuthorityConfiguration{
				AuthorityType: AuthorityTypeCustom,
				Authority:     "https://test-authority.dev/test",
			},
		}
		provider, err := NewConfidentialIdentityProvider(opts)
		if err != nil {
			t.Errorf("NewConfidentialIdentityProvider() error = %v", err)
			return
		}
		if provider == nil {
			t.Errorf("NewConfidentialIdentityProvider() provider = nil")
			return
		}
		provider.client = mClient
		mClient.On("AcquireTokenByCredential", mock.Anything, mock.Anything).
			Return(confidential.AuthResult{}, fmt.Errorf("error acquiring token"))
		token, err := provider.RequestToken(context.Background())
		assert.ErrorContains(t, err, "failed to acquire token:")
		assert.Empty(t, token, "RequestToken() token should be empty")
	})
	t.Run("without initialization", func(t *testing.T) {
		t.Parallel()
		provider := &ConfidentialIdentityProvider{}
		token, err := provider.RequestToken(context.Background())
		assert.ErrorContains(t, err, "client is not initialized")
		assert.Empty(t, token, "RequestToken() token should be empty")
	})
}
