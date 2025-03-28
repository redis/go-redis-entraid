package identity

import (
	"context"
	"crypto"
	"crypto/x509"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/confidential"
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

type mockAzureCredential struct {
	mock.Mock
}

func (m *mockAzureCredential) GetToken(ctx context.Context, options policy.TokenRequestOptions) (azcore.AccessToken, error) {
	args := m.Called(ctx, options)
	if args.Get(0) == nil {
		return azcore.AccessToken{}, args.Error(1)
	}
	return args.Get(0).(azcore.AccessToken), args.Error(1)
}

type mockCredFactory struct {
	// Mock implementation of the credFactory interface
	mock.Mock
}

func (m *mockCredFactory) NewDefaultAzureCredential(options *azidentity.DefaultAzureCredentialOptions) (azureCredential, error) {
	args := m.Called(options)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(azureCredential), args.Error(1)
}

type mockConfidentialCredentialFactory struct {
	// Mock implementation of the confidentialCredFactory interface
	mock.Mock
}

func (m *mockConfidentialCredentialFactory) NewCredFromSecret(clientSecret string) (confidential.Credential, error) {
	args := m.Called(clientSecret)
	if args.Get(0) == nil {
		return confidential.Credential{}, args.Error(1)
	}
	return args.Get(0).(confidential.Credential), args.Error(1)
}

func (m *mockConfidentialCredentialFactory) NewCredFromCert(clientCert []*x509.Certificate, clientPrivateKey crypto.PrivateKey) (confidential.Credential, error) {
	args := m.Called(clientCert, clientPrivateKey)
	if args.Get(0) == nil {
		return confidential.Credential{}, args.Error(1)
	}
	return args.Get(0).(confidential.Credential), args.Error(1)
}

type mockConfidentialTokenClient struct {
	// Mock implementation of the confidentialTokenClient interface
	mock.Mock
}

func (m *mockConfidentialTokenClient) AcquireTokenByCredential(ctx context.Context, scopes []string, options ...confidential.AcquireByCredentialOption) (confidential.AuthResult, error) {
	args := m.Called(ctx, options)
	if args.Get(0) == nil {
		return confidential.AuthResult{}, args.Error(1)
	}
	return args.Get(0).(confidential.AuthResult), args.Error(1)
}
