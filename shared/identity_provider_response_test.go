package shared

import (
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	"github.com/redis/go-redis-entraid/token"
	"github.com/stretchr/testify/assert"
)

// Mock implementations for testing
type mockIDPResponse struct {
	responseType string
	authResult   *public.AuthResult
	accessToken  *azcore.AccessToken
	rawToken     string
}

func (m *mockIDPResponse) Type() string {
	return m.responseType
}

func (m *mockIDPResponse) AuthResult() (public.AuthResult, error) {
	if m.authResult == nil {
		return public.AuthResult{}, ErrAuthResultNotFound
	}
	return *m.authResult, nil
}

func (m *mockIDPResponse) AccessToken() (azcore.AccessToken, error) {
	if m.accessToken == nil {
		return azcore.AccessToken{}, ErrAccessTokenNotFound
	}
	return *m.accessToken, nil
}

func (m *mockIDPResponse) RawToken() (string, error) {
	if m.rawToken == "" {
		return "", ErrRawTokenNotFound
	}
	return m.rawToken, nil
}

type mockIDPParser struct {
	parseError error
	token      *token.Token
}

func (m *mockIDPParser) ParseResponse(response IdentityProviderResponse) (*token.Token, error) {
	if m.parseError != nil {
		return nil, m.parseError
	}
	return m.token, nil
}

type mockIDP struct {
	response IdentityProviderResponse
	err      error
}

func (m *mockIDP) RequestToken() (IdentityProviderResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func TestNewIDPResponse(t *testing.T) {
	tests := []struct {
		name          string
		responseType  string
		result        interface{}
		expectedError string
	}{
		{
			name:         "Valid AuthResult pointer",
			responseType: ResponseTypeAuthResult,
			result:       &public.AuthResult{},
		},
		{
			name:         "Valid AuthResult value",
			responseType: ResponseTypeAuthResult,
			result:       public.AuthResult{},
		},
		{
			name:         "Valid AccessToken pointer",
			responseType: ResponseTypeAccessToken,
			result:       &azcore.AccessToken{Token: "test-token"},
		},
		{
			name:         "Valid AccessToken value",
			responseType: ResponseTypeAccessToken,
			result:       azcore.AccessToken{Token: "test-token"},
		},
		{
			name:         "Valid RawToken string",
			responseType: ResponseTypeRawToken,
			result:       "test-token",
		},
		{
			name:         "Valid RawToken string pointer",
			responseType: ResponseTypeRawToken,
			result:       stringPtr("test-token"),
		},
		{
			name:          "Nil result",
			responseType:  ResponseTypeAuthResult,
			result:        nil,
			expectedError: ErrInvalidIDPResponse.Error(),
		},
		{
			name:          "Nil string pointer",
			responseType:  ResponseTypeRawToken,
			result:        (*string)(nil),
			expectedError: "raw token cannot be nil",
		},
		{
			name:          "Invalid AuthResult type",
			responseType:  ResponseTypeAuthResult,
			result:        "not-an-auth-result",
			expectedError: "invalid auth result type: expected public.AuthResult or *public.AuthResult",
		},
		{
			name:          "Invalid AccessToken type",
			responseType:  ResponseTypeAccessToken,
			result:        "not-an-access-token",
			expectedError: "invalid access token type: expected azcore.AccessToken or *azcore.AccessToken",
		},
		{
			name:          "Invalid RawToken type",
			responseType:  ResponseTypeRawToken,
			result:        123,
			expectedError: "invalid raw token type: expected string or *string",
		},
		{
			name:          "Invalid response type",
			responseType:  "InvalidType",
			result:        "test",
			expectedError: "unsupported identity provider response type: InvalidType",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := NewIDPResponse(tt.responseType, tt.result)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, resp)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, resp)
			assert.Equal(t, tt.responseType, resp.Type())

			switch tt.responseType {
			case ResponseTypeAuthResult:
				response, ok := resp.(AuthResultIDPResponse)
				assert.True(t, ok)
				res, err := response.AuthResult()
				assert.NoError(t, err)
				assert.NotNil(t, res)
			case ResponseTypeAccessToken:
				response, ok := resp.(AccessTokenIDPResponse)
				assert.True(t, ok)
				res, err := response.AccessToken()
				assert.NoError(t, err)
				assert.NotNil(t, res)
				assert.NotEmpty(t, res.Token)
			case ResponseTypeRawToken:
				response, ok := resp.(RawTokenIDPResponse)
				assert.True(t, ok)
				res, err := response.RawToken()
				assert.NoError(t, err)
				assert.NotNil(t, res)
			}
		})
	}
}

