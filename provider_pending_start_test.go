package entraid

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis-entraid/shared"
	"github.com/stretchr/testify/require"
)

type cancelObservedIdentityProvider struct {
	entered chan context.Context
	release chan struct{}
}

func (p *cancelObservedIdentityProvider) RequestToken(ctx context.Context) (shared.IdentityProviderResponse, error) {
	p.entered <- ctx
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.release:
		return nil, errors.New("test cleanup")
	}
}

func TestCloseCancelsAbandonedManagerStartup(t *testing.T) {
	idp := &cancelObservedIdentityProvider{entered: make(chan context.Context, 1), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(idp.release) }) })
	tm, err := manager.NewTokenManager(idp, manager.TokenManagerOptions{RequestTimeout: time.Hour})
	require.NoError(t, err)
	// This is the stopped-provider state before a restart can return a stopper.
	p := &entraidCredentialsProvider{tokenManager: tm}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, _, err := p.SubscribeWithContext(ctx, &distinctSilentListener{}); done <- err }()
	var acquisition context.Context
	select {
	case acquisition = <-idp.entered:
	case <-time.After(time.Second):
		t.Fatal("startup did not enter identity request")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("caller remained blocked")
	}
	require.NoError(t, p.Close())
	select {
	case <-acquisition.Done():
	case <-time.After(time.Second):
		t.Fatal("Close left abandoned startup request alive")
	}
}
