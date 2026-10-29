package entraid

import (
	"fmt"
	"io"

	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis/v9/auth"
)

// CredentialsProviderCloser is an optional owner-controlled shutdown contract.
// Clients own subscriptions, not a shared provider's lifetime.
type CredentialsProviderCloser interface {
	auth.StreamingCredentialsProvider
	io.Closer
}

var _ CredentialsProviderCloser = (*entraidCredentialsProvider)(nil)

type providerCloseAttempt struct {
	done chan struct{}
	err  error
}

// Close rejects subscriptions and releases provider-owned resources. Pending
// subscriptions finish before cleanup; they cannot register after Close begins.
// Concurrent calls join one cleanup attempt; a failed attempt may be retried.
// Snapshotted callbacks may finish afterward. Custom manager methods must not
// synchronously reenter Close on the same provider: startup/stop would wait on
// themselves. They may unsubscribe listeners. Stopper panics become retryable
// cleanup errors; cleanup completion is always published to concurrent callers.
func (e *entraidCredentialsProvider) Close() (err error) {
	e.stateMu.Lock()
	if attempt := e.closeAttempt; attempt != nil {
		e.stateMu.Unlock()
		<-attempt.done
		return attempt.err
	}
	if e.closed && e.stopTokenManager == nil && e.closeErr == nil {
		e.stateMu.Unlock()
		return nil
	}
	e.closed = true
	e.rwLock.Lock()
	for _, r := range e.registrations {
		r.active = false
	}
	e.listeners = nil
	e.registrations = nil
	e.rwLock.Unlock()
	attempt := &providerCloseAttempt{done: make(chan struct{})}
	e.closeAttempt = attempt
	pending := e.pendingDone
	e.stateMu.Unlock()
	// Always publish completion, including a panic from an extensible manager.
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("couldn't cancel token manager: %v", recovered)
		}
		e.stateMu.Lock()
		attempt.err = err
		e.closeErr = err
		if err == nil {
			e.stopTokenManager = nil
			e.stopFailed = false
		}
		e.closeAttempt = nil
		close(attempt.done)
		e.stateMu.Unlock()
	}()
	if pending != nil {
		<-pending
	}
	// Reservations have finished, but an earlier last-unsubscribe may still be
	// completing a stop. It also executes without provider locks held.
	for {
		e.stateMu.Lock()
		op := e.operation
		if op == nil {
			break
		}
		e.stateMu.Unlock()
		<-op.done
	}
	stop := e.stopTokenManager
	e.stateMu.Unlock()
	if owned, ok := e.tokenManager.(manager.TokenManagerStopper); ok {
		err = owned.Stop()
	} else if stop != nil {
		err = stop()
	}
	if err != nil {
		err = fmt.Errorf("couldn't cancel token manager: %w", err)
	}
	return err
}
