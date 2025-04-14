package manager

import "fmt"

// ErrTokenManagerAlreadyClosed is returned when the token manager is already closed.
var ErrTokenManagerAlreadyClosed = fmt.Errorf("token manager already closed")

// ErrTokenManagerAlreadyStarted is returned when the token manager is already started.
var ErrTokenManagerAlreadyStarted = fmt.Errorf("token manager already started")
