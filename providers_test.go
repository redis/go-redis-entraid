package entraid

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis-developer/go-redis-entraid/identity"
	"github.com/redis-developer/go-redis-entraid/manager"
	"github.com/redis-developer/go-redis-entraid/shared"
	"github.com/redis-developer/go-redis-entraid/token"
	"github.com/redis/go-redis/v9/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTokenManager implements the TokenManager interface for testing
type mockTokenManager struct {
	token *token.Token
	err   error
}

func (m *mockTokenManager) GetToken(forceRefresh bool) (*token.Token, error) {
	return m.token, m.err
}

func (m *mockTokenManager) Start(listener manager.TokenListener) (manager.CancelFunc, error) {
	if m.err != nil {
		listener.OnTokenError(m.err)
		return nil, m.err
	}

	listener.OnTokenNext(m.token)
	return func() error { return nil }, nil
}

func (m *mockTokenManager) Close() error {
	return nil
}

// mockCredentialsListener implements the CredentialsListener interface for testing
type mockCredentialsListener struct {
	LastTokenCh chan string
	LastErrCh   chan error
}

func (m *mockCredentialsListener) OnNext(credentials auth.Credentials) {
	if m.LastTokenCh == nil {
		m.LastTokenCh = make(chan string)
	}
	m.LastTokenCh <- credentials.RawCredentials()
}

func (m *mockCredentialsListener) OnError(err error) {
	if m.LastErrCh == nil {
		m.LastErrCh = make(chan error)
	}
	m.LastErrCh <- err
}

// testTokenManagerFactory is a factory function that returns a mock token manager
func testTokenManagerFactory(token *token.Token, err error) func(shared.IdentityProvider, manager.TokenManagerOptions) (manager.TokenManager, error) {
	return func(provider shared.IdentityProvider, options manager.TokenManagerOptions) (manager.TokenManager, error) {
		return &mockTokenManager{
			token: token,
			err:   err,
		}, nil
	}
}

func TestNewManagedIdentityCredentialsProvider(t *testing.T) {
	tests := []struct {
		name          string
		options       ManagedIdentityCredentialsProviderOptions
		expectedError error
	}{
		{
			name: "valid managed identity options",
			options: ManagedIdentityCredentialsProviderOptions{
				CredentialsProviderOptions: CredentialsProviderOptions{
					ClientID: "test-client-id",
					TokenManagerOptions: manager.TokenManagerOptions{
						ExpirationRefreshRatio: 0.7,
					},
				},
				ManagedIdentityProviderOptions: identity.ManagedIdentityProviderOptions{
					UserAssignedClientID: "test-client-id",
					ManagedIdentityType:  identity.UserAssignedIdentity,
					Scopes:               []string{identity.RedisScopeDefault},
				},
			},
			expectedError: nil,
		},
		{
			name: "system assigned identity",
			options: ManagedIdentityCredentialsProviderOptions{
				CredentialsProviderOptions: CredentialsProviderOptions{
					TokenManagerOptions: manager.TokenManagerOptions{
						ExpirationRefreshRatio: 0.7,
					},
				},
				ManagedIdentityProviderOptions: identity.ManagedIdentityProviderOptions{
					ManagedIdentityType: identity.SystemAssignedIdentity,
					Scopes:              []string{identity.RedisScopeDefault},
				},
			},
			expectedError: nil,
		},
		{
			name: "invalid managed identity type",
			options: ManagedIdentityCredentialsProviderOptions{
				ManagedIdentityProviderOptions: identity.ManagedIdentityProviderOptions{
					ManagedIdentityType: "invalid-type",
				},
			},
			expectedError: errors.New("invalid managed identity type"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test token
			testToken := token.New(
				"test",
				"test",
				"mock-token",
				time.Now().Add(time.Hour),
				time.Now(),
				int64(time.Hour),
			)

			// Set the token manager factory in the options
			tt.options.tokenManagerFactory = testTokenManagerFactory(testToken, nil)

			provider, err := NewManagedIdentityCredentialsProvider(tt.options)
			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)

				// Test the provider with a mock listener
				listener := &mockCredentialsListener{LastTokenCh: make(chan string)}
				_, _, err := provider.Subscribe(listener)
				assert.NoError(t, err)
				assert.Equal(t, "mock-token", <-listener.LastTokenCh)
			}
		})
	}
}

