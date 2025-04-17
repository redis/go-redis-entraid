package internal

import (
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	"github.com/stretchr/testify/assert"
)

func TestIDPResp_Type(t *testing.T) {
	tests := []struct {
		name       string
		resultType string
		want       string
	}{
		{
			name:       "AuthResult type",
			resultType: "AuthResult",
			want:       "AuthResult",
		},
		{
			name:       "Empty type",
			resultType: "",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &IDPResp{
				resultType: tt.resultType,
			}
			if got := resp.Type(); got != tt.want {
				t.Errorf("IDPResp.Type() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIDPResp_AuthResult(t *testing.T) {
	now := time.Now()
	authResult := &public.AuthResult{
		AccessToken: "test-token",
		ExpiresOn:   now,
	}

	tests := []struct {
		name          string
		authResult    *public.AuthResult
		wantToken     string
		wantExpiresOn time.Time
	}{
		{
			name:          "With AuthResult",
			authResult:    authResult,
			wantToken:     "test-token",
			wantExpiresOn: now,
		},
		{
			name:          "Nil AuthResult",
			authResult:    nil,
			wantToken:     "",
			wantExpiresOn: time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &IDPResp{
				authResultVal: tt.authResult,
			}
			got := resp.AuthResult()
			if got.AccessToken != tt.wantToken {
				t.Errorf("IDPResp.AuthResult().AccessToken = %v, want %v", got.AccessToken, tt.wantToken)
			}
			if !got.ExpiresOn.Equal(tt.wantExpiresOn) {
				t.Errorf("IDPResp.AuthResult().ExpiresOn = %v, want %v", got.ExpiresOn, tt.wantExpiresOn)
			}
		})
	}
}

func TestIDPResp_AccessToken(t *testing.T) {
	now := time.Now()
	accessToken := &azcore.AccessToken{
		Token:     "test-token",
		ExpiresOn: now,
	}

	tests := []struct {
		name          string
		accessToken   *azcore.AccessToken
		wantToken     string
		wantExpiresOn time.Time
	}{
		{
			name:          "With AccessToken",
			accessToken:   accessToken,
			wantToken:     "test-token",
			wantExpiresOn: now,
		},
		{
			name:          "Nil AccessToken",
			accessToken:   nil,
			wantToken:     "",
			wantExpiresOn: time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &IDPResp{
				accessTokenVal: tt.accessToken,
			}
			got := resp.AccessToken()
			if got.Token != tt.wantToken {
				t.Errorf("IDPResp.AccessToken().Token = %v, want %v", got.Token, tt.wantToken)
			}
			if !got.ExpiresOn.Equal(tt.wantExpiresOn) {
				t.Errorf("IDPResp.AccessToken().ExpiresOn = %v, want %v", got.ExpiresOn, tt.wantExpiresOn)
			}
		})
	}
}

func TestIDPResp_RawToken(t *testing.T) {
	tests := []struct {
		name     string
		rawToken string
		want     string
	}{
		{
			name:     "With RawToken",
			rawToken: "test-raw-token",
			want:     "test-raw-token",
		},
		{
			name:     "Empty RawToken",
			rawToken: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &IDPResp{
				rawTokenVal: tt.rawToken,
			}
			if got := resp.RawToken(); got != tt.want {
				t.Errorf("IDPResp.RawToken() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewIDPResp(t *testing.T) {
	tests := []struct {
		name        string
		resultType  string
		result      interface{}
		wantErr     bool
		checkResult func(t *testing.T, resp *IDPResp)
	}{
		{
			name:       "valid AuthResult pointer",
			resultType: "AuthResult",
			result: &public.AuthResult{
				AccessToken: "test-token",
			},
			wantErr: false,
			checkResult: func(t *testing.T, resp *IDPResp) {
				assert.Equal(t, "test-token", resp.AuthResult().AccessToken)
			},
		},
		{
			name:       "valid AuthResult value",
			resultType: "AuthResult",
			result: public.AuthResult{
				AccessToken: "test-token",
			},
			wantErr: false,
			checkResult: func(t *testing.T, resp *IDPResp) {
				assert.Equal(t, "test-token", resp.AuthResult().AccessToken)
			},
		},
		{
			name:       "valid AccessToken pointer",
			resultType: "AccessToken",
			result: &azcore.AccessToken{
				Token:     "test-token",
				ExpiresOn: time.Now(),
			},
			wantErr: false,
			checkResult: func(t *testing.T, resp *IDPResp) {
				assert.Equal(t, "test-token", resp.AccessToken().Token)
				assert.Equal(t, "test-token", resp.RawToken())
			},
		},
		{
			name:       "valid AccessToken value",
			resultType: "AccessToken",
			result: azcore.AccessToken{
				Token:     "test-token",
				ExpiresOn: time.Now(),
			},
			wantErr: false,
			checkResult: func(t *testing.T, resp *IDPResp) {
				assert.Equal(t, "test-token", resp.AccessToken().Token)
				assert.Equal(t, "test-token", resp.RawToken())
			},
		},
		{
			name:       "valid RawToken string",
			resultType: "RawToken",
			result:     "test-token",
			wantErr:    false,
			checkResult: func(t *testing.T, resp *IDPResp) {
				assert.Equal(t, "test-token", resp.RawToken())
			},
		},
		{
			name:       "valid RawToken string pointer",
			resultType: "RawToken",
			result:     stringPtr("test-token"),
			wantErr:    false,
			checkResult: func(t *testing.T, resp *IDPResp) {
				assert.Equal(t, "test-token", resp.RawToken())
			},
		},
		{
			name:       "nil result",
			resultType: "AuthResult",
			result:     nil,
			wantErr:    true,
		},
		{
			name:       "nil RawToken pointer",
			resultType: "RawToken",
			result:     (*string)(nil),
			wantErr:    true,
		},
		{
			name:       "invalid AuthResult type",
			resultType: "AuthResult",
			result:     "not-an-auth-result",
			wantErr:    true,
		},
		{
			name:       "invalid AccessToken type",
			resultType: "AccessToken",
			result:     "not-an-access-token",
			wantErr:    true,
		},
		{
			name:       "invalid RawToken type",
			resultType: "RawToken",
			result:     123,
			wantErr:    true,
		},
		{
			name:       "unsupported result type",
			resultType: "InvalidType",
			result:     "test",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewIDPResp(tt.resultType, tt.result)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, got)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, got)
			assert.Equal(t, tt.resultType, got.Type())

			if tt.checkResult != nil {
				tt.checkResult(t, got)
			}
		})
	}
}

func stringPtr(s string) *string {
	return &s
}

func BenchmarkIDPResp_Type(b *testing.B) {
	resp := &IDPResp{
		resultType: "AuthResult",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp.Type()
	}
}

func BenchmarkIDPResp_AuthResult(b *testing.B) {
	now := time.Now()
	authResult := &public.AuthResult{
		AccessToken: "test-token",
		ExpiresOn:   now,
	}
	resp := &IDPResp{
		authResultVal: authResult,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp.AuthResult()
	}
}

func BenchmarkIDPResp_AccessToken(b *testing.B) {
	now := time.Now()
	accessToken := &azcore.AccessToken{
		Token:     "test-token",
		ExpiresOn: now,
	}
	resp := &IDPResp{
		accessTokenVal: accessToken,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp.AccessToken()
	}
}

func BenchmarkIDPResp_RawToken(b *testing.B) {
	resp := &IDPResp{
		rawTokenVal: "test-raw-token",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp.RawToken()
	}
}

func BenchmarkNewIDPResp(b *testing.B) {
	now := time.Now()
	authResult := &public.AuthResult{
		AccessToken: "test-token",
		ExpiresOn:   now,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = NewIDPResp("AuthResult", authResult)
	}
}
