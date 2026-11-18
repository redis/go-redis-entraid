package manager

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis-entraid/shared"
	"github.com/redis/go-redis-entraid/token"
	"github.com/stretchr/testify/require"
)

type blockedParser struct {
	entered, release chan struct{}
}

func (p blockedParser) ParseResponse(shared.IdentityProviderResponse) (*token.Token, error) {
	close(p.entered)
	<-p.release
	return nil, errors.New("parser failed")
}

func TestLimitedDemandDuringBlockedParser(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lifetime time.Duration
		allow    bool
		usable   bool
	}{
		{"valid", time.Minute, false, true},
		{"strict", -time.Minute, false, false},
		{"allow expired", -time.Minute, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parser := blockedParser{make(chan struct{}), make(chan struct{})}
			unblock := sync.OnceFunc(func() { close(parser.release) })
			defer unblock()
			idp := &failingIdentityProvider{fail: func(int32) bool { return false }}
			value, err := NewTokenManager(idp, TokenManagerOptions{AllowExpiredToken: tc.allow, LowerRefreshBound: time.Hour, IdentityProviderResponseParser: parser})
			require.NoError(t, err)
			tm := value.(*entraidTokenManager)
			defer tm.Stop()
			current, err := lifetimeParser(tc.lifetime).ParseResponse(nil)
			require.NoError(t, err)
			tm.token = current
			forced := make(chan error, 1)
			go func() { _, err := tm.GetToken(true); forced <- err }()
			select {
			case <-parser.entered:
			case <-time.After(time.Second):
				t.Fatal("forced refresh did not enter parser")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			read := make(chan error, 1)
			go func() {
				tk, err := tm.GetTokenWithContext(ctx, false)
				if err == nil && tk != current {
					err = errors.New("did not receive cached token")
				}
				read <- err
			}()
			select {
			case err := <-read:
				if tc.usable {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				}
			case <-time.After(time.Second):
				t.Fatal("cache read blocked on parser")
			}
			unblock()
			require.ErrorContains(t, <-forced, "parser failed")
			require.EqualValues(t, 1, idp.calls.Load())
		})
	}
}

func TestRestartDoesNotBypassDemandBackoff(t *testing.T) {
	idp := &gatedIdentityProvider{entered: make(chan struct{}, 1), release: make(chan struct{})}
	unblock := sync.OnceFunc(func() { close(idp.release) })
	defer unblock()
	m, err := NewTokenManager(idp, TokenManagerOptions{
		LowerRefreshBound: time.Hour, IdentityProviderResponseParser: lifetimeParser(time.Minute),
		RetryOptions: RetryOptions{MaxDelay: time.Hour, InitialDelay: time.Hour},
	})
	require.NoError(t, err)
	tm := m.(*entraidTokenManager)
	defer tm.Stop()
	current, err := tm.GetToken(false)
	require.NoError(t, err)
	for i := 0; i < 10; i++ {
		stop, err := tm.StartSubscriptions(context.Background(), lifecycleTestListener{})
		require.NoError(t, err)
		got, err := tm.GetToken(false)
		require.NoError(t, err)
		require.Same(t, current, got)
		version := tm.changed // this request is blocked, so no publication yet
		require.NoError(t, stop())
		if i == 0 {
			select {
			case <-idp.entered:
			case <-time.After(time.Second):
				t.Fatal("restart did not kick a due request")
			}
			tm.tokenRWLock.RLock()
			flight := tm.acquire
			tm.tokenRWLock.RUnlock()
			unblock()
			<-flight.done
			require.ErrorContains(t, flight.err, "identity provider unavailable")
		}
		// Simulate the old timer winning its select just before the stop.
		_, err = tm.getToken(context.Background(), true, version)
		require.NoError(t, err)
	}
	require.EqualValues(t, 2, idp.calls.Load(), "reconnects must respect the failed demand request's backoff")
}

// Pause a caller at its initial context check, before it selects a token or a request.
type pausedLookupContext struct {
	context.Context
	pause func()
}

func (c pausedLookupContext) Err() error {
	c.pause()
	return c.Context.Err()
}

func TestDelayedReaderUsesTokenPublishedBeforeLaterRefresh(t *testing.T) {
	idp := &gatedIdentityProvider{entered: make(chan struct{}, 1), release: make(chan struct{})}
	defer close(idp.release)
	m, err := NewTokenManager(idp, TokenManagerOptions{IdentityProviderResponseParser: lifetimeParser(time.Hour)})
	require.NoError(t, err)
	tm := m.(*entraidTokenManager)
	defer tm.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	entered, resume := make(chan struct{}), make(chan struct{})
	reader := pausedLookupContext{ctx, sync.OnceFunc(func() {
		close(entered)
		select {
		case <-resume:
		case <-ctx.Done():
		}
	})}
	var got *token.Token
	var readErr error
	done := make(chan struct{})
	go func() {
		got, readErr = tm.GetTokenWithContext(reader, false)
		close(done)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("reader did not reach token lookup")
	}

	fresh, err := tm.GetTokenWithContext(ctx, true)
	require.NoError(t, err)
	go tm.GetToken(true) // the next request blocks in the identity provider
	select {
	case <-idp.entered:
	case <-ctx.Done():
		t.Fatal("later refresh did not start")
	}
	close(resume)
	select {
	case <-done:
		require.NoError(t, readErr, "cached read waited for the later refresh")
		require.Same(t, fresh, got)
	case <-ctx.Done():
		t.Fatal("cached read waited for the later refresh")
	}
}

func TestRetiredTimerCannotRefreshAfterRestart(t *testing.T) {
	idp := &gatedIdentityProvider{entered: make(chan struct{}, 1), release: make(chan struct{})}
	defer close(idp.release)
	m, err := NewTokenManager(idp, TokenManagerOptions{IdentityProviderResponseParser: lifetimeParser(time.Hour)})
	require.NoError(t, err)
	tm := m.(*entraidTokenManager)
	defer tm.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stop, err := tm.StartSubscriptions(ctx, lifecycleTestListener{})
	require.NoError(t, err)
	tm.tokenRWLock.RLock()
	scheduled := tm.changed
	tm.tokenRWLock.RUnlock()

	// Model a timer that fired just before the last socket left: its renewal
	// reaches getToken only after a new socket has restarted the scheduler.
	require.NoError(t, stop())
	_, err = tm.StartSubscriptions(ctx, lifecycleTestListener{})
	require.NoError(t, err)
	_, err = tm.getToken(ctx, true, scheduled)
	require.NoError(t, err)
	tm.tokenRWLock.RLock()
	defer tm.tokenRWLock.RUnlock()
	// A second request would occupy the slot until the test releases the provider.
	require.Nil(t, tm.acquire, "retired timer installed a new request")
	require.EqualValues(t, 1, idp.calls.Load(), "only the initial token was needed")
}