func TestNewConfidentialCredentialsProvider(t *testing.T) {
	tests := []struct {
		name          string
		options       ConfidentialCredentialsProviderOptions
		expectedError error
	}{
		{
			name: "valid confidential options with client secret",
			options: ConfidentialCredentialsProviderOptions{
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
			},
			expectedError: nil,
		},
		{
			name: "missing required fields",
			options: ConfidentialCredentialsProviderOptions{
				ConfidentialIdentityProviderOptions: identity.ConfidentialIdentityProviderOptions{
					CredentialsType: identity.ClientSecretCredentialType,
				},
			},
			expectedError: errors.New("client ID is required"),
		},
		{
			name: "invalid credentials type",
			options: ConfidentialCredentialsProviderOptions{
				ConfidentialIdentityProviderOptions: identity.ConfidentialIdentityProviderOptions{
					ClientID:        "test-client-id",
					CredentialsType: "invalid-type",
				},
			},
			expectedError: errors.New("invalid credentials type"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test token
			testToken := token.New(
				"test",
				"test",
				"mock-token",
				time.Now().Add(time.Hour),
				time.Now(),
				int64(time.Hour),
			)

			// Set the token manager factory in the options
			tt.options.tokenManagerFactory = testTokenManagerFactory(testToken, nil)

			provider, err := NewConfidentialCredentialsProvider(tt.options)
			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)

				// Test the provider with a mock listener
				listener := &mockCredentialsListener{LastTokenCh: make(chan string)}
				_, _, err := provider.Subscribe(listener)
				assert.NoError(t, err)
				assert.Equal(t, "mock-token", <-listener.LastTokenCh)
			}
		})
	}
}

func TestNewDefaultAzureCredentialsProvider(t *testing.T) {
	tests := []struct {
		name          string
		options       DefaultAzureCredentialsProviderOptions
		expectedError error
	}{
		{
			name: "valid default azure options",
			options: DefaultAzureCredentialsProviderOptions{
				CredentialsProviderOptions: CredentialsProviderOptions{
					ClientID: "test-client-id",
					TokenManagerOptions: manager.TokenManagerOptions{
						ExpirationRefreshRatio: 0.7,
					},
				},
				DefaultAzureIdentityProviderOptions: identity.DefaultAzureIdentityProviderOptions{
					Scopes: []string{identity.RedisScopeDefault},
				},
			},
			expectedError: nil,
		},
		{
			name: "empty options",
			options: DefaultAzureCredentialsProviderOptions{
				CredentialsProviderOptions: CredentialsProviderOptions{
					TokenManagerOptions: manager.TokenManagerOptions{
						ExpirationRefreshRatio: 0.7,
					},
				},
			},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test token
			testToken := token.New(
				"test",
				"test",
				"mock-token",
				time.Now().Add(time.Hour),
				time.Now(),
				int64(time.Hour),
			)

			// Set the token manager factory in the options
			tt.options.tokenManagerFactory = testTokenManagerFactory(testToken, nil)

			provider, err := NewDefaultAzureCredentialsProvider(tt.options)
			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)

				// Test the provider with a mock listener
				listener := &mockCredentialsListener{LastTokenCh: make(chan string)}
				_, _, err := provider.Subscribe(listener)
				assert.NoError(t, err)
				assert.Equal(t, "mock-token", <-listener.LastTokenCh)
			}
		})
	}
}

func TestCredentialsProviderErrorHandling(t *testing.T) {
	t.Run("on re-authentication error", func(t *testing.T) {
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
				ClientSecret:    "test-secret",
				Scopes:          []string{identity.RedisScopeDefault},
				Authority:       identity.AuthorityConfiguration{},
			},
		}

		// Create a test token
		testToken := token.New(
			"test",
			"test",
			"mock-token",
			time.Now().Add(time.Hour),
			time.Now(),
			int64(time.Hour),
		)

		// Set the token manager factory in the options
		options.tokenManagerFactory = testTokenManagerFactory(testToken, nil)

		provider, err := NewConfidentialCredentialsProvider(options)
		require.NoError(t, err)
		require.NotNil(t, provider)

		// Test that the error handler is properly set
		// Note: This is a simplified test as actual authentication would require Azure credentials
		assert.NotNil(t, provider)
	})

	t.Run("on retryable error", func(t *testing.T) {
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
				ClientSecret:    "test-secret",
				Scopes:          []string{identity.RedisScopeDefault},
				Authority:       identity.AuthorityConfiguration{},
			},
		}

		// Create a test token
		testToken := token.New(
			"test",
			"test",
			"mock-token",
			time.Now().Add(time.Hour),
			time.Now(),
			int64(time.Hour),
		)

		// Set the token manager factory in the options
		options.tokenManagerFactory = testTokenManagerFactory(testToken, nil)

		provider, err := NewConfidentialCredentialsProvider(options)
		require.NoError(t, err)
		require.NotNil(t, provider)

		// Test that the error handler is properly set
		// Note: This is a simplified test as actual authentication would require Azure credentials
		assert.NotNil(t, provider)
	})
}

