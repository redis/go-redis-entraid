// Package entraid provides a credentials provider that manages token retrieval and notifies listeners
// of token updates. It implements the auth.StreamingCredentialsProvider interface and is designed
// for use with the Redis authentication system.
package entraid

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis-entraid/token"
	"github.com/redis/go-redis/v9/auth"
)

// Ensure entraidCredentialsProvider implements the auth.StreamingCredentialsProvider interface.
var _ auth.StreamingCredentialsProvider = (*entraidCredentialsProvider)(nil)

// entraidCredentialsProvider is a struct that implements the StreamingCredentialsProvider interface.
type listenerRegistration struct {
	active bool
}

type entraidCredentialsProvider struct {
	options CredentialsProviderOptions // Configuration options for the provider.

	tokenManager     manager.TokenManager // Manages token retrieval.
	stopTokenManager manager.StopFunc     // Function to stop the token manager.

	// listeners is a slice of listeners that are notified when the token manager receives a new token.
	listeners     []auth.CredentialsListener // Slice of listeners notified on token updates.
	registrations map[auth.CredentialsListener]*listenerRegistration

	// rwLock is a mutex that is used to synchronize access to the listeners slice.
	rwLock sync.RWMutex // Mutex for synchronizing access to the listeners slice.

	// stateMu protects lifecycle bookkeeping only. Manager methods and listener
	// callbacks must run outside it; pending reservations prevent last-unsubscribe
	// shutdown from racing a subscription that has not registered yet.
	// A failed stop is uncertain state: retry it before starting a new subscription.
	stateMu       sync.Mutex
	pending       int
	pendingDone   chan struct{}
	operation     *managerOperation
	stopFailed    bool
	stopRequested bool
}

type managerOperation struct {
	done chan struct{}
	err  error
}

// onTokenNext is a method that is called when the token manager receives a new token.
// onTokenNext snapshots the listeners under the lock and invokes callbacks
// after releasing it. An already-snapshotted callback may finish after unsubscribe returns.
// It notifies all registered listeners with the new token.
func (e *entraidCredentialsProvider) onTokenNext(t *token.Token) {
	e.rwLock.RLock()
	// Make a deep copy of the listeners slice to avoid data race
	listeners := make([]auth.CredentialsListener, len(e.listeners))
	copy(listeners, e.listeners)
	e.rwLock.RUnlock()
	// Notify all listeners with the new token.
	for _, listener := range listeners {
		listener.OnNext(t)
	}
}

// onTokenError is a method that is called when the token manager encounters an error.
// It notifies all registered listeners with the error.
func (e *entraidCredentialsProvider) onTokenError(err error) {
	e.rwLock.RLock()
	// Make a deep copy of the listeners slice to avoid data race
	listeners := make([]auth.CredentialsListener, len(e.listeners))
	copy(listeners, e.listeners)
	e.rwLock.RUnlock()

	// Notify all listeners with the error
	for _, listener := range listeners {
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
// - auth.UnsubscribeFunc: A function that can be called to unsubscribe the listener.
// - error: An error if the subscription fails, such as if the token cannot be retrieved.
//
// Note: If the listener is already subscribed, it will not receive duplicate notifications.
func (e *entraidCredentialsProvider) Subscribe(listener auth.CredentialsListener) (auth.Credentials, auth.UnsubscribeFunc, error) {
	return e.subscribeContext(context.Background(), listener)
}

// SubscribeWithContext bounds this caller's wait for subscription setup.
// The built-in token manager keeps shared acquisition independent of caller
// cancellation, including after a successful manager start. A canceled setup
// does not retire provider-owned work. Custom managers can provide StartWithContext and
// GetTokenWithContext to support the same context-aware waits; managers with
// only the legacy methods remain synchronous inside those calls.
func (e *entraidCredentialsProvider) SubscribeWithContext(ctx context.Context, listener auth.CredentialsListener) (auth.Credentials, auth.UnsubscribeFunc, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return e.subscribeContext(ctx, listener)
}

type contextTokenManager interface {
	StartWithContext(context.Context, manager.TokenListener) (manager.StopFunc, error)
	GetTokenWithContext(context.Context, bool) (*token.Token, error)
}

// reserveSubscription protects an in-progress registration, not execution of
// arbitrary manager code. Its completion channel also lets owner shutdown wait
// for already-reserved subscribers without holding stateMu.
func (e *entraidCredentialsProvider) reserveSubscription(ctx context.Context) error {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.pending == 0 {
		e.pendingDone = make(chan struct{})
	}
	e.pending++
	return nil
}

// finishSubscription completes a deferred last-listener stop request once the
// final pending setup exits. The stopRequested flag is set only by an actual
// last unsubscribe, so caller cancellation alone does not stop shared work.
func (e *entraidCredentialsProvider) finishSubscription() error {
	e.stateMu.Lock()
	e.pending--
	if e.pending == 0 {
		close(e.pendingDone)
	}
	e.rwLock.RLock()
	unused := len(e.listeners) == 0
	e.rwLock.RUnlock()
	if !e.stopRequested || e.pending != 0 || !unused || e.operation != nil || e.stopTokenManager == nil || e.stopFailed {
		e.stateMu.Unlock()
		return nil
	}
	stop := e.stopTokenManager
	op := &managerOperation{done: make(chan struct{}), err: fmt.Errorf("token manager stop did not complete")}
	e.operation = op
	e.stateMu.Unlock()
	if err := e.completeStop(op, stop); err != nil {
		return fmt.Errorf("couldn't cancel token manager: %w", err)
	}
	return nil
}

// ensureStarted shares lifecycle transitions, but never calls a custom manager
// with provider locks held. A canceled startup waiter does not poison peers.
func (e *entraidCredentialsProvider) ensureStarted(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.stateMu.Lock()
		if op := e.operation; op != nil {
			e.stateMu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-op.done:
				if op.err != nil && !errors.Is(op.err, context.Canceled) && !errors.Is(op.err, context.DeadlineExceeded) {
					return op.err
				}
				continue
			}
		}
		if e.stopFailed {
			stop := e.stopTokenManager
			op := &managerOperation{done: make(chan struct{}), err: fmt.Errorf("token manager stop did not complete")}
			e.operation = op
			e.stateMu.Unlock()
			if err := e.completeStop(op, stop); err != nil {
				return err
			}
			continue
		}
		if e.stopTokenManager != nil {
			e.stateMu.Unlock()
			return nil
		}
		op := &managerOperation{done: make(chan struct{}), err: fmt.Errorf("token manager start did not complete")}
		e.operation = op
		e.stateMu.Unlock()
		return e.completeStart(ctx, op)
	}
}

