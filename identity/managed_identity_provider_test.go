package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	mi "github.com/AzureAD/microsoft-authentication-library-for-go/apps/managedidentity"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockManagedIdentityClient is a mock implementation of the managed identity client
type MockManagedIdentityClient struct {
	mock.Mock
}

func (m *MockManagedIdentityClient) AcquireToken(ctx context.Context, resource string, opts ...mi.AcquireTokenOption) (public.AuthResult, error) {
	args := m.Called(ctx, resource)
	return args.Get(0).(public.AuthResult), args.Error(1)
}

func TestNewManagedIdentityProvider(t *testing.T) {
	tests := []struct {
		name          string
		opts          ManagedIdentityProviderOptions
		expectedError string
	}{
		{
			name: "System assigned identity",
			opts: ManagedIdentityProviderOptions{
				ManagedIdentityType: SystemAssignedIdentity,
				Scopes:              []string{"https://redis.azure.com"},
			},
			expectedError: "",
		},
		{
			name: "User assigned identity with client ID",
			opts: ManagedIdentityProviderOptions{
				ManagedIdentityType:  UserAssignedObjectID,
				UserAssignedObjectID: "test-client-id",
				Scopes:               []string{"https://redis.azure.com"},
			},
			expectedError: "",
		},
		{
			name: "User assigned identity without client ID",
			opts: ManagedIdentityProviderOptions{
				ManagedIdentityType: UserAssignedObjectID,
				Scopes:              []string{"https://redis.azure.com"},
			},
			expectedError: "user assigned object ID is required when using user assigned identity",
		},
		{
			name: "Invalid identity type",
			opts: ManagedIdentityProviderOptions{
				ManagedIdentityType: "invalid-type",
				Scopes:              []string{"https://redis.azure.com"},
			},
			expectedError: "invalid managed identity type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewManagedIdentityProvider(tt.opts)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, tt.opts.ManagedIdentityType, provider.managedIdentityType)
				assert.Equal(t, tt.opts.UserAssignedObjectID, provider.userAssignedObjectID)
				assert.Equal(t, tt.opts.Scopes, provider.scopes)
				assert.NotNil(t, provider.client)
			}
		})
	}
}

func TestRequestToken(t *testing.T) {
	tests := []struct {
		name          string
		provider      *ManagedIdentityProvider
		expectedError string
	}{
		{
			name: "Success with default resource",
			provider: &ManagedIdentityProvider{
				scopes: []string{},
				client: new(MockManagedIdentityClient),
			},
			expectedError: "",
		},
		{
			name: "Success with custom resource",
			provider: &ManagedIdentityProvider{
				scopes: []string{"custom-resource"},
				client: new(MockManagedIdentityClient),
			},
			expectedError: "",
		},
		{
			name: "Error when client is nil",
			provider: &ManagedIdentityProvider{
				scopes: []string{},
				client: nil,
			},
			expectedError: "managed identity client is not initialized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the mock expectations if we have a mock client
			if tt.provider.client != nil {
				mockClient := tt.provider.client.(*MockManagedIdentityClient)
				expectedResource := RedisResource
				if len(tt.provider.scopes) > 0 {
					expectedResource = tt.provider.scopes[0]
				}

				if tt.expectedError == "" {
					mockClient.On("AcquireToken", mock.Anything, expectedResource).
						Return(public.AuthResult{
							AccessToken: "test-token",
							ExpiresOn:   time.Now().Add(time.Hour),
						}, nil)
				} else {
					mockClient.On("AcquireToken", mock.Anything, expectedResource).
						Return(public.AuthResult{}, errors.New(tt.expectedError))
				}
			}

			response, err := tt.provider.RequestToken(context.Background())

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, response)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, response)
			}

			// Verify mock expectations
			if tt.provider.client != nil {
				mockClient := tt.provider.client.(*MockManagedIdentityClient)
				mockClient.AssertExpectations(t)
			}
		})
	}
}

