package internal

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
)

// IDPResp represents a response from an Identity Provider (IDP)
// It can contain either an AuthResult, AccessToken, or a raw token string
type IDPResp struct {
	// resultType indicates which type of response this is
	resultType     string
	authResultVal  *public.AuthResult
	accessTokenVal *azcore.AccessToken
	rawTokenVal    string
}

// NewIDPResp creates a new IDPResp with the given values
// It validates the input and ensures the response type matches the provided value
func NewIDPResp(resultType string, result interface{}) (*IDPResp, error) {
	if result == nil {
		return nil, ErrInvalidIDPResponse
	}

	r := &IDPResp{resultType: resultType}

	switch resultType {
	case "AuthResult":
		switch v := result.(type) {
		case *public.AuthResult:
			r.authResultVal = v
		case public.AuthResult:
			r.authResultVal = &v
		default:
			return nil, fmt.Errorf("invalid auth result type: expected public.AuthResult or *public.AuthResult, got %T", result)
		}
	case "AccessToken":
		switch v := result.(type) {
		case *azcore.AccessToken:
			r.accessTokenVal = v
			r.rawTokenVal = v.Token
		case azcore.AccessToken:
			r.accessTokenVal = &v
			r.rawTokenVal = v.Token
		default:
			return nil, fmt.Errorf("invalid access token type: expected azcore.AccessToken or *azcore.AccessToken, got %T", result)
		}
	case "RawToken":
		switch v := result.(type) {
		case string:
			r.rawTokenVal = v
		case *string:
			if v == nil {
				return nil, fmt.Errorf("raw token cannot be nil")
			}
			r.rawTokenVal = *v
		default:
			return nil, fmt.Errorf("invalid raw token type: expected string or *string, got %T", result)
		}
	default:
		return nil, fmt.Errorf("unsupported identity provider response type: %s", resultType)
	}

	return r, nil
}

// Type returns the type of response this IDPResp represents
func (a *IDPResp) Type() string {
	return a.resultType
}

// AuthResult returns the AuthResult if present, or an empty AuthResult if not set
// Use HasAuthResult() to check if the value is actually set
func (a *IDPResp) AuthResult() (public.AuthResult, error) {
	if a.authResultVal == nil {
		return public.AuthResult{}, ErrAuthResultNotFound
	}
	return *a.authResultVal, nil
}

// AccessToken returns the AccessToken if present, or an empty AccessToken if not set
// Use HasAccessToken() to check if the value is actually set
func (a *IDPResp) AccessToken() (azcore.AccessToken, error) {
	if a.accessTokenVal == nil {
		return azcore.AccessToken{}, ErrAccessTokenNotFound
	}
	return *a.accessTokenVal, nil
}

// RawToken returns the raw token string
func (a *IDPResp) RawToken() (string, error) {
	if a.rawTokenVal == "" {
		return "", ErrRawTokenNotFound
	}
	return a.rawTokenVal, nil
}
