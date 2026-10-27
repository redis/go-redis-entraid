package entraid

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis-entraid/shared"
	"github.com/redis/go-redis-entraid/token"
	"github.com/redis/go-redis/v9/auth"
	"github.com/stretchr/testify/require"
)

// regressionManager gives tests deterministic control over manager startup,
// acquisition, and stop without external identity-provider requests.
type regressionManager struct {
	mu           sync.Mutex
	token        *token.Token
	startErr     error
	getErr       error
	stopErr      error
	starts       int
	stops        int
	startGate    <-chan struct{}
	startEntered chan struct{}
	getGate      <-chan struct{}
	getEntered   chan struct{}
}

func (m *regressionManager) GetToken(bool) (*token.Token, error) {
	if m.getGate != nil {
		<-m.getGate
	}
	return m.token, m.getErr
}
func (m *regressionManager) Start(manager.TokenListener) (manager.StopFunc, error) {
	m.mu.Lock()
	m.starts++
	m.mu.Unlock()
	if m.startGate != nil {
		<-m.startGate
	}
	if m.startErr != nil {
		return nil, m.startErr
	}
	return func() error { m.mu.Lock(); defer m.mu.Unlock(); m.stops++; return m.stopErr }, nil
}
func (m *regressionManager) StartWithContext(ctx context.Context, listener manager.TokenListener) (manager.StopFunc, error) {
	if m.startEntered != nil {
		m.startEntered <- struct{}{}
	}
	m.mu.Lock()
	m.starts++
	m.mu.Unlock()
	if m.startGate != nil {
		select {
		case <-m.startGate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.startErr != nil {
		return nil, m.startErr
	}
	return func() error { m.mu.Lock(); defer m.mu.Unlock(); m.stops++; return m.stopErr }, nil
}
func (m *regressionManager) GetTokenWithContext(ctx context.Context, force bool) (*token.Token, error) {
	if m.getEntered != nil {
		select {
		case m.getEntered <- struct{}{}:
		default:
		}
	}
	if m.getGate != nil {
		select {
		case <-m.getGate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.token, m.getErr
}

type silentCredentialsListener struct{}

func (silentCredentialsListener) OnNext(auth.Credentials) {}
func (silentCredentialsListener) OnError(error)           {}

func regressionToken() *token.Token {
	return token.New("test", "test", "test-token", time.Now().Add(time.Hour), time.Now(), time.Hour.Milliseconds())
}

func TestSubscribeStartErrorDoesNotPoisonRetry(t *testing.T) {
	m := &regressionManager{token: regressionToken(), startErr: errors.New("start failed")}
	p := &entraidCredentialsProvider{tokenManager: m}
	_, _, err := p.Subscribe(silentCredentialsListener{})
	require.Error(t, err)

	m.startErr = nil
	done := make(chan error, 1)
	go func() { _, _, err := p.Subscribe(silentCredentialsListener{}); done <- err }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("retry blocked after failed manager start")
	}
}

func TestSubscribeWithContextCanceledDuringManagerStart(t *testing.T) {
	gate := make(chan struct{})
	m := &regressionManager{token: regressionToken(), startGate: gate, startEntered: make(chan struct{}, 1)}
	p := &entraidCredentialsProvider{tokenManager: m}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, _, err := p.SubscribeWithContext(ctx, silentCredentialsListener{}); done <- err }()
	select {
	case <-m.startEntered:
	case <-time.After(time.Second):
		t.Fatal("Start was not entered")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("canceled subscription remained blocked in manager startup")
	}
	close(gate)
}

func TestSubscribeWithContextCanceledDuringTokenAcquisition(t *testing.T) {
	gate := make(chan struct{})
	m := &regressionManager{token: regressionToken(), getGate: gate, getEntered: make(chan struct{}, 1)}
	p := &entraidCredentialsProvider{tokenManager: m}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, _, err := p.SubscribeWithContext(ctx, silentCredentialsListener{}); done <- err }()
	select {
	case <-m.getEntered:
	case <-time.After(time.Second):
		t.Fatal("GetToken was not entered")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("canceled subscription remained blocked in token acquisition")
	}
	close(gate)
}

type pausedIdentityProvider struct {
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   int
	pauseAt int
}

func (p *pausedIdentityProvider) RequestToken(context.Context) (shared.IdentityProviderResponse, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	select {
	case p.entered <- struct{}{}:
	default:
	}
	if p.pauseAt == 0 || call == p.pauseAt {
		<-p.release // Deliberately ignores cancellation to model an uninterruptible SDK callback.
	}
	return shared.NewIDPResponse(shared.ResponseTypeRawToken, "built-in-manager-token")
}

type fixedTokenParser struct {
	mu           sync.Mutex
	calls        int
	firstExpired bool
}

func (p *fixedTokenParser) ParseResponse(shared.IdentityProviderResponse) (*token.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.firstExpired && p.calls == 1 {
		return token.New("test", "test", "expired", time.Now().Add(-time.Minute), time.Now().Add(-time.Hour), time.Hour.Milliseconds()), nil
	}
	return regressionToken(), nil
}

func TestBuiltInManagerCancellationKeepsSharedAcquisition(t *testing.T) {
	idp := &pausedIdentityProvider{entered: make(chan struct{}, 2), release: make(chan struct{})}
	tm, err := manager.NewTokenManager(idp, manager.TokenManagerOptions{IdentityProviderResponseParser: &fixedTokenParser{}})
	require.NoError(t, err)
	p := &entraidCredentialsProvider{tokenManager: tm}

	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(idp.release) }) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	peerCtx, cancelPeer := context.WithCancel(context.Background())
	defer cancelPeer()
	first := make(chan error, 1)
	go func() { _, _, err := p.SubscribeWithContext(ctx, silentCredentialsListener{}); first <- err }()
	select {
	case <-idp.entered:
	case <-time.After(time.Second):
		t.Fatal("built-in manager did not reach identity provider")
	}

	type result struct {
		unsubscribe auth.UnsubscribeFunc
		err         error
	}
	second := make(chan result, 1)
	go func() {
		_, u, err := p.SubscribeWithContext(peerCtx, &distinctSilentListener{})
		second <- result{u, err}
	}()
	cancel()
	select {
	case err := <-first:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("canceled caller remained blocked in built-in acquisition")
	}
	release.Do(func() { close(idp.release) })
	select {
	case r := <-second:
		require.NoError(t, r.err)
		defer r.unsubscribe()
	case <-time.After(time.Second):
		t.Fatal("remaining caller did not receive shared acquisition")
	}
	idp.mu.Lock()
	calls := idp.calls
	idp.mu.Unlock()
	require.Equal(t, 1, calls, "canceled caller must not cancel or duplicate the shared request")
	p.rwLock.RLock()
	count := len(p.listeners)
	p.rwLock.RUnlock()
	require.Equal(t, 1, count, "canceled caller must not leave a listener registered")
}

