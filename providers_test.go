package entraid

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis-entraid/identity"
	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis-entraid/shared"
	"github.com/redis/go-redis-entraid/token"
	"github.com/redis/go-redis/v9/auth"
	"github.com/stretchr/testify/assert"
)

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
				rawTokenString,
				time.Now().Add(time.Hour),
				time.Now(),
				int64(time.Hour),
			)

			// Set the token manager factory in the options
			tt.options.tokenManagerFactory = testFakeTokenManagerFactory(testToken, nil)

			provider, err := NewManagedIdentityCredentialsProvider(tt.options)
			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)

				// Test the provider with a mock listener
				listener := &mockCredentialsListener{LastTokenCh: make(chan string)}
				tk, cancel, err := provider.Subscribe(listener)
				defer func() {
					err := cancel()
					if err != nil {
						panic(err)
					}
				}()
				assert.Equal(t, rawTokenString, tk.RawCredentials())
				assert.NoError(t, err)
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
				rawTokenString,
				time.Now().Add(time.Hour),
				time.Now(),
				int64(time.Hour),
			)

			// Set the token manager factory in the options
			tt.options.tokenManagerFactory = testFakeTokenManagerFactory(testToken, nil)

			provider, err := NewConfidentialCredentialsProvider(tt.options)
			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)

				// Test the provider with a mock listener
				listener := &mockCredentialsListener{LastTokenCh: make(chan string)}
				credentials, cancel, err := provider.Subscribe(listener)
				defer func() {
					err := cancel()
					if err != nil {
						panic(err)
					}
				}()
				assert.Equal(t, rawTokenString, credentials.RawCredentials())
				assert.NoError(t, err)
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
				rawTokenString,
				time.Now().Add(time.Hour),
				time.Now(),
				int64(time.Hour),
			)

			// Set the token manager factory in the options
			tt.options.tokenManagerFactory = testFakeTokenManagerFactory(testToken, nil)

			provider, err := NewDefaultAzureCredentialsProvider(tt.options)
			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)

				// Test the provider with a mock listener
				listener := &mockCredentialsListener{LastTokenCh: make(chan string)}
				tk, cancel, err := provider.Subscribe(listener)
				defer func() {
					err := cancel()
					if err != nil {
						panic(err)
					}
				}()
				assert.Equal(t, rawTokenString, tk.RawCredentials())
				assert.NoError(t, err)
			}
		})
	}
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
					rawTokenString,
					time.Now().Add(time.Hour),
					time.Now(),
					int64(time.Hour.Seconds()),
				)

				// Set the token manager factory in the options
				options.tokenManagerFactory = testFakeTokenManagerFactory(testToken, nil)

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
					rawTokenString,
					time.Now().Add(time.Hour),
					time.Now(),
					int64(time.Hour),
				)

				// Set the token manager factory in the options
				options.tokenManagerFactory = testFakeTokenManagerFactory(testToken, nil)

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
					rawTokenString,
					time.Now().Add(time.Hour),
					time.Now(),
					int64(time.Hour),
				)

				// Set the token manager factory in the options
				options.tokenManagerFactory = testFakeTokenManagerFactory(testToken, nil)

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

func TestNewManagedIdentityCredentialsProvider_TokenManagerFactoryError(t *testing.T) {
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

	// Set the token manager factory to return an error
	options.tokenManagerFactory = func(shared.IdentityProvider, manager.TokenManagerOptions) (manager.TokenManager, error) {
		return nil, fmt.Errorf("token manager factory error")
	}

	provider, err := NewManagedIdentityCredentialsProvider(options)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token manager factory error")
	assert.Nil(t, provider)
}

func TestNewConfidentialCredentialsProvider_TokenManagerFactoryError(t *testing.T) {
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

	// Set the token manager factory to return an error
	options.tokenManagerFactory = func(shared.IdentityProvider, manager.TokenManagerOptions) (manager.TokenManager, error) {
		return nil, fmt.Errorf("token manager factory error")
	}

	provider, err := NewConfidentialCredentialsProvider(options)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token manager factory error")
	assert.Nil(t, provider)
}

func TestNewDefaultAzureCredentialsProvider_TokenManagerFactoryError(t *testing.T) {
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

	// Set the token manager factory to return an error
	options.tokenManagerFactory = func(shared.IdentityProvider, manager.TokenManagerOptions) (manager.TokenManager, error) {
		return nil, fmt.Errorf("token manager factory error")
	}

	provider, err := NewDefaultAzureCredentialsProvider(options)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token manager factory error")
	assert.Nil(t, provider)
}

func TestNewManagedIdentityCredentialsProvider_TokenManagerStartError(t *testing.T) {
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
		rawTokenString,
		time.Now().Add(time.Hour),
		time.Now(),
		int64(time.Hour),
	)

	// Create a mock token manager that returns an error on Start
	mockTM := &fakeTokenManager{
		token: testToken,
		err:   fmt.Errorf("token manager start error"),
	}

	// Set the token manager factory to return our mock
	options.tokenManagerFactory = func(shared.IdentityProvider, manager.TokenManagerOptions) (manager.TokenManager, error) {
		return mockTM, nil
	}

	provider, err := NewManagedIdentityCredentialsProvider(options)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token manager start error")
	assert.Nil(t, provider)
}

func TestNewConfidentialCredentialsProvider_TokenManagerStartError(t *testing.T) {
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
		rawTokenString,
		time.Now().Add(time.Hour),
		time.Now(),
		int64(time.Hour),
	)

	// Create a mock token manager that returns an error on Start
	mockTM := &fakeTokenManager{
		token: testToken,
		err:   fmt.Errorf("token manager start error"),
	}

	// Set the token manager factory to return our mock
	options.tokenManagerFactory = func(shared.IdentityProvider, manager.TokenManagerOptions) (manager.TokenManager, error) {
		return mockTM, nil
	}

	provider, err := NewConfidentialCredentialsProvider(options)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token manager start error")
	assert.Nil(t, provider)
}

func TestNewDefaultAzureCredentialsProvider_TokenManagerStartError(t *testing.T) {
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
		rawTokenString,
		time.Now().Add(time.Hour),
		time.Now(),
		int64(time.Hour),
	)

	// Create a mock token manager that returns an error on Start
	mockTM := &fakeTokenManager{
		token: testToken,
		err:   fmt.Errorf("token manager start error"),
	}

	// Set the token manager factory to return our mock
	options.tokenManagerFactory = func(shared.IdentityProvider, manager.TokenManagerOptions) (manager.TokenManager, error) {
		return mockTM, nil
	}

	provider, err := NewDefaultAzureCredentialsProvider(options)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token manager start error")
	assert.Nil(t, provider)
}
