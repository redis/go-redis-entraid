package entraid

import (
	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis-entraid/token"
)

// entraidTokenListener implements the TokenListener interface for the entraidCredentialsProvider.
// It listens for token updates and errors from the token manager and notifies the credentials provider.
type entraidTokenListener struct {
	cp *entraidCredentialsProvider
}

// tokenListenerFromCP creates a new entraidTokenListener from the given entraidCredentialsProvider.
// It is used to listen for token updates and errors from the token manager.
// This function is typically called when starting the token manager.
// It returns a pointer to the entraidTokenListener instance that is created from the credentials provider.
func tokenListenerFromCP(cp *entraidCredentialsProvider) manager.TokenListener {
	return &entraidTokenListener{
		cp,
	}
}

// OnTokenNext is called when the token manager receives a new token.
// It notifies the credentials provider with the new token.
// This function is typically called when the token manager successfully retrieves a token.
func (l *entraidTokenListener) OnNext(t *token.Token) {
	l.cp.onTokenNext(t)
}

// OnTokenError is called when the token manager encounters an error.
// It notifies the credentials provider with the error.
// This function is typically called when the token manager fails to retrieve a token.
func (l *entraidTokenListener) OnError(err error) {
	l.cp.onTokenError(err)
}
