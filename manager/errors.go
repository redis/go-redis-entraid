package manager

import "fmt"

// ErrTokenManagerAlreadyStopped is returned when the token manager is already stopped.
var ErrTokenManagerAlreadyStopped = fmt.Errorf("token manager already stopped")

// ErrTokenManagerAlreadyStarted is returned when the token manager is already started.
var ErrTokenManagerAlreadyStarted = fmt.Errorf("token manager already started")
