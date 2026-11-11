package manager

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis-entraid/shared"
	"github.com/redis/go-redis-entraid/token"
	"github.com/stretchr/testify/require"
)

// failingIdentityProvider fails the requests for which fail returns true.
type failingIdentityProvider struct {
	calls atomic.Int32
	fail  func(call int32) bool
}

func (p *failingIdentityProvider) RequestToken(context.Context) (shared.IdentityProviderResponse, error) {
	if call := p.calls.Add(1); p.fail(call) {
		return nil, errors.New("identity provider unavailable")
	}
	return shared.NewIDPResponse(shared.ResponseTypeRawToken, newTestJWTToken(time.Now().Add(time.Hour)))
}

type renewalListener struct {
	next         chan struct{}
	failedRounds atomic.Int32
}

func (l *renewalListener) OnNext(*token.Token) {
	select {
	case l.next <- struct{}{}:
	default:
	}
}

func (l *renewalListener) OnError(error) { l.failedRounds.Add(1) }

func TestRenewalResumesAfterFailedRenewal(t *testing.T) {
	tests := []struct {
		name      string
		retryable bool
		failing   int32 // consecutive renewal requests that fail
	}{
		{name: "retries exhausted", retryable: true, failing: 4},
		{name: "not retryable", retryable: false, failing: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The first request is the initial acquisition in Start.
			idp := &failingIdentityProvider{fail: func(call int32) bool { return call > 1 && call <= 1+tt.failing }}
			tm, err := NewTokenManager(idp, TokenManagerOptions{
				LowerRefreshBound: time.Hour, // renew right away
				RetryOptions: RetryOptions{
					IsRetryable:       func(error) bool { return tt.retryable },
					MaxAttempts:       2,
					InitialDelay:      10 * time.Millisecond,
					MaxDelay:          10 * time.Millisecond,
					BackoffMultiplier: 1,
				},
			})
			require.NoError(t, err)
			listener := &renewalListener{next: make(chan struct{}, 1)}
			stop, err := tm.Start(listener)
			require.NoError(t, err)
			defer stop()

			select {
			case <-listener.next:
				require.EqualValues(t, 2, listener.failedRounds.Load(), "each failed round must be reported")
				require.GreaterOrEqual(t, idp.calls.Load(), 2+tt.failing)
			case <-time.After(2 * time.Second):
				t.Fatalf("renewal did not resume after %d failed rounds", listener.failedRounds.Load())
			}
		})
	}
}

type timedListener struct{ failures chan time.Time }

func (l *timedListener) OnNext(*token.Token) {}

func (l *timedListener) OnError(error) {
	select {
	case l.failures <- time.Now():
	default:
	}
}

func TestNewRoundDelay(t *testing.T) {
	tests := []struct {
		name         string
		initialDelay time.Duration
		maxDelay     time.Duration
		want         time.Duration
	}{
		// not MaxDelay plus InitialDelay
		{name: "MaxDelay", initialDelay: 400 * time.Millisecond, maxDelay: 800 * time.Millisecond, want: 800 * time.Millisecond},
		{name: "InitialDelay when MaxDelay is shorter", initialDelay: 300 * time.Millisecond, maxDelay: time.Millisecond, want: 300 * time.Millisecond},
		{name: "InitialDelay when MaxDelay is negative", initialDelay: 300 * time.Millisecond, maxDelay: -time.Second, want: 300 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idp := &failingIdentityProvider{fail: func(call int32) bool { return call > 1 }}
			tm, err := NewTokenManager(idp, TokenManagerOptions{
				LowerRefreshBound: time.Hour, // renew right away
				RetryOptions: RetryOptions{
					IsRetryable:  func(error) bool { return false },
					InitialDelay: tt.initialDelay,
					MaxDelay:     tt.maxDelay,
				},
			})
			require.NoError(t, err)
			listener := &timedListener{failures: make(chan time.Time, 2)}
			stop, err := tm.Start(listener)
			require.NoError(t, err)
			defer stop()

			first := <-listener.failures
			select {
			case second := <-listener.failures:
				require.GreaterOrEqual(t, second.Sub(first), tt.want)
				require.Less(t, second.Sub(first), tt.want+300*time.Millisecond)
			case <-time.After(3 * time.Second):
				t.Fatal("no new round after a failed one")
			}
		})
	}
}