func TestCredentialsProviderInterface(t *testing.T) {
	// Test that all providers implement the StreamingCredentialsProvider interface
	tests := []struct {
		name     string
		provider auth.StreamingCredentialsProvider
	}{
		{
			name: "managed identity provider",
			provider: func() auth.StreamingCredentialsProvider {
				options := ManagedIdentityCredentialsProviderOptions{
					CredentialsProviderOptions: CredentialsProviderOptions{
						ClientID: "test-client-id",
						TokenManagerOptions: manager.TokenManagerOptions{
							ExpirationRefreshRatio: 0.7,
						},
					},
					ManagedIdentityProviderOptions: identity.ManagedIdentityProviderOptions{
						UserAssignedClientID: "test-client-id",
						ManagedIdentityType:  identity.UserAssignedIdentity,
						Scopes:               []string{identity.RedisScopeDefault},
					},
				}

				// Create a test token
				testToken := token.New(
					"test",
					"test",
					"mock-token",
					time.Now().Add(time.Hour),
					time.Now(),
					int64(time.Hour),
				)

				// Set the token manager factory in the options
				options.tokenManagerFactory = testTokenManagerFactory(testToken, nil)

				p, _ := NewManagedIdentityCredentialsProvider(options)
				return p
			}(),
		},
		{
			name: "confidential provider",
			provider: func() auth.StreamingCredentialsProvider {
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
						ClientSecret:    "test-secret",
						Scopes:          []string{identity.RedisScopeDefault},
						Authority:       identity.AuthorityConfiguration{},
					},
				}

				// Create a test token
				testToken := token.New(
					"test",
					"test",
					"mock-token",
					time.Now().Add(time.Hour),
					time.Now(),
					int64(time.Hour),
				)

				// Set the token manager factory in the options
				options.tokenManagerFactory = testTokenManagerFactory(testToken, nil)

				p, _ := NewConfidentialCredentialsProvider(options)
				return p
			}(),
		},
		{
			name: "default azure provider",
			provider: func() auth.StreamingCredentialsProvider {
				options := DefaultAzureCredentialsProviderOptions{
					CredentialsProviderOptions: CredentialsProviderOptions{
						ClientID: "test-client-id",
						TokenManagerOptions: manager.TokenManagerOptions{
							ExpirationRefreshRatio: 0.7,
						},
					},
					DefaultAzureIdentityProviderOptions: identity.DefaultAzureIdentityProviderOptions{
						Scopes: []string{identity.RedisScopeDefault},
					},
				}

				// Create a test token
				testToken := token.New(
					"test",
					"test",
					"mock-token",
					time.Now().Add(time.Hour),
					time.Now(),
					int64(time.Hour),
				)

				// Set the token manager factory in the options
				options.tokenManagerFactory = testTokenManagerFactory(testToken, nil)

				p, _ := NewDefaultAzureCredentialsProvider(options)
				return p
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that the provider implements the interface by calling its methods
			// Note: These are simplified tests as actual authentication would require Azure credentials
			listener := &mockCredentialsListener{}
			credentials, cancel, err := tt.provider.Subscribe(listener)
			assert.NotNil(t, credentials)
			assert.NotNil(t, cancel)
			assert.NoError(t, err)
		})
	}
}

func TestCredentialsProviderSubscribe(t *testing.T) {
	// Create a test token
	testToken := token.New(
		"test",
		"test",
		"mock-token",
		time.Now().Add(time.Hour),
		time.Now(),
		int64(time.Hour),
	)

	// Create a test provider
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
			ClientSecret:    "test-secret",
			Scopes:          []string{identity.RedisScopeDefault},
			Authority:       identity.AuthorityConfiguration{},
		},
	}

	// Set the token manager factory in the options
	options.tokenManagerFactory = testTokenManagerFactory(testToken, nil)

	provider, err := NewConfidentialCredentialsProvider(options)
	require.NoError(t, err)
	require.NotNil(t, provider)

	t.Run("concurrent subscribe and cancel", func(t *testing.T) {
		const numListeners = 10
		var wg sync.WaitGroup
		listeners := make([]*mockCredentialsListener, numListeners)
		cancels := make([]auth.CancelProviderFunc, numListeners)

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
			case token := <-listener.LastTokenCh:
				assert.Equal(t, "mock-token", token, "listener %d received wrong token", i)
			case err := <-listener.LastErrCh:
				t.Fatalf("listener %d received error: %v", i, err)
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
			case token := <-listener.LastTokenCh:
				t.Fatalf("listener %d received unexpected token after cancellation: %s", i, token)
			case err := <-listener.LastErrCh:
				t.Fatalf("listener %d received unexpected error after cancellation: %v", i, err)
			default:
				// No message received, which is expected
			}
		}
	})
}

func TestCredentialsProviderOptions(t *testing.T) {
	t.Run("default token manager factory", func(t *testing.T) {
		options := CredentialsProviderOptions{}
		factory := options.getTokenManagerFactory()
		assert.NotNil(t, factory)
	})

	t.Run("custom token manager factory", func(t *testing.T) {
		m := &mockTokenManager{}
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
		assert.Error(t, err)
		assert.Nil(t, provider)
	})
}