func stringPtr(s string) *string {
	return &s
}

func TestIdentityProviderResponse(t *testing.T) {
	now := time.Now()
	expires := now.Add(time.Hour)

	authResult := &public.AuthResult{
		AccessToken: "test-access-token",
		ExpiresOn:   expires,
	}

	accessToken := &azcore.AccessToken{
		Token:     "test-access-token",
		ExpiresOn: expires,
	}

	tests := []struct {
		name         string
		responseType string
		result       interface{}
		expectedType string
	}{
		{
			name:         "AuthResult response",
			responseType: ResponseTypeAuthResult,
			result:       authResult,
			expectedType: ResponseTypeAuthResult,
		},
		{
			name:         "AccessToken response",
			responseType: ResponseTypeAccessToken,
			result:       accessToken,
			expectedType: ResponseTypeAccessToken,
		},
		{
			name:         "RawToken response",
			responseType: ResponseTypeRawToken,
			result:       "test-raw-token",
			expectedType: ResponseTypeRawToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := NewIDPResponse(tt.responseType, tt.result)
			assert.NoError(t, err)
			switch tt.expectedType {
			case ResponseTypeAuthResult:
				typedResponse, ok := response.(AuthResultIDPResponse)
				assert.True(t, ok)
				result, err := typedResponse.AuthResult()
				assert.NoError(t, err)
				assert.Equal(t, authResult.AccessToken, result.AccessToken)
				assert.Equal(t, authResult.ExpiresOn, result.ExpiresOn)
			case ResponseTypeAccessToken:
				typedResponse, ok := response.(AccessTokenIDPResponse)
				assert.True(t, ok)
				token, err := typedResponse.AccessToken()
				assert.NoError(t, err)
				assert.Equal(t, accessToken.Token, token.Token)
				assert.Equal(t, accessToken.ExpiresOn, token.ExpiresOn)
			case ResponseTypeRawToken:
				typedResponse, ok := response.(RawTokenIDPResponse)
				assert.True(t, ok)
				rawToken, err := typedResponse.RawToken()
				assert.NoError(t, err)
				assert.Equal(t, "test-raw-token", rawToken)
			}
		})
	}
}

func TestIdentityProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider *mockIDP
		wantErr  bool
	}{
		{
			name: "Successful token request",
			provider: &mockIDP{
				response: &mockIDPResponse{
					responseType: ResponseTypeRawToken,
					rawToken:     "test-token",
				},
			},
			wantErr: false,
		},
		{
			name: "Failed token request",
			provider: &mockIDP{
				err: assert.AnError,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := tt.provider.RequestToken()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, response)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, response)
				assert.Equal(t, ResponseTypeRawToken, response.Type())
				rawTokenResponse, ok := response.(RawTokenIDPResponse)
				assert.True(t, ok)
				assert.NotNil(t, rawTokenResponse)
				// Check the raw token value
				rawToken, err := rawTokenResponse.RawToken()
				assert.NoError(t, err)
				assert.NotEmpty(t, rawToken)
				assert.Equal(t, "test-token", rawToken)
			}
		})
	}
}

func TestIdentityProviderResponseParser(t *testing.T) {
	now := time.Now()
	expires := now.Add(time.Hour)
	testToken := token.New("test-user", "test-password", "test-token", expires, now, time.Hour.Milliseconds())

	tests := []struct {
		name      string
		parser    *mockIDPParser
		response  IdentityProviderResponse
		wantErr   bool
		wantToken *token.Token
	}{
		{
			name: "Successful parse",
			parser: &mockIDPParser{
				token: testToken,
			},
			response: &mockIDPResponse{
				responseType: ResponseTypeRawToken,
				rawToken:     "test-token",
			},
			wantErr:   false,
			wantToken: testToken,
		},
		{
			name: "Failed parse",
			parser: &mockIDPParser{
				parseError: assert.AnError,
			},
			response: &mockIDPResponse{
				responseType: ResponseTypeRawToken,
				rawToken:     "test-token",
			},
			wantErr:   true,
			wantToken: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := tt.parser.ParseResponse(tt.response)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, token)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantToken, token)
			}
		})
	}
}
