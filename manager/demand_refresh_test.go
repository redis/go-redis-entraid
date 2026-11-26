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

func TestCachedReadDuringBlockedParse(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lifetime time.Duration
		usable   bool
	}{
		{"valid", time.Minute, true},
		{"expired", -time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parser := blockedParser{make(chan struct{}), make(chan struct{})}
			idp := &failingIdentityProvider{fail: func(int32) bool { return false }}
			value, err := NewTokenManager(idp, TokenManagerOptions{LowerRefreshBound: time.Hour, IdentityProviderResponseParser: parser})
			require.NoError(t, err)
			tm := value.(*entraidTokenManager)
			defer tm.Stop()
			unblock := sync.OnceFunc(func() { close(parser.release) })
			defer unblock() // release the parser before Stop even if a check fails
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
	idp := &gatedIdentityProvider{release: make(chan struct{}, 1)}
	defer close(idp.release)
	m, err := NewTokenManager(idp, TokenManagerOptions{
		LowerRefreshBound: time.Hour, IdentityProviderResponseParser: lifetimeParser(time.Minute),
		RetryOptions: RetryOptions{MaxDelay: time.Hour, InitialDelay: time.Hour},
	})
	require.NoError(t, err)
	tm := m.(*entraidTokenManager)
	defer tm.Stop()
	current, err := tm.GetToken(false)
	require.NoError(t, err)
	_, err = tm.GetToken(false) // the cached token is already due for refresh
	require.NoError(t, err)
	tm.tokenRWLock.RLock()
	flight := tm.acquire
	tm.tokenRWLock.RUnlock()
	require.NotNil(t, flight)
	idp.release <- struct{}{}
	<-flight.done
	require.ErrorContains(t, flight.err, "identity provider unavailable")

	for range 2 { // restart again after the last unsubscribe
		stop, err := tm.StartSubscriptions(context.Background(), lifecycleTestListener{})
		require.NoError(t, err)
		got, err := tm.GetToken(false)
		require.NoError(t, err)
		require.Same(t, current, got)
		require.NoError(t, stop())
	}
	tm.tokenRWLock.RLock()
	defer tm.tokenRWLock.RUnlock()
	require.Nil(t, tm.acquire, "reconnects must respect the failed demand request's backoff")
	require.EqualValues(t, 2, idp.calls.Load())
}

func TestArmedTimerHonorsDemandBackoff(t *testing.T) {
	idp := &failingIdentityProvider{fail: func(call int32) bool { return call > 1 }}
	m, err := NewTokenManager(idp, TokenManagerOptions{
		LowerRefreshBound: time.Hour, IdentityProviderResponseParser: lifetimeParser(time.Minute),
		RetryOptions: RetryOptions{InitialDelay: time.Hour, MaxDelay: time.Hour},
	})
	require.NoError(t, err)
	tm := m.(*entraidTokenManager)
	stop, err := tm.Start(lifecycleTestListener{})
	require.NoError(t, err)
	defer stop()

	// Capture the timer's cache version before a reader requests the due token.
	tm.tokenRWLock.RLock()
	scheduled := tm.changed
	tm.tokenRWLock.RUnlock()
	_, err = tm.GetToken(false)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		tm.tokenRWLock.RLock()
		defer tm.tokenRWLock.RUnlock()
		return time.Now().Before(tm.nextDemand)
	}, time.Second, time.Millisecond, "reader refresh did not enter backoff")

	// Dispatch that timer now, inside the backoff set by the failed request.
	_, err = tm.getToken(context.Background(), true, scheduled, 0)
	require.EqualValues(t, 2, idp.calls.Load(), "armed timer bypassed demand backoff")
	require.NoError(t, err)
}

func TestRenewalAfterNonForcedRecovery(t *testing.T) {
	idp := &failingIdentityProvider{fail: func(call int32) bool { return call == 1 }}
	tm, err := NewTokenManager(idp, TokenManagerOptions{
		IdentityProviderResponseParser: lifetimeParser(2 * time.Second),
		ExpirationRefreshRatio:         0.05, // renew after 100 ms
		RetryOptions:                   RetryOptions{MaxDelay: time.Hour},
	})
	require.NoError(t, err)
	_, err = tm.GetToken(true) // enter backoff with no usable cached token
	require.ErrorContains(t, err, "identity provider unavailable")

	// Start acquires a token without forcing a refresh. Without another read,
	// the scheduler must renew that token before the old backoff expires.
	listener := &renewalListener{next: make(chan struct{}, 1)}
	stop, err := tm.Start(listener)
	require.NoError(t, err)
	defer stop()
	select {
	case <-listener.next:
	case <-time.After(time.Second):
		t.Fatal("replacement token was not renewed after recovery")
	}
}

func TestRetiredTimerCannotRefresh(t *testing.T) {
	for _, state := range []string{"stopped", "restarted"} {
		t.Run(state, func(t *testing.T) {
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

			// A timer fired just before the last unsubscribe, but has not requested its token yet.
			require.NoError(t, stop())
			if state == "restarted" {
				_, err = tm.StartSubscriptions(ctx, lifecycleTestListener{})
				require.NoError(t, err)
			}
			_, err = tm.getToken(ctx, true, scheduled, 0)
			require.NoError(t, err)
			tm.tokenRWLock.RLock()
			defer tm.tokenRWLock.RUnlock()
			// A request from the stale timer would still be blocked in the provider
			// and remain in tm.acquire.
			require.Nil(t, tm.acquire, "retired timer installed a new request")
			require.EqualValues(t, 1, idp.calls.Load(), "only the initial token was needed")
		})
	}
}
