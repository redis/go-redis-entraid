package entraid

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// This is a local RESP fixture, not an Azure/Redis deployment. The real Redis
// clients must finish initialization and own their subscriptions themselves.
func subscribedPipeClient(t *testing.T, p *entraidCredentialsProvider) (*redis.Client, *atomic.Int32) {
	t.Helper()
	clientEnd, serverEnd := net.Pipe()
	done := make(chan struct{})
	auths := &atomic.Int32{}
	go func() {
		defer close(done)
		defer serverEnd.Close()
		reader := bufio.NewReader(serverEnd)
		for {
			args, err := readPipeCommand(reader)
			if err != nil {
				return
			}
			var reply string
			switch strings.ToLower(args[0]) {
			case "hello":
				reply = "*0\r\n"
			case "ping":
				reply = "+PONG\r\n"
			case "auth":
				auths.Add(1)
				reply = "+OK\r\n"
			default:
				reply = "-ERR unsupported fixture command\r\n"
			}
			if _, err = io.WriteString(serverEnd, reply); err != nil {
				return
			}
		}
	}()
	var dialed atomic.Bool
	client := redis.NewClient(&redis.Options{
		Protocol: 2, DisableIdentity: true, StreamingCredentialsProvider: p,
		PoolSize: 1, MaxRetries: -1, ReadTimeout: time.Second, WriteTimeout: time.Second,
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			if dialed.Swap(true) {
				return nil, fmt.Errorf("fixture connection was redialed")
			}
			return clientEnd, nil
		},
	})
	t.Cleanup(func() {
		_ = client.Close()
		_ = clientEnd.Close()
		_ = serverEnd.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("RESP fixture did not stop")
		}
	})
	return client, auths
}

func readPipeCommand(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("expected RESP array")
	}
	count, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil || count < 1 || count > 32 {
		return nil, fmt.Errorf("invalid command length")
	}
	args := make([]string, count)
	for i := range args {
		line, err = reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(line, "$") {
			return nil, fmt.Errorf("expected RESP bulk string")
		}
		size, err := strconv.Atoi(strings.TrimSpace(line[1:]))
		if err != nil || size < 0 || size > 4096 {
			return nil, fmt.Errorf("invalid argument length")
		}
		value := make([]byte, size+2)
		if _, err = io.ReadFull(reader, value); err != nil {
			return nil, err
		}
		if string(value[size:]) != "\r\n" {
			return nil, fmt.Errorf("invalid argument terminator")
		}
		args[i] = string(value[:size])
	}
	return args, nil
}

func TestClosingOneSubscribedClientKeepsOtherClientActive(t *testing.T) {
	m := &lifecycleTestManager{}
	value, err := NewCredentialsProvider(m, CredentialsProviderOptions{})
	require.NoError(t, err)
	p := value.(*entraidCredentialsProvider)
	t.Cleanup(func() { _ = p.Close() })
	first, _ := subscribedPipeClient(t, p)
	second, secondAuths := subscribedPipeClient(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, first.Ping(ctx).Err())
	require.NoError(t, second.Ping(ctx).Err())
	p.rwLock.RLock()
	registered := len(p.listeners)
	p.rwLock.RUnlock()
	require.Equal(t, 2, registered, "clients must actually subscribe")
	require.NoError(t, first.Close())
	require.Zero(t, m.stops.Load(), "closing one connection must not stop the shared provider")
	p.rwLock.RLock()
	registered = len(p.listeners)
	p.rwLock.RUnlock()
	require.Equal(t, 1, registered)
	p.onTokenNext(regressionToken())
	require.EqualValues(t, 1, secondAuths.Load(), "remaining client must receive and use the credential update")
	require.NoError(t, second.Ping(ctx).Err())
	require.NoError(t, p.Close())
	require.EqualValues(t, 1, m.stops.Load())
}
