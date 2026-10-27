package entraid

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis-entraid/token"
	"github.com/stretchr/testify/require"
)

type pendingFailureManager struct {
	entered  chan struct{}
	release  chan struct{}
	fail     bool
	stops    chan struct{}
	stopOnce sync.Once
}

func (m *pendingFailureManager) Start(manager.TokenListener) (manager.StopFunc, error) {
	return func() error { m.stopOnce.Do(func() { close(m.stops) }); return nil }, nil
}
func (m *pendingFailureManager) GetToken(bool) (*token.Token, error) {
	if m.fail {
		close(m.entered)
		<-m.release
		return nil, errors.New("identity service unavailable")
	}
	return regressionToken(), nil
}

func TestFailedPendingSubscriptionCompletesLastUnsubscribeCleanup(t *testing.T) {
	m := &pendingFailureManager{entered: make(chan struct{}), release: make(chan struct{}), stops: make(chan struct{})}
	p, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
	require.NoError(t, err)
	_, unsubscribe, err := p.Subscribe(silentCredentialsListener{})
	require.NoError(t, err)
	m.fail = true
	result := make(chan error, 1)
	go func() { _, _, err := p.Subscribe(&distinctSilentListener{}); result <- err }()
	select {
	case <-m.entered:
	case <-time.After(time.Second):
		t.Fatal("pending subscription did not enter acquisition")
	}
	require.NoError(t, unsubscribe())
	select {
	case <-m.stops:
		t.Fatal("last unsubscribe stopped an active acquisition")
	default:
	}
	close(m.release)
	select {
	case err := <-result:
		require.ErrorContains(t, err, "identity service unavailable")
	case <-time.After(time.Second):
		t.Fatal("failed setup did not return")
	}
	select {
	case <-m.stops:
	case <-time.After(time.Second):
		t.Fatal("failed pending setup left a listener-free manager running")
	}
}

func TestFailedPendingSetupDoesNotStopReplacementSubscriber(t *testing.T) {
	m := &pendingFailureManager{entered: make(chan struct{}), release: make(chan struct{}), stops: make(chan struct{})}
	value, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
	require.NoError(t, err)
	_, oldUnsubscribe, err := value.Subscribe(silentCredentialsListener{})
	require.NoError(t, err)
	m.fail = true
	failed := make(chan error, 1)
	go func() { _, _, err := value.Subscribe(&distinctSilentListener{}); failed <- err }()
	select {
	case <-m.entered:
	case <-time.After(time.Second):
		t.Fatal("failed subscriber did not enter acquisition")
	}
	require.NoError(t, oldUnsubscribe())
	// Receiving entered orders the fake's first flag read before this change.
	m.fail = false
	_, replacement, err := value.Subscribe(&parentListener{id: 3})
	require.NoError(t, err)
	close(m.release)
	select {
	case err := <-failed:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("failed subscriber did not return")
	}
	select {
	case <-m.stops:
		t.Fatal("failed setup stopped the successful replacement")
	default:
	}
	require.NoError(t, replacement())
	select {
	case <-m.stops:
	case <-time.After(time.Second):
		t.Fatal("replacement unsubscribe did not stop manager")
	}
}
