package manager

import (
	"net"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	"github.com/redis-developer/go-redis-entraid/shared"
	"github.com/redis-developer/go-redis-entraid/token"
	"github.com/stretchr/testify/mock"
)

// testJWTToken is a JWT token for testing
//
//	{
//	 "iss": "test jwt",
//	 "iat": 1743515011,
//	 "exp": 1775051011,
//	 "aud": "www.example.com",
//	 "sub": "test@test.com",
//	 "oid": "test"
//	}
//
// key: qwertyuiopasdfghjklzxcvbnm123456
const testJWTToken = "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJ0ZXN0IGp3dCIsImlhdCI6MTc0MzUxNTAxMSwiZXhwIjoxNzc1MDUxMDExLCJhdWQiOiJ3d3cuZXhhbXBsZS5jb20iLCJzdWIiOiJ0ZXN0QHRlc3QuY29tIiwib2lkIjoidGVzdCJ9.6RG721V2eFlSLsCRmo53kSRRrTZIe1UPdLZCUEvIarU"

// testJWTExpiredToken is an expired JWT token for testing
//
// {
// "iss": "test jwt",
// "iat": 1617795148,
// "exp": 1617795148,
// "aud": "www.example.com",
// "sub": "test@test.com",
// "oid": "test"
// }
//
// key: qwertyuiopasdfghjklzxcvbnm123456
const testJWTExpiredToken = "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJ0ZXN0IGp3dCIsImlhdCI6MTYxNzc5NTE0OCwiZXhwIjoxNjE3Nzk1MTQ4LCJhdWQiOiJ3d3cuZXhhbXBsZS5jb20iLCJzdWIiOiJ0ZXN0QHRlc3QuY29tIiwib2lkIjoidGVzdCJ9.IbGPhHRiPYcpUDrhAPf4h3gH1XXBOu560NYT59rUMzc"

// testJWTWithZeroExpiryToken is a JWT token with zero expiry for testing
//
// {
// "iss": "test jwt",
// "iat": 1744025944,
// "exp": null,
// "aud": "www.example.com",
// "sub": "test@test.com",
// "oid": "test"
// }
// key: qwertyuiopasdfghjklzxcvbnm123456
const testJWTWithZeroExpiryToken = "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJ0ZXN0IGp3dCIsImlhdCI6MTc0NDAyNTk0NCwiZXhwIjpudWxsLCJhdWQiOiJ3d3cuZXhhbXBsZS5jb20iLCJzdWIiOiJ0ZXN0QHRlc3QuY29tIiwib2lkIjoidGVzdCJ9.bLSANIzawE5Y6rgspvvUaRhkBq6Y4E0ggjXlmHRn8ew"

var testTokenValid = token.New(
	"test",
	"password",
	"test",
	time.Now().Add(time.Hour),
	time.Now(),
	int64(time.Hour),
)

type mockIdentityProviderResponseParser struct {
	// Mock implementation of the IdentityProviderResponseParser interface
	mock.Mock
}

func (m *mockIdentityProviderResponseParser) ParseResponse(response shared.IdentityProviderResponse) (*token.Token, error) {
	args := m.Called(response)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*token.Token), args.Error(1)
}

type mockIdentityProvider struct {
	// Mock implementation of the mockIdentityProvider interface
	// Add any necessary fields or methods for the mock identity provider here
	mock.Mock
}

func (m *mockIdentityProvider) RequestToken() (shared.IdentityProviderResponse, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(shared.IdentityProviderResponse), args.Error(1)
}

func newMockError(retriable bool) error {
	if retriable {
		return &mockError{
			isTimeout:   true,
			isTemporary: true,
			error:       os.ErrDeadlineExceeded,
		}
	} else {
		return &mockError{
			isTimeout:   false,
			isTemporary: false,
			error:       os.ErrInvalid,
		}
	}
}

type mockError struct {
	// Mock implementation of the network error
	error
	isTimeout   bool
	isTemporary bool
}

func (m *mockError) Error() string {
	return "this is mock error"
}

func (m *mockError) Timeout() bool {
	return m.isTimeout
}
func (m *mockError) Temporary() bool {
	return m.isTemporary
}
func (m *mockError) Unwrap() error {
	return m.error
}

func (m *mockError) Is(err error) bool {
	return m.error == err
}

var _ net.Error = (*mockError)(nil)

type mockTokenListener struct {
	// Mock implementation of the TokenManagerListener interface
	mock.Mock
	Id int32
}

func (m *mockTokenListener) OnTokenNext(token *token.Token) {
	_ = m.Called(token)
}

func (m *mockTokenListener) OnTokenError(err error) {
	_ = m.Called(err)
}

type authResult struct {
	// ResultType is the type of the response (AuthResult, AccessToken, or RawToken)
	ResultType string
	// AuthResultVal is the auth result value
	AuthResultVal *public.AuthResult
	// AccessTokenVal is the access token value
	AccessTokenVal *azcore.AccessToken
	// RawTokenVal is the raw token value
	RawTokenVal string
}

func (a *authResult) Type() string {
	return a.ResultType
}

func (a *authResult) AuthResult() public.AuthResult {
	if a.AuthResultVal == nil {
		return public.AuthResult{}
	}
	return *a.AuthResultVal
}

func (a *authResult) AccessToken() azcore.AccessToken {
	if a.AccessTokenVal == nil {
		return azcore.AccessToken{}
	}
	return *a.AccessTokenVal
}

func (a *authResult) RawToken() string {
	return a.RawTokenVal
}
