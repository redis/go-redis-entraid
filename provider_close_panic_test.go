package entraid

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloseContainsStopperPanicAndAllowsRetry(t *testing.T) {
	var calls atomic.Int32
	m := &lifecycleTestManager{stopHook: func() error {
		if calls.Add(1) == 1 {
			panic("custom stopper panic")
		}
		return nil
	}}
	value, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
	require.NoError(t, err)
	p := value.(CredentialsProviderCloser)
	type result struct {
		err      error
		panicked any
	}
	invoke := func(done chan<- result) {
		r := result{}
		defer func() { r.panicked = recover(); done <- r }()
		r.err = p.Close()
	}
	first := make(chan result, 1)
	go invoke(first)
	select {
	case r := <-first:
		require.Nil(t, r.panicked, "custom stopper panic must become a cleanup error")
		require.ErrorContains(t, r.err, "custom stopper panic")
	case <-time.After(time.Second):
		t.Fatal("first Close did not finish")
	}
	retry := make(chan result, 1)
	go invoke(retry)
	select {
	case r := <-retry:
		require.Nil(t, r.panicked)
		require.NoError(t, r.err)
	case <-time.After(time.Second):
		t.Fatal("stopper panic stranded the close attempt")
	}
	require.EqualValues(t, 2, calls.Load())
	require.NoError(t, p.Close())
	require.EqualValues(t, 2, calls.Load(), "successful shutdown must be idempotent")
}
