package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/redis/go-redis-entraid/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNewTokenCredentialIdentityProvider(t *testing.T) {
	t.Parallel()

	t.Run("requires NewCredential", func(t *testing.T) {
		t.Parallel()
		provider, err := NewTokenCredentialIdentityProvider(TokenCredentialIdentityProviderOptions{})
		assert.ErrorIs(t, err, ErrNewCredentialRequired)
		assert.Nil(t, provider)
	})

	t.Run("defaults the scope", func(t *testing.T) {
		t.Parallel()
		provider, err := NewTokenCredentialIdentityProvider(TokenCredentialIdentityProviderOptions{
			NewCredential: func() (azcore.TokenCredential, error) { return &mockAzureCredential{}, nil },
		})
		assert.NoError(t, err)
		assert.Equal(t, []string{RedisScopeDefault}, provider.scopes)
	})
}

func TestTokenCredentialIdentityProvider_RequestToken(t *testing.T) {
	t.Parallel()

	t.Run("returns the access token for the configured scopes", func(t *testing.T) {
		t.Parallel()
		scopes := []string{"https://example.com/.default"}
		cred := &mockAzureCredential{}
		cred.On("GetToken", mock.Anything, policy.TokenRequestOptions{Scopes: scopes}).
			Return(azcore.AccessToken{Token: testJWTToken}, nil)
		calls := 0
		provider, err := NewTokenCredentialIdentityProvider(TokenCredentialIdentityProviderOptions{
			NewCredential: func() (azcore.TokenCredential, error) {
				calls++
				return cred, nil
			},
			Scopes: scopes,
		})
		assert.NoError(t, err)

		for range 2 {
			resp, err := provider.RequestToken(context.Background())
			assert.NoError(t, err)
			assert.Equal(t, shared.ResponseTypeAccessToken, resp.Type())
			token, err := resp.(shared.AccessTokenIDPResponse).AccessToken()
			assert.NoError(t, err)
			assert.Equal(t, testJWTToken, token.Token)
		}
		assert.Equal(t, 2, calls, "NewCredential should be called on every token request")
		cred.AssertExpectations(t)
	})

	t.Run("returns an error when NewCredential fails", func(t *testing.T) {
		t.Parallel()
		provider, err := NewTokenCredentialIdentityProvider(TokenCredentialIdentityProviderOptions{
			NewCredential: func() (azcore.TokenCredential, error) { return nil, errors.New("no federated token file") },
		})
		assert.NoError(t, err)

		resp, err := provider.RequestToken(context.Background())
		assert.Nil(t, resp)
		assert.ErrorContains(t, err, "failed to create token credential: no federated token file")
	})

	t.Run("returns an error when GetToken fails", func(t *testing.T) {
		t.Parallel()
		cred := &mockAzureCredential{}
		cred.On("GetToken", mock.Anything, mock.Anything).Return(nil, errors.New("token request failed"))
		provider, err := NewTokenCredentialIdentityProvider(TokenCredentialIdentityProviderOptions{
			NewCredential: func() (azcore.TokenCredential, error) { return cred, nil },
		})
		assert.NoError(t, err)

		resp, err := provider.RequestToken(context.Background())
		assert.Nil(t, resp)
		assert.ErrorContains(t, err, "failed to get token: token request failed")
	})
}