func TestRequestToken_ErrorCases(t *testing.T) {
	tests := []struct {
		name          string
		provider      *ManagedIdentityProvider
		setupMock     func(*MockManagedIdentityClient)
		expectedError string
	}{
		{
			name: "AcquireToken fails",
			provider: &ManagedIdentityProvider{
				scopes: []string{},
				client: new(MockManagedIdentityClient),
			},
			setupMock: func(m *MockManagedIdentityClient) {
				m.On("AcquireToken", mock.Anything, RedisResource).
					Return(public.AuthResult{}, errors.New("failed to acquire token"))
			},
			expectedError: "couldn't acquire token: failed to acquire token",
		},
		{
			name: "AcquireToken fails with custom resource",
			provider: &ManagedIdentityProvider{
				scopes: []string{"custom-resource"},
				client: new(MockManagedIdentityClient),
			},
			setupMock: func(m *MockManagedIdentityClient) {
				m.On("AcquireToken", mock.Anything, "custom-resource").
					Return(public.AuthResult{}, errors.New("failed to acquire token"))
			},
			expectedError: "couldn't acquire token: failed to acquire token",
		},
		{
			name: "AcquireToken fails with invalid resource",
			provider: &ManagedIdentityProvider{
				scopes: []string{"invalid-resource"},
				client: new(MockManagedIdentityClient),
			},
			setupMock: func(m *MockManagedIdentityClient) {
				m.On("AcquireToken", mock.Anything, "invalid-resource").
					Return(public.AuthResult{}, errors.New("invalid resource"))
			},
			expectedError: "couldn't acquire token: invalid resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := tt.provider.client.(*MockManagedIdentityClient)
			tt.setupMock(mockClient)

			response, err := tt.provider.RequestToken(context.Background())

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)
			assert.Nil(t, response)
			mockClient.AssertExpectations(t)
		})
	}
}

// MockMIClient is a mock implementation of the mi.Client interface
type MockMIClient struct {
	mock.Mock
}

func (m *MockMIClient) AcquireToken(ctx context.Context, resource string, opts ...mi.AcquireTokenOption) (public.AuthResult, error) {
	args := m.Called(ctx, resource)
	return args.Get(0).(public.AuthResult), args.Error(1)
}

func (m *MockMIClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

func TestRealManagedIdentityClient(t *testing.T) {
	// Create a mock managed identity client
	mockMIClient := new(MockManagedIdentityClient)
	client := &realManagedIdentityClient{client: mockMIClient}

	tests := []struct {
		name          string
		resource      string
		setupMock     func(*MockManagedIdentityClient)
		expectedError string
	}{
		{
			name:     "Success with default resource",
			resource: RedisResource,
			setupMock: func(m *MockManagedIdentityClient) {
				m.On("AcquireToken", mock.Anything, RedisResource, mock.Anything).
					Return(public.AuthResult{
						AccessToken: "test-token",
						ExpiresOn:   time.Now().Add(time.Hour),
					}, nil)
			},
		},
		{
			name:     "Success with custom resource",
			resource: "custom-resource",
			setupMock: func(m *MockManagedIdentityClient) {
				m.On("AcquireToken", mock.Anything, "custom-resource", mock.Anything).
					Return(public.AuthResult{
						AccessToken: "test-token",
						ExpiresOn:   time.Now().Add(time.Hour),
					}, nil)
			},
		},
		{
			name:     "Error from underlying client",
			resource: RedisResource,
			setupMock: func(m *MockManagedIdentityClient) {
				m.On("AcquireToken", mock.Anything, RedisResource, mock.Anything).
					Return(public.AuthResult{}, errors.New("underlying client error"))
			},
			expectedError: "underlying client error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset the mock for each test
			mockMIClient.ExpectedCalls = nil
			mockMIClient.Calls = nil

			// Set up the mock
			tt.setupMock(mockMIClient)

			// Call AcquireToken with empty options slice to match mock setup
			result, err := client.AcquireToken(context.Background(), tt.resource, []mi.AcquireTokenOption{}...)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Equal(t, public.AuthResult{}, result)
			} else {
				assert.NoError(t, err)
				assert.NotEqual(t, public.AuthResult{}, result)
				assert.Equal(t, "test-token", result.AccessToken)
			}

			// Verify mock expectations
			mockMIClient.AssertExpectations(t)
		})
	}
}
