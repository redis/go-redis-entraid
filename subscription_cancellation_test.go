package entraid

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis/v9/auth"
	"github.com/stretchr/testify/require"
)

func TestCanceledSubscriptionAfterStartPreservesSharedAcquisition(t *testing.T) {
	idp := &pausedIdentityProvider{entered: make(chan struct{}, 3), release: make(chan struct{}), pauseAt: 2}
	tm, err := manager.NewTokenManager(idp, manager.TokenManagerOptions{
		IdentityProviderResponseParser: &fixedTokenParser{firstExpired: true},
		RetryOptions:                   manager.RetryOptions{InitialDelay: time.Hour},
	})
	require.NoError(t, err)
	// A stopped provider has no successful-start stopper. The initial parser
	// result forces acquisition again after Start, rather than during Start.
	p := &entraidCredentialsProvider{tokenManager: tm}
	var release sync.Once
	t.Cleanup(func() {
		release.Do(func() { close(idp.release) })
		p.stateMu.Lock()
		stop := p.stopTokenManager
		p.stateMu.Unlock()
		if stop != nil {
			_ = stop()
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() { _, _, err := p.SubscribeWithContext(ctx, silentCredentialsListener{}); first <- err }()
	for i := 0; i < 2; i++ {
		select {
		case <-idp.entered:
		case <-time.After(time.Second):
			t.Fatal("startup and subscription acquisition were not reached")
		}
	}
	cancel()
	select {
	case err := <-first:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("canceled subscriber did not return")
	}
	type result struct {
		unsubscribe auth.UnsubscribeFunc
		err         error
	}
	next := make(chan result, 1)
	go func() { _, u, err := p.Subscribe(&distinctSilentListener{}); next <- result{u, err} }()
	release.Do(func() { close(idp.release) })
	select {
	case r := <-next:
		require.NoError(t, r.err)
		defer r.unsubscribe()
	case <-time.After(time.Second):
		t.Fatal("next subscriber did not receive shared acquisition")
	}
	idp.mu.Lock()
	calls := idp.calls
	idp.mu.Unlock()
	require.Equal(t, 2, calls, "cancellation retired shared acquisition and forced another identity request")
	p.rwLock.RLock()
	listeners := len(p.listeners)
	p.rwLock.RUnlock()
	require.Equal(t, 1, listeners, "canceled subscriber must not register")
}
