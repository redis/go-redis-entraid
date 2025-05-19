package entraid

import (
	"sync"
	"testing"
	"time"

	"github.com/redis-developer/go-redis-entraid/identity"
	"github.com/redis-developer/go-redis-entraid/manager"
	"github.com/redis-developer/go-redis-entraid/shared"
	"github.com/redis-developer/go-redis-entraid/token"
	"github.com/redis/go-redis/v9/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCredentialsProviderErrorScenarios(t *testing.T) {
	t.Run("token manager start error", func(t *testing.T) {
		// Create a test provider with invalid options
		options := ConfidentialCredentialsProviderOptions{
			CredentialsProviderOptions: CredentialsProviderOptions{
				ClientID: "test-client-id",
				TokenManagerOptions: manager.TokenManagerOptions{
					ExpirationRefreshRatio: 0.7,
				},
			},
			ConfidentialIdentityProviderOptions: identity.ConfidentialIdentityProviderOptions{
				ClientID:        "test-client-id",
				CredentialsType: "invalid-type", // Invalid credentials type
				ClientSecret:    "test-secret",
				Scopes:          []string{identity.RedisScopeDefault},
				Authority:       identity.AuthorityConfiguration{},
			},
		}

		provider, err := NewConfidentialCredentialsProvider(options)
		assert.Error(t, err)
		assert.Nil(t, provider)
	})

	t.Run("token manager get token error", func(t *testing.T) {
		// Create a test provider with invalid options
		options := ConfidentialCredentialsProviderOptions{
			CredentialsProviderOptions: CredentialsProviderOptions{
				ClientID: "test-client-id",
				TokenManagerOptions: manager.TokenManagerOptions{
					ExpirationRefreshRatio: 0.7,
				},
			},
			ConfidentialIdentityProviderOptions: identity.ConfidentialIdentityProviderOptions{
				ClientID:        "test-client-id",
				CredentialsType: identity.ClientSecretCredentialType,
				ClientSecret:    "", // Empty client secret
				Scopes:          []string{identity.RedisScopeDefault},
				Authority:       identity.AuthorityConfiguration{},
			},
		}

		provider, err := NewConfidentialCredentialsProvider(options)
		assert.Error(t, err)
		assert.Nil(t, provider)
	})

	t.Run("concurrent error handling", func(t *testing.T) {
		// Create a test provider with invalid options
		options := ManagedIdentityCredentialsProviderOptions{
			CredentialsProviderOptions: CredentialsProviderOptions{
				ClientID: "test-client-id",
				TokenManagerOptions: manager.TokenManagerOptions{
					ExpirationRefreshRatio: 0.7,
				},
			},
			ManagedIdentityProviderOptions: identity.ManagedIdentityProviderOptions{
				ManagedIdentityType: "invalid-type", // Invalid managed identity type
				Scopes:              []string{identity.RedisScopeDefault},
			},
		}

		provider, err := NewManagedIdentityCredentialsProvider(options)
		assert.Error(t, err)
		assert.Nil(t, provider)
	})

	t.Run("concurrent token updates", func(t *testing.T) {
		// Create a test provider with invalid options
		options := DefaultAzureCredentialsProviderOptions{
			CredentialsProviderOptions: CredentialsProviderOptions{
				ClientID: "test-client-id",
				TokenManagerOptions: manager.TokenManagerOptions{
					ExpirationRefreshRatio: 0.7,
				},
			},
			DefaultAzureIdentityProviderOptions: identity.DefaultAzureIdentityProviderOptions{
				Scopes: []string{}, // Empty scopes
			},
		}

		provider, err := NewDefaultAzureCredentialsProvider(options)
		// bad options - empty scopes
		assert.Error(t, err)
		assert.Nil(t, provider)
	})
}

