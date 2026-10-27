package entraid

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProviderCloseRetiresRegistrationLeases(t *testing.T) {
	m := &regressionManager{token: regressionToken()}
	value, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
	require.NoError(t, err)
	p := value.(*entraidCredentialsProvider)
	listener := &distinctSilentListener{}
	_, first, err := p.SubscribeWithContext(context.Background(), listener)
	require.NoError(t, err)
	_, second, err := p.Subscribe(listener)
	require.NoError(t, err)
	registration := p.registrations[listener]
	require.NotNil(t, registration)
	require.NoError(t, p.Close())
	require.False(t, registration.active)
	require.Empty(t, p.registrations)
	require.Empty(t, p.listeners)
	require.NoError(t, first())
	require.NoError(t, second())
	_, unsubscribe, err := p.SubscribeWithContext(context.Background(), listener)
	require.Error(t, err)
	require.Nil(t, unsubscribe)
	m.mu.Lock()
	stops := m.stops
	m.mu.Unlock()
	require.Equal(t, 1, stops, "stale handles must not repeat owner shutdown")
}

func TestProviderContextSubscribeRejectedWhileOwnerStopRuns(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	m := &lifecycleTestManager{}
	m.stopHook = func() error {
		close(entered)
		<-release
		m.active.Add(-1)
		return nil
	}
	value, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
	require.NoError(t, err)
	p := value.(*entraidCredentialsProvider)
	done := make(chan error, 1)
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	go func() { done <- p.Close() }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("owner stop was not entered")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, unsubscribe, err := p.SubscribeWithContext(ctx, &distinctSilentListener{})
	require.ErrorContains(t, err, "closed")
	require.Nil(t, unsubscribe)
	require.NoError(t, ctx.Err(), "subscription rejection must not wait for external StopFunc")
	once.Do(func() { close(release) })
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("owner close did not finish")
	}
}
