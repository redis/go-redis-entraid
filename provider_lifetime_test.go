package entraid

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis-entraid/internal"
	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis-entraid/shared"
	"github.com/redis/go-redis-entraid/token"
	"github.com/stretchr/testify/require"
)

// The first request supplies the constructor's token. Each later request is
// observable before release so a socket can disappear mid-refresh.
type demandIdentity struct {
	calls    atomic.Int32
	requests chan context.Context
	release  chan struct{}
	lifetime time.Duration
}

func (p *demandIdentity) RequestToken(ctx context.Context) (shared.IdentityProviderResponse, error) {
	if p.calls.Add(1) == 1 {
		return shared.NewIDPResponse(shared.ResponseTypeRawToken, "initial")
	}
	p.requests <- ctx
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.release:
		return shared.NewIDPResponse(shared.ResponseTypeRawToken, "replacement")
	}
}
func (p *demandIdentity) ParseResponse(r shared.IdentityProviderResponse) (*token.Token, error) {
	raw, err := r.RawToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	return token.New("user", raw, raw, now.Add(p.lifetime), now, p.lifetime.Milliseconds()), nil
}

const demandRetry = 20 * time.Millisecond

func demandProvider(t *testing.T, lifetime time.Duration) (*demandIdentity, CredentialsProviderCloser) {
	t.Helper()
	idp := &demandIdentity{lifetime: lifetime, requests: make(chan context.Context, 100), release: make(chan struct{}, 1)}
	m, err := manager.NewTokenManager(idp, manager.TokenManagerOptions{
		IdentityProviderResponseParser: idp, LowerRefreshBound: time.Hour, RequestTimeout: 5 * time.Second,
		RetryOptions: manager.RetryOptions{MaxAttempts: 1, InitialDelay: demandRetry, MaxDelay: demandRetry},
	})
	require.NoError(t, err)
	p, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
	require.NoError(t, err)
	owner := p.(CredentialsProviderCloser)
	t.Cleanup(func() { require.NoError(t, owner.Close()) })
	return idp, owner
}

func demandSignal[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("expected demand or shutdown signal")
		var zero T
		return zero
	}
}
func expectNoDemand(t *testing.T, idp *demandIdentity) {
	t.Helper()
	select {
	case <-idp.requests:
		t.Fatal("started another identity request without demand")
	case <-time.After(4 * demandRetry):
	}
}

func TestReconnectRefreshDoesNotRequireAnOpenSocket(t *testing.T) {
	idp, p := demandProvider(t, time.Minute) // both tokens are eligible, but already due for renewal
	expectNoDemand(t, idp)                   // construction alone must not start a renewal loop
	for i := 0; i < 20; i++ {
		tk, unsubscribe, err := p.Subscribe(&distinctSilentListener{})
		require.NoError(t, err)
		require.Equal(t, "initial", tk.RawCredentials())
		require.NoError(t, unsubscribe()) // Redis would detach this listener after rejected AUTH
	}
	request := demandSignal(t, idp.requests)
	require.NoError(t, request.Err(), "last unsubscribe canceled the demand refresh")
	require.EqualValues(t, 2, idp.calls.Load(), "reconnects must share one pending request")
	idp.release <- struct{}{}       // no listeners remain
	demandSignal(t, request.Done()) // the request finished; there is still no demand
	expectNoDemand(t, idp)
	tk, unsubscribe, err := p.Subscribe(&distinctSilentListener{})
	require.NoError(t, err)
	require.Equal(t, "replacement", tk.RawCredentials())
	request = demandSignal(t, idp.requests)
	require.NoError(t, unsubscribe())
	require.NoError(t, request.Err())
	require.NoError(t, p.Close())
	demandSignal(t, request.Done())
	require.ErrorIs(t, request.Err(), context.Canceled)
}

func TestOwnerCloseCancelsPendingUnboundedSubscription(t *testing.T) {
	idp, p := demandProvider(t, time.Millisecond) // too little validity to return the cache
	pending := make(chan error, 1)
	go func() { _, _, err := p.Subscribe(&distinctSilentListener{}); pending <- err }()
	request := demandSignal(t, idp.requests)
	closed := make(chan error, 1)
	go func() { closed <- p.Close() }()
	require.NoError(t, demandSignal(t, closed))
	demandSignal(t, request.Done())
	require.ErrorIs(t, request.Err(), context.Canceled)
	require.ErrorIs(t, demandSignal(t, pending), context.Canceled)
}

type rotatingManager struct {
	regressionManager
	listener manager.TokenListener
	next     *token.Token
}

func (m *rotatingManager) StartSubscriptions(_ context.Context, l manager.TokenListener) (internal.StopRenewal, error) {
	m.listener = l
	return func() error { return nil }, nil
}
func (m *rotatingManager) GetTokenWithContext(context.Context, bool) (*token.Token, error) {
	snapshot := m.token
	if m.next != nil {
		// Notify the provider before the subscriber can register with this snapshot.
		m.token, m.next = m.next, nil
		m.listener.OnNext(m.token)
	}
	return snapshot, nil
}

func TestSubscribeDoesNotMissRefreshDuringRegistration(t *testing.T) {
	now := time.Now()
	fresh := token.New("user", "new", "new", now.Add(time.Hour), now, time.Hour.Milliseconds())
	m := &rotatingManager{regressionManager: regressionManager{token: regressionToken()}, next: fresh}
	p, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
	require.NoError(t, err)
	defer p.(CredentialsProviderCloser).Close()
	tk, unsub, err := p.Subscribe(&distinctSilentListener{})
	require.NoError(t, err)
	defer unsub()
	require.Equal(t, fresh.RawCredentials(), tk.RawCredentials())
}
