package internal

import "fmt"

var ErrInvalidIDPResponse = fmt.Errorf("invalid identity provider response")
var ErrAuthResultNotFound = fmt.Errorf("auth result not found")
var ErrAccessTokenNotFound = fmt.Errorf("access token not found")
var ErrRawTokenNotFound = fmt.Errorf("raw token not found")