func TestCredentialsProviderWithMockIdentityProvider(t *testing.T) {
	t.Parallel()

	t.Run("Subscribe and Unsubscribe", func(t *testing.T) {
		t.Parallel()

		// Create mock token manager
		tm := &fakeTokenManager{
			token: token.New(
				"test",
				"test",
				"test-token",
				time.Now().Add(time.Hour),
				time.Now(),
				int64(time.Hour),
			),
		}

		// Create credentials provider
		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		assert.NoError(t, err)
		assert.NotNil(t, cp)

		// Create mock listener
		listener := &mockCredentialsListener{
			LastTokenCh: make(chan string),
			LastErrCh:   make(chan error),
		}

		// Subscribe listener
		credentials, cancel, err := cp.Subscribe(listener)
		assert.NoError(t, err)
		assert.NotNil(t, credentials)
		assert.NotNil(t, cancel)

		// Wait for initial token
		tk, err := listener.readWithTimeout(time.Second)
		assert.NoError(t, err)
		assert.Equal(t, "test-token", tk)

		// Unsubscribe
		err = cancel()
		assert.NoError(t, err)
	})

	t.Run("Multiple Listeners", func(t *testing.T) {
		t.Parallel()

		// Create mock token manager
		tm := &fakeTokenManager{
			token: token.New(
				"test",
				"test",
				"test-token",
				time.Now().Add(time.Hour),
				time.Now(),
				int64(time.Hour),
			),
		}

		// Create credentials provider
		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		assert.NoError(t, err)
		assert.NotNil(t, cp)

		// Create multiple mock listeners
		listener1 := &mockCredentialsListener{
			LastTokenCh: make(chan string),
			LastErrCh:   make(chan error),
		}
		listener2 := &mockCredentialsListener{
			LastTokenCh: make(chan string),
			LastErrCh:   make(chan error),
		}

		// Subscribe first listener
		credentials1, cancel1, err := cp.Subscribe(listener1)
		assert.NoError(t, err)
		assert.NotNil(t, credentials1)
		assert.NotNil(t, cancel1)

		// Subscribe second listener
		credentials2, cancel2, err := cp.Subscribe(listener2)
		assert.NoError(t, err)
		assert.NotNil(t, credentials2)
		assert.NotNil(t, cancel2)

		// Wait for initial tokens
		token1, err := listener1.readWithTimeout(time.Second)
		assert.NoError(t, err)
		assert.Equal(t, "test-token", token1)

		token2, err := listener2.readWithTimeout(time.Second)
		assert.NoError(t, err)
		assert.Equal(t, "test-token", token2)

		// Unsubscribe first listener
		err = cancel1()
		assert.NoError(t, err)

		// Unsubscribe second listener
		err = cancel2()
		assert.NoError(t, err)
	})

	t.Run("Token Updates", func(t *testing.T) {
		t.Parallel()

		// Create mock token manager
		tm := &fakeTokenManager{
			token: token.New(
				"test",
				"test",
				"initial-token",
				time.Now().Add(time.Hour),
				time.Now(),
				int64(time.Hour),
			),
		}

		// Create credentials provider
		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		assert.NoError(t, err)
		assert.NotNil(t, cp)

		// Create mock listener
		listener := &mockCredentialsListener{
			LastTokenCh: make(chan string),
			LastErrCh:   make(chan error),
		}

		// Subscribe listener
		credentials, cancel, err := cp.Subscribe(listener)
		assert.NoError(t, err)
		assert.NotNil(t, credentials)
		assert.NotNil(t, cancel)

		// Wait for initial token
		tk, err := listener.readWithTimeout(time.Second)
		assert.NoError(t, err)
		assert.Equal(t, "initial-token", tk)

		tm.lock.Lock()
		// Update token
		tm.token = token.New(
			"test",
			"test",
			"updated-token",
			time.Now().Add(time.Hour),
			time.Now(),
			int64(time.Hour),
		)
		tm.lock.Unlock()

		// Wait for token update
		tk, err = listener.readWithTimeout(time.Second)
		assert.NoError(t, err)
		assert.Equal(t, "updated-token", tk)

		// Unsubscribe
		err = cancel()
		assert.NoError(t, err)
	})

	t.Run("Error Handling", func(t *testing.T) {
		t.Parallel()

		// Create mock token manager with error
		tm := &fakeTokenManager{
			err: assert.AnError,
		}

		// Create credentials provider
		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		assert.Error(t, err)
		assert.Nil(t, cp)
	})
}

func TestCredentialsProviderOptions(t *testing.T) {
	t.Run("default token manager factory", func(t *testing.T) {
		options := CredentialsProviderOptions{}
		factory := options.getTokenManagerFactory()
		assert.NotNil(t, factory)
	})

	t.Run("custom token manager factory", func(t *testing.T) {
		m := &fakeTokenManager{}
		customFactory := func(shared.IdentityProvider, manager.TokenManagerOptions) (manager.TokenManager, error) {
			return m, nil
		}
		options := CredentialsProviderOptions{
			tokenManagerFactory: customFactory,
		}
		tm, err := options.getTokenManagerFactory()(nil, manager.TokenManagerOptions{})
		assert.NotNil(t, tm)
		assert.NoError(t, err)
		assert.Equal(t, m, tm)
	})
}

