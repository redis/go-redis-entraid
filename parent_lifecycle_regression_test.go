package entraid

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis-entraid/token"
	"github.com/redis/go-redis/v9/auth"
)

type parentLifecycleManager struct {
	running           atomic.Bool
	gate              atomic.Bool
	entered, release  chan struct{}
	startErr, stopErr error
}

func (m *parentLifecycleManager) Start(manager.TokenListener) (manager.StopFunc, error) {
	if m.startErr != nil {
		return nil, m.startErr
	}
	m.running.Store(true)
	return func() error { m.running.Store(false); return m.stopErr }, nil
}
func (m *parentLifecycleManager) GetToken(bool) (*token.Token, error) {
	if m.gate.Load() {
		close(m.entered)
		<-m.release
	}
	now := time.Now()
	return token.New("u", "p", "p", now.Add(time.Hour), now, time.Hour.Milliseconds()), nil
}

type parentListener struct{ id int }

func (*parentListener) OnNext(auth.Credentials) {}
func (*parentListener) OnError(error)           {}

func TestParentRegistrationCannotRaceLastStop(t *testing.T) {
	m := &parentLifecycleManager{entered: make(chan struct{}), release: make(chan struct{})}
	p, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, oldUnsub, err := p.Subscribe(&parentListener{1})
	if err != nil {
		t.Fatal(err)
	}
	m.gate.Store(true)
	type result struct {
		unsubscribe auth.UnsubscribeFunc
		err         error
	}
	done := make(chan result, 1)
	go func() { _, unsub, err := p.Subscribe(&parentListener{2}); done <- result{unsub, err} }()
	select {
	case <-m.entered:
	case <-time.After(time.Second):
		t.Fatal("new subscription did not enter acquisition")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- oldUnsub() }()
	var stopErr error
	completed := false
	select {
	case stopErr = <-stopped:
		completed = true
	case <-time.After(100 * time.Millisecond):
	}
	close(m.release)
	var r result
	select {
	case r = <-done:
	case <-time.After(time.Second):
		t.Fatal("new subscription did not complete")
	}
	if !completed {
		select {
		case stopErr = <-stopped:
		case <-time.After(time.Second):
			t.Fatal("old unsubscribe did not complete")
		}
	}
	if r.err != nil || stopErr != nil {
		t.Fatalf("subscribe=%v unsubscribe=%v", r.err, stopErr)
	}
	defer r.unsubscribe()
	if !m.running.Load() {
		t.Fatal("subscription succeeded after last unsubscribe stopped token renewal")
	}
}
