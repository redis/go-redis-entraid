package entraid

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/redis/go-redis-entraid/identity"
	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis-entraid/shared"
	"github.com/redis/go-redis-entraid/token"
	"github.com/stretchr/testify/assert"
)

type fakeTokenCredential struct{}

func (fakeTokenCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: rawTokenString, ExpiresOn: time.Now().Add(time.Hour)}, nil
}

func newFakeTokenCredential() (azcore.TokenCredential, error) {
	return fakeTokenCredential{}, nil
}

func TestNewTokenCredentialCredentialsProvider(t *testing.T) {
	t.Run("valid options", func(t *testing.T) {
		options := TokenCredentialCredentialsProviderOptions{
			CredentialsProviderOptions: CredentialsProviderOptions{
				TokenManagerOptions: manager.TokenManagerOptions{
					ExpirationRefreshRatio: 0.7,
				},
			},
			TokenCredentialIdentityProviderOptions: identity.TokenCredentialIdentityProviderOptions{
				NewCredential: newFakeTokenCredential,
				Scopes:        []string{identity.RedisScopeDefault},
			},
		}
		testToken := token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(time.Hour),
			time.Now(),
			time.Hour.Milliseconds(),
		)
		options.tokenManagerFactory = testFakeTokenManagerFactory(testToken, nil)

		provider, err := NewTokenCredentialCredentialsProvider(options)
		assert.NoError(t, err)
		assert.NotNil(t, provider)

		listener := &mockCredentialsListener{LastTokenCh: make(chan string)}
		tk, cancel, err := provider.Subscribe(listener)
		assert.NoError(t, err)
		defer func() {
			assert.NoError(t, cancel())
		}()
		assert.Equal(t, rawTokenString, tk.RawCredentials())
	})

	t.Run("requires NewCredential", func(t *testing.T) {
		provider, err := NewTokenCredentialCredentialsProvider(TokenCredentialCredentialsProviderOptions{})
		assert.ErrorIs(t, err, identity.ErrNewCredentialRequired)
		assert.Nil(t, provider)
	})
}

func TestNewTokenCredentialCredentialsProvider_TokenManagerFactoryError(t *testing.T) {
	options := TokenCredentialCredentialsProviderOptions{
		TokenCredentialIdentityProviderOptions: identity.TokenCredentialIdentityProviderOptions{
			NewCredential: newFakeTokenCredential,
		},
	}
	options.tokenManagerFactory = func(shared.IdentityProvider, manager.TokenManagerOptions) (manager.TokenManager, error) {
		return nil, fmt.Errorf("token manager factory error")
	}

	provider, err := NewTokenCredentialCredentialsProvider(options)
	assert.ErrorContains(t, err, "token manager factory error")
	assert.Nil(t, provider)
}
