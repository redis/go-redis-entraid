package entraid

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscribeRepairsFailedStopBeforeRestart(t *testing.T) {
	m := &regressionManager{token: regressionToken(), stopErr: errors.New("incomplete stop")}
	p := &entraidCredentialsProvider{tokenManager: m}
	_, unsubscribe, err := p.Subscribe(silentCredentialsListener{})
	require.NoError(t, err)
	require.ErrorContains(t, unsubscribe(), "incomplete stop")
	m.mu.Lock()
	m.stopErr = nil
	m.mu.Unlock()
	_, next, err := p.Subscribe(&distinctSilentListener{})
	require.NoError(t, err)
	defer next()
	m.mu.Lock()
	starts, stops := m.starts, m.stops
	m.mu.Unlock()
	require.Equal(t, 2, stops, "failed stop must be repaired before assuming manager state")
	require.Equal(t, 2, starts, "successful cleanup must be followed by a fresh manager start")
}
