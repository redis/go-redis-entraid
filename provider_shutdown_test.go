package entraid

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis-entraid/token"
	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/auth"
	"github.com/stretchr/testify/require"
)

type lifecycleTestManager struct {
	starts     atomic.Int32
	stops      atomic.Int32
	active     atomic.Int32
	startErr   error
	stopErr    error
	stopHook   func() error
	getEntered chan struct{}
	getRelease <-chan struct{}
}

func (m *lifecycleTestManager) GetToken(bool) (*token.Token, error) {
	if m.getEntered != nil {
		close(m.getEntered)
		<-m.getRelease
	}
	now := time.Now()
	return token.New("test", "test", "token", now.Add(time.Hour), now, time.Hour.Milliseconds()), nil
}

func (m *lifecycleTestManager) Start(manager.TokenListener) (manager.StopFunc, error) {
	if m.startErr != nil {
		return nil, m.startErr
	}
	m.starts.Add(1)
	m.active.Add(1)
	return func() error {
		m.stops.Add(1)
		if m.stopHook != nil {
			return m.stopHook()
		}
		if m.stopErr != nil {
			return m.stopErr
		}
		m.active.Add(-1)
		return nil
	}, nil
}

func TestProviderConstructorStillFailsWhenManagerStartFails(t *testing.T) {
	startErr := errors.New("token initialization failed")
	provider, err := NewCredentialsProvider(&lifecycleTestManager{startErr: startErr}, CredentialsProviderOptions{})
	require.ErrorIs(t, err, startErr)
	require.Nil(t, provider)
}

// TestProviderCanCloseAfterRedisDialFailure guards the ownership path that
// Client.Close cannot reach when a failed dial meant Subscribe never ran.
func TestProviderCanCloseAfterRedisDialFailure(t *testing.T) {
	m := &lifecycleTestManager{}
	provider, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
	require.NoError(t, err)
	require.EqualValues(t, 1, m.active.Load(), "constructor starts manager eagerly")

	client := redis.NewClient(&redis.Options{
		StreamingCredentialsProvider: provider,
		MaxRetries:                   -1,
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("Redis unavailable")
		},
	})
	require.Error(t, client.Ping(context.Background()).Err())
	require.NoError(t, client.Close())
	require.Zero(t, m.stops.Load(), "client close must not own or stop provider")
	require.EqualValues(t, 1, m.active.Load(), "failed dial left no subscription to clean up")

	closer, ok := provider.(interface{ Close() error })
	require.True(t, ok, "provider must expose an explicit owner-controlled Close method")
	require.NoError(t, closer.Close())
	require.EqualValues(t, 1, m.stops.Load())
	require.Zero(t, m.active.Load())
}

// TestProviderCloseIsIdempotentAndRejectsNewSubscriptions specifies a terminal
// provider lifecycle: Close may be repeated, but a closed provider cannot restart.
func TestProviderCloseIsIdempotentAndRejectsNewSubscriptions(t *testing.T) {
	m := &lifecycleTestManager{}
	provider, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
	require.NoError(t, err)
	closer, ok := provider.(interface{ Close() error })
	require.True(t, ok, "provider must expose an explicit owner-controlled Close method")

	const concurrentClosers = 32
	var wg sync.WaitGroup
	closeErrors := make(chan error, concurrentClosers)
	for i := 0; i < concurrentClosers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			closeErrors <- closer.Close()
		}()
	}
	wg.Wait()
	close(closeErrors)
	for closeErr := range closeErrors {
		require.NoError(t, closeErr)
	}
	require.EqualValues(t, 1, m.stops.Load(), "concurrent repeated Close calls stop manager exactly once")

	listener := &lifecycleTestListener{}
	_, unsubscribe, err := provider.Subscribe(listener)
	require.Error(t, err, "Subscribe after Close must fail instead of restarting the manager")
	require.Nil(t, unsubscribe)
	require.EqualValues(t, 1, m.starts.Load())
}

// TestClosingOneRedisClientDoesNotStopSharedProvider verifies client cleanup
// is scoped to its own subscription; only the provider owner can stop the shared
// manager, including when multiple clients refer to the provider.
type lifecycleTestListener struct{ id byte }

