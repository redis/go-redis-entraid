package identity

import (
	"context"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/redis/go-redis-entraid/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNewDefaultAzureIdentityProvider(t *testing.T) {
	t.Parallel()
	// Create a new DefaultAzureIdentityProvider with default options
	provider, err := NewDefaultAzureIdentityProvider(DefaultAzureIdentityProviderOptions{})
	if err != nil {
		t.Fatalf("failed to create DefaultAzureIdentityProvider: %v", err)
	}

	// Check if the provider is not nil
	if provider == nil {
		t.Fatal("provider should not be nil")
	}

	if provider.scopes == nil {
		t.Fatal("provider.scopes should not be nil")
	}

	assert.Contains(t, provider.scopes, RedisScopeDefault, "provider should contain default scope")
}
func TestAzureDefaultIdentityProvider_RequestToken(t *testing.T) {
	t.Parallel()
	provider, err := NewDefaultAzureIdentityProvider(DefaultAzureIdentityProviderOptions{})
	if err != nil {
		t.Fatalf("failed to create DefaultAzureIdentityProvider: %v", err)
	}

	// Request a token from the provider in incorrect environment
	// should fail.
	token, err := provider.RequestToken(context.Background())
	assert.Nil(t, token, "token should be nil")
	assert.Error(t, err, "failed to request token")

	// use mockAzureCredential to simulate the environment
	mToken := azcore.AccessToken{
		Token: testJWTToken,
	}
	mCreds := &mockAzureCredential{}
	mCreds.On("GetToken", mock.Anything, mock.Anything).Return(mToken, nil)
	mCredFactory := &mockCredFactory{}
	mCredFactory.On("NewDefaultAzureCredential", mock.Anything).Return(mCreds, nil)
	provider.credFactory = mCredFactory
	resp, err := provider.RequestToken(context.Background())
	assert.NotNil(t, resp, "resp should not be nil")
	assert.NoError(t, err, "failed to request resp")
	assert.Equal(t, shared.ResponseTypeAccessToken, resp.Type(), "resp type should be access resp")
	atoken, err := resp.(shared.AccessTokenIDPResponse).AccessToken()
	assert.NotNil(t, atoken, "token should not be nil")
	assert.NoError(t, err, "failed to get token")
	assert.Equal(t, mToken, atoken, "access resp should be equal to testJWTToken")
}

func TestAzureDefaultIdentityProvider_RequestTokenWithScopes(t *testing.T) {
	// Create a new DefaultAzureIdentityProvider with custom scopes
	scopes := []string{"https://example.com/.default"}
	provider, err := NewDefaultAzureIdentityProvider(DefaultAzureIdentityProviderOptions{
		Scopes: scopes,
	})
	if err != nil {
		t.Fatalf("failed to create DefaultAzureIdentityProvider: %v", err)
	}

	t.Run("RequestToken with custom scopes", func(t *testing.T) {
		// Request a token from the provider
		token, err := provider.RequestToken(context.Background())
		assert.Nil(t, token, "token should be nil")
		assert.Error(t, err, "failed to request token")

		// use mockAzureCredential to simulate the environment
		mToken := azcore.AccessToken{
			Token: testJWTToken,
		}
		mCreds := &mockAzureCredential{}
		mCreds.On("GetToken", mock.Anything, policy.TokenRequestOptions{Scopes: scopes}).Return(mToken, nil)
		mCredFactory := &mockCredFactory{}
		mCredFactory.On("NewDefaultAzureCredential", mock.Anything).Return(mCreds, nil)
		provider.credFactory = mCredFactory
		resp, err := provider.RequestToken(context.Background())
		assert.NotNil(t, resp, "resp should not be nil")
		assert.NoError(t, err, "failed to request resp")
		assert.Equal(t, shared.ResponseTypeAccessToken, resp.Type(), "resp type should be access resp")
		atoken, err := resp.(shared.AccessTokenIDPResponse).AccessToken()
		assert.NotNil(t, atoken, "token should not be nil")
		assert.NoError(t, err, "failed to get token")
		assert.Equal(t, mToken, atoken, "access resp should be equal to testJWTToken")
	})
	t.Run("RequestToken with error from credFactory", func(t *testing.T) {
		// use mockAzureCredential to simulate the environment
		mCredFactory := &mockCredFactory{}
		mCredFactory.On("NewDefaultAzureCredential", mock.Anything).Return(nil, assert.AnError)
		provider.credFactory = mCredFactory
		resp, err := provider.RequestToken(context.Background())
		assert.Nil(t, resp, "resp should be nil")
		assert.Error(t, err, "failed to request resp")
	})
}
