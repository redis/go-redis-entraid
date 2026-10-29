package entraid

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis-entraid/token"
	"github.com/redis/go-redis/v9/auth"
	"github.com/stretchr/testify/require"
)

type synchronousCallbackManager struct {
	listener manager.TokenListener
	notify   bool
	onError  bool
	stops    atomic.Int32
}

func (m *synchronousCallbackManager) Start(l manager.TokenListener) (manager.StopFunc, error) {
	m.listener = l
	return func() error { m.stops.Add(1); return nil }, nil
}
func (m *synchronousCallbackManager) GetToken(bool) (*token.Token, error) {
	if m.notify {
		if m.onError {
			m.listener.OnError(errors.New("acquisition notification"))
		} else {
			m.listener.OnNext(regressionToken())
		}
	}
	return regressionToken(), nil
}

type selfRemovingListener struct{ unsubscribe auth.UnsubscribeFunc }

func (l *selfRemovingListener) OnNext(auth.Credentials) { _ = l.unsubscribe() }
func (l *selfRemovingListener) OnError(error)           { _ = l.unsubscribe() }

func TestSubscribeAllowsSynchronousCallbackUnsubscribe(t *testing.T) {
	for _, onError := range []bool{false, true} {
		name := "OnNext"
		if onError {
			name = "OnError"
		}
		t.Run(name, func(t *testing.T) {
			m := &synchronousCallbackManager{onError: onError}
			p, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
			require.NoError(t, err)
			listener := &selfRemovingListener{}
			_, listener.unsubscribe, err = p.Subscribe(listener)
			require.NoError(t, err)
			m.notify = true
			type result struct {
				unsubscribe auth.UnsubscribeFunc
				err         error
			}
			done := make(chan result, 1)
			go func() { _, u, err := p.Subscribe(&distinctSilentListener{}); done <- result{u, err} }()
			select {
			case r := <-done:
				require.NoError(t, r.err)
				require.Zero(t, m.stops.Load(), "pending subscription must protect renewal")
				require.NoError(t, r.unsubscribe())
				require.EqualValues(t, 1, m.stops.Load())
			case <-time.After(time.Second):
				t.Fatal("Subscribe deadlocked with synchronous callback unsubscribe")
			}
		})
	}
}
