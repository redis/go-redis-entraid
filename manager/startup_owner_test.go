package manager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis-entraid/shared"
	"github.com/stretchr/testify/require"
)

type startupContextProvider struct {
	entered chan context.Context
	release chan struct{}
}

func (p *startupContextProvider) RequestToken(ctx context.Context) (shared.IdentityProviderResponse, error) {
	p.entered <- ctx
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.release:
		return nil, errors.New("test cleanup")
	}
}

func TestOwnerStopCancelsAcquisitionAcrossCanceledStartup(t *testing.T) {
	idp := &startupContextProvider{entered: make(chan context.Context, 1), release: make(chan struct{})}
	t.Cleanup(func() { close(idp.release) })
	value, err := NewTokenManager(idp, TokenManagerOptions{RequestTimeout: time.Hour})
	require.NoError(t, err)
	m := value.(*entraidTokenManager)
	firstCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := m.GetTokenWithContext(firstCtx, false); first <- err }()
	var request context.Context
	select {
	case request = <-idp.entered:
	case <-time.After(time.Second):
		t.Fatal("identity request not entered")
	}
	cancel()
	select {
	case err := <-first:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("caller did not cancel")
	}
	// Startup joins an acquisition that already belongs to this manager. Its
	// deadline must not replace the owner context used by that existing worker.
	startupCtx, startupCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer startupCancel()
	_, err = m.StartWithContext(startupCtx, lifecycleTestListener{})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, m.Stop())
	select {
	case <-request.Done():
	case <-time.After(time.Second):
		t.Fatal("startup detached the shared request from owner shutdown")
	}
	require.NoError(t, m.Stop())
}