func TestBuiltInManagerGetTokenCancellationIsShared(t *testing.T) {
	idp := &pausedIdentityProvider{entered: make(chan struct{}, 2), release: make(chan struct{}), pauseAt: 2}
	parser := &fixedTokenParser{firstExpired: true}
	tm, err := manager.NewTokenManager(idp, manager.TokenManagerOptions{
		IdentityProviderResponseParser: parser,
		RetryOptions:                   manager.RetryOptions{InitialDelay: 5 * time.Second},
	})
	require.NoError(t, err)
	p := &entraidCredentialsProvider{tokenManager: tm}
	stop, err := tm.Start(tokenListenerFromCP(p))
	require.NoError(t, err)
	p.stopTokenManager = stop
	defer stop()
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(idp.release) }) })

	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, _, err := p.SubscribeWithContext(ctx, silentCredentialsListener{}); first <- err }()
	select {
	case <-idp.entered: // initial startup request
	case <-time.After(time.Second):
		t.Fatal("initial built-in request did not complete")
	}
	select {
	case <-idp.entered: // subscription refresh request
	case <-time.After(time.Second):
		t.Fatal("built-in GetToken did not reach identity provider")
	}
	second := make(chan error, 1)
	go func() {
		_, _, err := p.SubscribeWithContext(context.Background(), &distinctSilentListener{})
		second <- err
	}()
	cancel()
	select {
	case err := <-first:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("canceled GetToken waiter remained blocked")
	}
	release.Do(func() { close(idp.release) })
	select {
	case err := <-second:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("second subscriber did not receive shared GetToken result")
	}
	idp.mu.Lock()
	calls := idp.calls
	idp.mu.Unlock()
	require.Equal(t, 2, calls, "subscription callers should share one refresh request")
	p.rwLock.RLock()
	count := len(p.listeners)
	p.rwLock.RUnlock()
	require.Equal(t, 1, count)
	require.NoError(t, stop())
	restarted, err := tm.Start(tokenListenerFromCP(p))
	require.NoError(t, err, "built-in manager must restart after a stopped lifecycle context")
	require.NoError(t, restarted())
}

