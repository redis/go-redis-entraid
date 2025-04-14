// Package entraid provides a credentials provider that manages token retrieval and notifies listeners
// of token updates. It implements the auth.StreamingCredentialsProvider interface and is designed
// for use with the Redis authentication system.
package entraid

import (
	"fmt"
	"sync"

	"github.com/redis-developer/go-redis-entraid/manager"
	"github.com/redis-developer/go-redis-entraid/token"
	"github.com/redis/go-redis/v9/auth"
)

// Ensure entraidCredentialsProvider implements the auth.StreamingCredentialsProvider interface.
var _ auth.StreamingCredentialsProvider = (*entraidCredentialsProvider)(nil)

// entraidCredentialsProvider is a struct that implements the StreamingCredentialsProvider interface.
type entraidCredentialsProvider struct {
	options CredentialsProviderOptions // Configuration options for the provider.

	tokenManager       manager.TokenManager // Manages token retrieval.
	cancelTokenManager manager.CancelFunc   // Function to cancel the token manager.

	// listeners is a slice of listeners that are notified when the token manager receives a new token.
	listeners []auth.CredentialsListener // Slice of listeners notified on token updates.

	// rwLock is a mutex that is used to synchronize access to the listeners slice.
	rwLock sync.RWMutex // Mutex for synchronizing access to the listeners slice.
}

// onTokenNext is a method that is called when the token manager receives a new token.
// It notifies all registered listeners with the new token.
func (e *entraidCredentialsProvider) onTokenNext(t *token.Token) {
	e.rwLock.RLock()
	defer e.rwLock.RUnlock()
	// Notify all listeners with the new token.
	for _, listener := range e.listeners {
		listener.OnNext(t)
	}
}

// onTokenError is a method that is called when the token manager encounters an error.
// It notifies all registered listeners with the error.
func (e *entraidCredentialsProvider) onTokenError(err error) {
	e.rwLock.RLock()
	defer e.rwLock.RUnlock()

	// Notify all listeners with the error
	for _, listener := range e.listeners {
		listener.OnError(err)
	}
}

// Subscribe subscribes a listener to the credentials provider.
// It returns the current credentials, a cancel function to unsubscribe, and an error if the subscription fails.
//
// Parameters:
// - listener: The listener that will receive updates about token changes.
//
// Returns:
// - auth.Credentials: The current credentials for the listener.
// - auth.CancelProviderFunc: A function that can be called to unsubscribe the listener.
// - error: An error if the subscription fails, such as if the token cannot be retrieved.
//
// Note: If the listener is already subscribed, it will not receive duplicate notifications.
func (e *entraidCredentialsProvider) Subscribe(listener auth.CredentialsListener) (auth.Credentials, auth.CancelProviderFunc, error) {
	e.rwLock.Lock()
	// Check if the listener is already in the list of listeners.
	alreadySubscribed := false
	for _, l := range e.listeners {
		if l == listener {
			alreadySubscribed = true
			break
		}
	}

	if !alreadySubscribed {
		// add new listener
		e.listeners = append(e.listeners, listener)
	}
	e.rwLock.Unlock()

	token, err := e.tokenManager.GetToken(false)
	if err != nil {
		//go listener.OnError(err)
		return nil, nil, err
	}

	// Notify the listener with the credentials.
	//go listener.OnNext(token)

	cancel := func() error {
		// Remove the listener from the list of listeners.
		e.rwLock.Lock()
		defer e.rwLock.Unlock()

		for i, l := range e.listeners {
			if l == listener {
				e.listeners = append(e.listeners[:i], e.listeners[i+1:]...)
				break
			}
		}

		// Clear the listeners slice if it's empty
		if len(e.listeners) == 0 {
			e.listeners = make([]auth.CredentialsListener, 0)
			if e.cancelTokenManager != nil {
				defer func() {
					e.cancelTokenManager = nil
				}()
				return e.cancelTokenManager()
			}
		}
		return nil
	}

	return token, cancel, nil
}

// NewCredentialsProvider creates a new credentials provider with the specified token manager and options.
// It returns a StreamingCredentialsProvider interface and an error if the token manager cannot be started.
//
// Parameters:
// - tokenManager: The TokenManager used to obtain tokens.
// - options: Options for configuring the credentials provider.
//
// Returns:
// - auth.StreamingCredentialsProvider: The newly created credentials provider.
// - error: An error if the token manager cannot be started.
func NewCredentialsProvider(tokenManager manager.TokenManager, options CredentialsProviderOptions) (auth.StreamingCredentialsProvider, error) {
	cp := &entraidCredentialsProvider{
		tokenManager: tokenManager,
		options:      options,
		listeners:    make([]auth.CredentialsListener, 0),
	}
	cancelTokenManager, err := cp.tokenManager.Start(tokenListenerFromCP(cp))
	if err != nil {
		return nil, fmt.Errorf("couldn't start token manager: %w", err)
	}
	cp.cancelTokenManager = cancelTokenManager
	return cp, nil
}
