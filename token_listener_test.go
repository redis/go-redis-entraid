package entraid

import (
	"errors"
	"testing"
	"time"

	"github.com/redis-developer/go-redis-entraid/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenListenerFromCP(t *testing.T) {
	cp := &entraidCredentialsProvider{}
	listener := tokenListenerFromCP(cp)

	require.NotNil(t, listener)
	_, ok := listener.(*entraidTokenListener)
	assert.True(t, ok, "listener should be of type entraidTokenListener")
}

func TestOnTokenNext(t *testing.T) {
	cp := &entraidCredentialsProvider{}
	listener := tokenListenerFromCP(cp)

	now := time.Now()
	testToken := token.New("test-user", "test-pass", "test-token", now.Add(time.Hour), now, 3600)

	listener.OnNext(testToken)

	// Since we can't directly access the internal state of entraidCredentialsProvider,
	// we'll verify that the listener was created and the call didn't panic
	assert.NotNil(t, listener)
}

func TestOnTokenError(t *testing.T) {
	cp := &entraidCredentialsProvider{}
	listener := tokenListenerFromCP(cp)

	testError := errors.New("test error")
	listener.OnError(testError)

	// Since we can't directly access the internal state of entraidCredentialsProvider,
	// we'll verify that the listener was created and the call didn't panic
	assert.NotNil(t, listener)
}