func TestGetTokenReturnsCurrentTokenWhileRenewalFails(t *testing.T) {
	newManager := func(t *testing.T, idp shared.IdentityProvider, lifetime time.Duration) TokenManager {
		tm, err := NewTokenManager(idp, TokenManagerOptions{
			LowerRefreshBound:              time.Hour, // every request is a renewal
			IdentityProviderResponseParser: lifetimeParser(lifetime),
		})
		require.NoError(t, err)
		return tm
	}

	t.Run("valid token", func(t *testing.T) {
		idp := &failingIdentityProvider{fail: func(call int32) bool { return call > 1 }}
		tm := newManager(t, idp, time.Hour)
		current, err := tm.GetToken(false)
		require.NoError(t, err)

		got, err := tm.GetToken(false)
		require.NoError(t, err)
		require.Same(t, current, got)
		require.EqualValues(t, 2, idp.calls.Load(), "a renewal must still be attempted")

		_, err = tm.GetToken(true)
		require.ErrorContains(t, err, "identity provider unavailable")
	})

	t.Run("token about to expire", func(t *testing.T) {
		idp := &failingIdentityProvider{fail: func(call int32) bool { return call > 1 }}
		tm := newManager(t, idp, currentTokenMinValidity/2)
		_, err := tm.GetToken(false)
		require.NoError(t, err)

		_, err = tm.GetToken(false)
		require.ErrorContains(t, err, "identity provider unavailable")
	})
}

// gatedIdentityProvider issues a token on the first request. Later requests
// wait for release and fail.
type gatedIdentityProvider struct {
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (p *gatedIdentityProvider) RequestToken(context.Context) (shared.IdentityProviderResponse, error) {
	if p.calls.Add(1) == 1 {
		return shared.NewIDPResponse(shared.ResponseTypeRawToken, "token")
	}
	select {
	case p.entered <- struct{}{}:
	default:
	}
	<-p.release
	return nil, errors.New("identity provider unavailable")
}

func TestNonForcedCallerSharingFailedForcedRequestGetsCurrentToken(t *testing.T) {
	idp := &gatedIdentityProvider{entered: make(chan struct{}, 1), release: make(chan struct{})}
	tokenManager, err := NewTokenManager(idp, TokenManagerOptions{
		LowerRefreshBound:              time.Hour, // every request is a renewal
		IdentityProviderResponseParser: lifetimeParser(time.Hour),
	})
	require.NoError(t, err)
	tm := tokenManager.(*entraidTokenManager)
	current, err := tm.GetToken(false)
	require.NoError(t, err)

	forced := make(chan error, 1)
	go func() {
		_, err := tm.GetTokenWithContext(context.Background(), true)
		forced <- err
	}()
	<-idp.entered
	type result struct {
		token *token.Token
		err   error
	}
	joined := make(chan result, 1)
	go func() {
		tk, err := tm.GetTokenWithContext(context.Background(), false)
		joined <- result{tk, err}
	}()
	time.Sleep(50 * time.Millisecond) // let the non-forced caller join the forced request
	close(idp.release)

	require.ErrorContains(t, <-forced, "identity provider unavailable")
	r := <-joined
	require.NoError(t, r.err)
	require.Same(t, current, r.token)
	require.EqualValues(t, 2, idp.calls.Load(), "the non-forced caller must share the forced request")
}

// lifetimeParser returns tokens that expire after lifetime.
type lifetimeParser time.Duration

func (p lifetimeParser) ParseResponse(shared.IdentityProviderResponse) (*token.Token, error) {
	now := time.Now()
	return token.New("user", "password", "raw", now.Add(time.Duration(p)), now, time.Duration(p).Milliseconds()), nil
}