type distinctSilentListener struct{}

func (*distinctSilentListener) OnNext(auth.Credentials) {}
func (*distinctSilentListener) OnError(error)           {}

func TestLastUnsubscribeCannotStopConcurrentNewSubscription(t *testing.T) {
	m := &regressionManager{token: regressionToken()}
	p := &entraidCredentialsProvider{tokenManager: m}
	_, oldUnsubscribe, err := p.Subscribe(silentCredentialsListener{})
	require.NoError(t, err)
	gate, entered := make(chan struct{}), make(chan struct{}, 1)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate) }) })
	m.getGate, m.getEntered = gate, entered
	type result struct {
		unsubscribe auth.UnsubscribeFunc
		err         error
	}
	done := make(chan result, 1)
	go func() { _, u, err := p.Subscribe(&distinctSilentListener{}); done <- result{u, err} }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("new subscription did not reach acquisition")
	}
	oldDone := make(chan error, 1)
	go func() { oldDone <- oldUnsubscribe() }()
	select {
	case err := <-oldDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("unsubscribe must not wait for external acquisition")
	}
	release.Do(func() { close(gate) })
	select {
	case r := <-done:
		require.NoError(t, r.err)
		defer r.unsubscribe()
	case <-time.After(time.Second):
		t.Fatal("new subscription did not complete")
	}
	m.mu.Lock()
	stops := m.stops
	m.mu.Unlock()
	require.Zero(t, stops, "pending registration must protect renewal")
	p.rwLock.RLock()
	registered := len(p.listeners)
	p.rwLock.RUnlock()
	require.Equal(t, 1, registered)
}

func TestUnsubscribeIsIdempotent(t *testing.T) {
	m := &regressionManager{token: regressionToken()}
	p := &entraidCredentialsProvider{tokenManager: m}
	_, unsubscribe, err := p.Subscribe(silentCredentialsListener{})
	require.NoError(t, err)
	require.NoError(t, unsubscribe())
	require.NoError(t, unsubscribe())
}

func TestStopErrorDoesNotPoisonProviderLifecycleLock(t *testing.T) {
	m := &regressionManager{token: regressionToken(), stopErr: errors.New("stop failed")}
	p := &entraidCredentialsProvider{tokenManager: m}
	_, unsubscribe, err := p.Subscribe(silentCredentialsListener{})
	require.NoError(t, err)
	require.ErrorContains(t, unsubscribe(), "stop failed")

	done := make(chan error, 1)
	go func() {
		_, nextUnsubscribe, err := p.Subscribe(&distinctSilentListener{})
		if err == nil {
			err = nextUnsubscribe()
		}
		done <- err
	}()
	select {
	case err := <-done:
		require.ErrorContains(t, err, "stop failed")
	case <-time.After(time.Second):
		t.Fatal("stop error left provider lifecycle locked")
	}
}

func TestDuplicateUnsubscribeLeaseDoesNotRemoveLaterRegistration(t *testing.T) {
	m := &regressionManager{token: regressionToken()}
	p := &entraidCredentialsProvider{tokenManager: m}
	listener := silentCredentialsListener{}
	_, u1, err := p.Subscribe(listener)
	require.NoError(t, err)
	_, u2, err := p.Subscribe(listener)
	require.NoError(t, err)
	require.NoError(t, u1())
	_, u3, err := p.Subscribe(listener)
	require.NoError(t, err)
	require.NoError(t, u2())
	p.rwLock.RLock()
	require.Len(t, p.listeners, 1)
	p.rwLock.RUnlock()
	require.NoError(t, u3())
}