func (e *entraidCredentialsProvider) completeStart(ctx context.Context, op *managerOperation) (err error) {
	defer func() {
		e.stateMu.Lock()
		e.operation = nil
		close(op.done)
		e.stateMu.Unlock()
	}()
	var stop manager.StopFunc
	if cm, ok := e.tokenManager.(contextTokenManager); ok {
		stop, err = cm.StartWithContext(ctx, tokenListenerFromCP(e))
	} else {
		stop, err = e.tokenManager.Start(tokenListenerFromCP(e))
	}
	e.stateMu.Lock()
	if err == nil {
		e.stopTokenManager = stop
		e.stopRequested = false
	}
	op.err = err
	e.stateMu.Unlock()
	return err
}

func (e *entraidCredentialsProvider) completeStop(op *managerOperation, stop manager.StopFunc) (err error) {
	defer func() {
		e.stateMu.Lock()
		if op.err == nil {
			e.stopTokenManager = nil
			e.stopRequested = false
		}
		e.stopFailed = op.err != nil
		e.operation = nil
		close(op.done)
		e.stateMu.Unlock()
	}()
	err = stop()
	op.err = err
	return err
}

// validateListener catches invalid interface values before they can be used as
// keys in the registrations map. Comparing the interface to itself deliberately
// lets Go detect dynamic uncomparability; the scoped recovery converts that
// panic into a normal subscription error.
func validateListener(listener auth.CredentialsListener) (err error) {
	if listener == nil {
		return errors.New("credentials listener must not be nil")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("credentials listener must be comparable: %v", recovered)
		}
	}()
	_ = listener == listener
	return nil
}

func (e *entraidCredentialsProvider) subscribeContext(ctx context.Context, listener auth.CredentialsListener) (credentials auth.Credentials, unsubscribe auth.UnsubscribeFunc, err error) {
	if err := validateListener(listener); err != nil {
		return nil, nil, err
	}
	if err := e.reserveSubscription(ctx); err != nil {
		return nil, nil, err
	}
	defer func() {
		if cleanupErr := e.finishSubscription(); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()
	err = e.ensureStarted(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't start token manager: %w", err)
	}
	var tk *token.Token
	if cm, ok := e.tokenManager.(contextTokenManager); ok {
		tk, err = cm.GetTokenWithContext(ctx, false)
	} else {
		tk, err = e.tokenManager.GetToken(false)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		// The manager belongs to the provider, not this setup attempt. Keep
		// shared work available to later subscribers when this waiter leaves.
		return nil, nil, fmt.Errorf("couldn't get token: %w", err)
	}
	e.stateMu.Lock()
	e.rwLock.Lock()
	registration := e.registrations[listener]
	if registration == nil {
		registration = &listenerRegistration{active: true}
		if e.registrations == nil {
			e.registrations = make(map[auth.CredentialsListener]*listenerRegistration)
		}
		e.registrations[listener] = registration
		e.listeners = append(e.listeners, listener)
	}
	e.rwLock.Unlock()
	e.stopRequested = false
	e.stateMu.Unlock()
	var once sync.Once
	var unsubscribeErr error
	return tk, func() error {
		once.Do(func() { unsubscribeErr = e.unsubscribe(listener, registration) })
		return unsubscribeErr
	}, nil
}

func (e *entraidCredentialsProvider) unsubscribe(listener auth.CredentialsListener, registration *listenerRegistration) error {
	e.stateMu.Lock()
	e.rwLock.Lock()
	if e.registrations[listener] != registration || !registration.active {
		e.rwLock.Unlock()
		e.stateMu.Unlock()
		return nil
	}
	registration.active = false
	delete(e.registrations, listener)
	for i, l := range e.listeners {
		if l == listener {
			e.listeners = append(e.listeners[:i], e.listeners[i+1:]...)
			break
		}
	}
	last := len(e.listeners) == 0
	e.rwLock.Unlock()
	if last {
		e.stopRequested = true
	}
	if !last || e.pending != 0 || e.operation != nil || e.stopTokenManager == nil {
		e.stateMu.Unlock()
		return nil
	}
	stop := e.stopTokenManager
	op := &managerOperation{done: make(chan struct{}), err: fmt.Errorf("token manager stop did not complete")}
	e.operation = op
	e.stateMu.Unlock()
	if err := e.completeStop(op, stop); err != nil {
		return fmt.Errorf("couldn't cancel token manager: %w", err)
	}
	return nil
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
	// Start the token manager.
	stop, err := tokenManager.Start(tokenListenerFromCP(cp))
	if err != nil {
		return nil, fmt.Errorf("couldn't start token manager: %w", err)
	}
	cp.stopTokenManager = stop
	return cp, nil
}