func TestCredentialsProviderSubscribe(t *testing.T) {
	// Create a test provider
	opts := ConfidentialCredentialsProviderOptions{
		CredentialsProviderOptions: CredentialsProviderOptions{
			ClientID: "test-client-id",
			TokenManagerOptions: manager.TokenManagerOptions{
				ExpirationRefreshRatio: 0.7,
			},
		},
		ConfidentialIdentityProviderOptions: identity.ConfidentialIdentityProviderOptions{
			ClientID:        "test-client-id",
			CredentialsType: identity.ClientSecretCredentialType,
			ClientSecret:    "test-secret",
			Scopes:          []string{identity.RedisScopeDefault},
			Authority:       identity.AuthorityConfiguration{},
		},
	}
	t.Run("double subscribe and cancel resubscribe", func(t *testing.T) {
		t.Parallel()
		testToken := token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(tokenExpiration),
			time.Now(),
			int64(tokenExpiration),
		)

		listener := &mockCredentialsListener{
			LastTokenCh: make(chan string, 1),
			LastErrCh:   make(chan error, 1),
		}
		mtm := &mockTokenManager{done: make(chan struct{})}
		// Set the token manager factory in the options
		options := opts
		options.tokenManagerFactory = mockTokenManagerFactory(mtm)
		mtm.On("GetToken", false).Return(testToken, nil)
		mtm.On("Start", mock.Anything).
			Run(mockTokenManagerLoop(mtm, tokenExpiration, testToken, nil)).
			Return(manager.StopFunc(mtm.stop), nil)
		provider, err := NewConfidentialCredentialsProvider(options)
		require.NoError(t, err)
		require.NotNil(t, provider)
		// Subscribe the listener
		tk, cancel, err := provider.Subscribe(listener)
		require.NoError(t, err)
		require.NotNil(t, tk)
		require.NotNil(t, cancel)
		// try to subscribe the same listener again
		tk2, cancel2, err := provider.Subscribe(listener)
		require.NoError(t, err)
		require.NotNil(t, tk2)
		require.NotNil(t, cancel2)
		// Verify the listener received the token once
		select {
		case tk := <-listener.LastTokenCh:
			assert.Equal(t, rawTokenString, tk, "listener received wrong token")
		case err := <-listener.LastErrCh:
			t.Fatalf("listener received error: %v", err)
		case <-time.After(3 * tokenExpiration):
			t.Fatalf("listener timed out waiting for token")
		}
		// verify it is not received again
		select {
		case tk := <-listener.LastTokenCh:
			t.Fatalf("listener received unexpected token: %v", tk)
		case err := <-listener.LastErrCh:
			t.Fatalf("listener received unexpected error: %v", err)
		case <-time.After(tokenExpiration / 2):
			// No message received, which is expected
		}

	})

	t.Run("concurrent subscribe and cancel with error ", func(t *testing.T) {
		t.Parallel()
		testToken := token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(tokenExpiration),
			time.Now(),
			int64(tokenExpiration),
		)
		mtm := &mockTokenManager{done: make(chan struct{})}
		// Set the token manager factory in the options
		options := opts
		options.tokenManagerFactory = mockTokenManagerFactory(mtm)
		mtm.On("GetToken", false).Return(testToken, nil)

		mtm.On("Start", mock.Anything).
			Run(mockTokenManagerLoop(mtm, tokenExpiration, nil, errTokenError)).
			Return(manager.StopFunc(mtm.stop), nil)
		provider, err := NewConfidentialCredentialsProvider(options)
		require.NoError(t, err)
		require.NotNil(t, provider)
		var wg sync.WaitGroup
		listeners := make([]*mockCredentialsListener, numListeners)
		cancels := make([]auth.UnsubscribeFunc, numListeners)

		// Subscribe multiple listeners concurrently
		for i := 0; i < numListeners; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				listener := &mockCredentialsListener{
					LastTokenCh: make(chan string, 1),
					LastErrCh:   make(chan error, 1),
				}
				listeners[idx] = listener
				_, cancel, err := provider.Subscribe(listener)
				require.NoError(t, err)
				cancels[idx] = cancel
			}(i)
		}
		wg.Wait()

		// Verify all listeners received the token
		for i, listener := range listeners {
			select {
			case tk := <-listener.LastTokenCh:
				t.Fatalf("listener %d received token: %v", i, tk)
			case err := <-listener.LastErrCh:
				assert.Equal(t, errTokenError.Error(), err.Error(), "listener %d received wrong error", i)
			case <-time.After(3 * tokenExpiration):
				t.Fatalf("listener %d timed out waiting for token", i)
			}
		}

		// Cancel all subscriptions concurrently
		for i := 0; i < numListeners; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				err := cancels[idx]()
				require.NoError(t, err)
			}(i)
		}
		wg.Wait()

		// Verify no more tokens are sent after cancellation
		for i, listener := range listeners {
			select {
			case tk := <-listener.LastTokenCh:
				t.Fatalf("listener %d received unexpected token after cancellation: %s", i, tk)
			case err := <-listener.LastErrCh:
				t.Fatalf("listener %d received unexpected error after cancellation: %v", i, err)
			case <-time.After(3 * tokenExpiration):
				// No message received, which is expected
			}
		}
	})

	t.Run("concurrent subscribe and get token error ", func(t *testing.T) {
		t.Parallel()
		mtm := &mockTokenManager{done: make(chan struct{})}
		// Set the token manager factory in the options
		options := opts
		options.tokenManagerFactory = mockTokenManagerFactory(mtm)
		mtm.On("GetToken", false).Return(nil, assert.AnError)

		mtm.On("Start", mock.Anything).
			Run(mockTokenManagerLoop(mtm, tokenExpiration, nil, errTokenError)).
			Return(manager.StopFunc(mtm.stop), nil)
		provider, err := NewConfidentialCredentialsProvider(options)
		require.NoError(t, err)
		require.NotNil(t, provider)

		var wg sync.WaitGroup
		listeners := make([]*mockCredentialsListener, numListeners)

		// Subscribe multiple listeners concurrently
		for i := 0; i < numListeners; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				listener := &mockCredentialsListener{
					LastTokenCh: make(chan string, 1),
					LastErrCh:   make(chan error, 1),
				}
				listeners[idx] = listener
				tk, cancel, err := provider.Subscribe(listener)
				require.Nil(t, tk)
				require.Error(t, err)
				require.Nil(t, cancel)
			}(i)
		}
		wg.Wait()

		// Verify no more tokens are sent after cancellation
		for i, listener := range listeners {
			select {
			case tk := <-listener.LastTokenCh:
				t.Fatalf("listener %d received unexpected token after cancellation: %s", i, tk)
			case err := <-listener.LastErrCh:
				t.Fatalf("listener %d received unexpected error after cancellation: %v", i, err)
			case <-time.After(3 * tokenExpiration):
				// No message received, which is expected
			}
		}
	})

	t.Run("concurrent subscribe and cancel", func(t *testing.T) {
		t.Parallel()
		testToken := token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(tokenExpiration),
			time.Now(),
			int64(tokenExpiration),
		)
		// Set the token manager factory in the options
		options := opts
		options.tokenManagerFactory = testFakeTokenManagerFactory(testToken, nil)

		provider, err := NewConfidentialCredentialsProvider(options)
		require.NoError(t, err)
		require.NotNil(t, provider)
		var wg sync.WaitGroup
		listeners := make([]*mockCredentialsListener, numListeners)
		cancels := make([]auth.UnsubscribeFunc, numListeners)

		// Subscribe multiple listeners concurrently
		for i := 0; i < numListeners; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				listener := &mockCredentialsListener{
					LastTokenCh: make(chan string, 1),
					LastErrCh:   make(chan error, 1),
				}
				listeners[idx] = listener
				_, cancel, err := provider.Subscribe(listener)
				require.NoError(t, err)
				cancels[idx] = cancel
			}(i)
		}
		wg.Wait()

		// Verify all listeners received the token
		for i, listener := range listeners {
			select {
			case tk := <-listener.LastTokenCh:
				assert.Equal(t, rawTokenString, tk, "listener %d received wrong token", i)
			case err := <-listener.LastErrCh:
				t.Fatalf("listener %d received error: %v", i, err)
			case <-time.After(3 * tokenExpiration):
				t.Fatalf("listener %d timed out waiting for token", i)
			}
		}

		// Cancel all subscriptions concurrently
		for i := 0; i < numListeners; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				err := cancels[idx]()
				require.NoError(t, err)
			}(i)
		}
		wg.Wait()

		// Verify no more tokens are sent after cancellation
		for i, listener := range listeners {
			select {
			case tk := <-listener.LastTokenCh:
				t.Fatalf("listener %d received unexpected token after cancellation: %s", i, tk)
			case err := <-listener.LastErrCh:
				t.Fatalf("listener %d received unexpected error after cancellation: %v", i, err)
			case <-time.After(3 * tokenExpiration):
				// No message received, which is expected
			}
		}
	})
}