func (*lifecycleTestListener) OnNext(auth.Credentials) {}
func (*lifecycleTestListener) OnError(error)           {}

func TestProviderCloseRetriesFailedStop(t *testing.T) {
	stopErr := errors.New("manager stop failed")
	var attempts atomic.Int32
	var m *lifecycleTestManager
	m = &lifecycleTestManager{stopHook: func() error {
		if attempts.Add(1) == 1 {
			return stopErr
		}
		m.active.Add(-1)
		return nil
	}}
	provider, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
	require.NoError(t, err)
	closer := provider.(CredentialsProviderCloser)
	listener := &lifecycleTestListener{}
	_, unsubscribe, err := provider.Subscribe(listener)
	require.NoError(t, err)
	require.NotNil(t, unsubscribe)

	require.ErrorIs(t, closer.Close(), stopErr)
	require.EqualValues(t, 1, m.active.Load(), "first failure must leave worker active")
	require.NoError(t, closer.Close(), "a later Close retries cleanup")
	require.Zero(t, m.active.Load())
	require.EqualValues(t, 2, m.stops.Load())
	_, unsubscribe, err = provider.Subscribe(&lifecycleTestListener{})
	require.Error(t, err, "failed Close still terminally closes subscriptions")
	require.Nil(t, unsubscribe)
}

func TestProviderCloseDoesNotDeadlockCallbackUnsubscribe(t *testing.T) {
	callbackEntered := make(chan struct{})
	stopEntered := make(chan struct{})
	callbackRelease := make(chan struct{})
	callbackDone := make(chan struct{})
	var unsubscribe auth.UnsubscribeFunc
	var m *lifecycleTestManager
	m = &lifecycleTestManager{stopHook: func() error {
		close(stopEntered)
		<-callbackDone
		m.active.Add(-1)
		return nil
	}}
	provider, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
	require.NoError(t, err)
	listener := &callbackUnsubscribeListener{
		entered:     callbackEntered,
		release:     callbackRelease,
		done:        callbackDone,
		unsubscribe: func() error { return unsubscribe() },
	}
	_, unsubscribe, err = provider.Subscribe(listener)
	require.NoError(t, err)
	go provider.(*entraidCredentialsProvider).onTokenNext(nil)
	<-callbackEntered

	closeDone := make(chan error, 1)
	go func() { closeDone <- provider.(CredentialsProviderCloser).Close() }()
	select {
	case <-stopEntered:
	case <-time.After(time.Second):
		t.Fatal("Close did not enter StopFunc")
	}
	close(callbackRelease)
	select {
	case err := <-closeDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Close deadlocked with callback unsubscribe")
	}
}

type callbackUnsubscribeListener struct {
	entered, release, done chan struct{}
	unsubscribe            func() error
}

func (l *callbackUnsubscribeListener) OnNext(auth.Credentials) {
	close(l.entered)
	<-l.release
	_ = l.unsubscribe()
	close(l.done)
}
func (*callbackUnsubscribeListener) OnError(error) {}

func TestProviderCloseWaitsForInFlightSubscribe(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	m := &lifecycleTestManager{getEntered: entered, getRelease: release}
	value, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
	require.NoError(t, err)
	p := value.(*entraidCredentialsProvider)
	type result struct {
		unsubscribe auth.UnsubscribeFunc
		err         error
	}
	subscribeDone := make(chan result, 1)
	go func() { _, u, err := p.Subscribe(&lifecycleTestListener{}); subscribeDone <- result{u, err} }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not enter acquisition")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- p.Close() }()
	require.Eventually(t, func() bool { p.stateMu.Lock(); defer p.stateMu.Unlock(); return p.closed }, time.Second, time.Millisecond, "Close must first reject future registration")
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before acquisition finished: %v", err)
	default:
	}
	once.Do(func() { close(release) })
	select {
	case r := <-subscribeDone:
		require.ErrorContains(t, r.err, "closed")
		require.Nil(t, r.unsubscribe)
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not finish")
	}
	select {
	case err := <-closeDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Close did not finish")
	}
	require.EqualValues(t, 1, m.stops.Load())
}
