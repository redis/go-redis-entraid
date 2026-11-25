package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"config"

	entraid "github.com/redis/go-redis-entraid"
	"github.com/redis/go-redis-entraid/identity"
	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/auth"
)

// The token is refreshed about every second (see newProvider); every
// test keeps its subscription open across several refreshes.
const (
	refreshes = 5
	runFor    = 8 * time.Second
)

// countingProvider counts the credentials pushed to listeners, so a test
// can check that refreshes actually happened while it ran.
type countingProvider struct {
	auth.StreamingCredentialsProvider
	pushed atomic.Int64
}

func (p *countingProvider) Subscribe(l auth.CredentialsListener) (auth.Credentials, auth.UnsubscribeFunc, error) {
	return p.StreamingCredentialsProvider.Subscribe(&countingListener{CredentialsListener: l, p: p})
}

type countingListener struct {
	auth.CredentialsListener
	p *countingProvider
}

func (l *countingListener) OnNext(c auth.Credentials) {
	l.p.pushed.Add(1)
	l.CredentialsListener.OnNext(c)
}

func newProvider(t *testing.T, cfg *config.EntraidConfig) *countingProvider {
	t.Helper()
	cp, err := entraid.NewConfidentialCredentialsProvider(entraid.ConfidentialCredentialsProviderOptions{
		CredentialsProviderOptions: entraid.CredentialsProviderOptions{
			TokenManagerOptions: manager.TokenManagerOptions{
				ExpirationRefreshRatio: 0.001,       // refresh very early
				LowerRefreshBound:      time.Second, // about once per second
			},
		},
		ConfidentialIdentityProviderOptions: identity.ConfidentialIdentityProviderOptions{
			ClientID:        cfg.AzureClientID,
			ClientSecret:    cfg.AzureClientSecret,
			CredentialsType: identity.ClientSecretCredentialType,
			Authority: identity.AuthorityConfiguration{
				AuthorityType: identity.AuthorityTypeMultiTenant,
				TenantID:      cfg.AzureTenantID,
			},
			Scopes: cfg.GetRedisScopes(),
		},
	})
	if err != nil {
		t.Fatalf("credentials provider: %v", err)
	}
	return &countingProvider{StreamingCredentialsProvider: cp}
}

func loadConfig(t *testing.T) *config.EntraidConfig {
	t.Helper()
	cfg, err := config.LoadConfig(os.Getenv("REDIS_ENDPOINTS_CONFIG_PATH"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func standaloneOptions(t *testing.T, cfg *config.EntraidConfig, cp auth.StreamingCredentialsProvider, protocol int) *redis.Options {
	t.Helper()
	opts, err := redis.ParseURL(cfg.Endpoints["standalone-entraid-acl"].Endpoints[0])
	if err != nil {
		t.Fatalf("parse standalone URL: %v", err)
	}
	opts.StreamingCredentialsProvider = cp
	opts.Protocol = protocol
	return opts
}

func clusterOptions(t *testing.T, cfg *config.EntraidConfig, cp auth.StreamingCredentialsProvider, protocol int) *redis.ClusterOptions {
	t.Helper()
	opts, err := redis.ParseURL(cfg.Endpoints["cluster-entraid-acl"].Endpoints[0])
	if err != nil {
		t.Fatalf("parse cluster URL: %v", err)
	}
	return &redis.ClusterOptions{
		Addrs:                        []string{opts.Addr},
		TLSConfig:                    opts.TLSConfig,
		StreamingCredentialsProvider: cp,
		Protocol:                     protocol,
	}
}

// streamResult is what a subscriber saw while messages were published.
type streamResult struct {
	received      []string
	resubscribes  int // confirmations after the initial ones (reconnects)
	firstConfirms int
}

// runStream subscribes through sub, publishes count messages per
// channel through publish (one every period), and collects what arrives
// until every message is in or runFor plus a grace period passes.
func runStream(t *testing.T, ps *redis.PubSub, channels []string, publish func(ctx context.Context, channel, msg string) error) streamResult {
	t.Helper()
	ctx := context.Background()

	events := ps.ChannelWithSubscriptions()
	var res streamResult
	// Wait for the initial confirmations.
	deadline := time.After(10 * time.Second)
	for res.firstConfirms < len(channels) {
		select {
		case ev := <-events:
			if _, ok := ev.(*redis.Subscription); ok {
				res.firstConfirms++
			}
		case <-deadline:
			t.Fatalf("got %d of %d subscribe confirmations", res.firstConfirms, len(channels))
		}
	}

	total := 0
	var mu sync.Mutex
	stop := time.Now().Add(runFor)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; time.Now().Before(stop); i++ {
			for _, ch := range channels {
				msg := fmt.Sprintf("%s-%d", ch, i)
				if err := publish(ctx, ch, msg); err != nil {
					t.Errorf("publish %s: %v", msg, err)
					return
				}
				mu.Lock()
				total++
				mu.Unlock()
			}
			time.Sleep(200 * time.Millisecond)
		}
	}()

	grace := time.After(runFor + 10*time.Second)
	published := false
	doneCh := done
	for {
		mu.Lock()
		want := total
		mu.Unlock()
		if published && len(res.received) >= want {
			if len(res.received) > want {
				t.Fatalf("received %d messages, published %d (duplicates)", len(res.received), want)
			}
			return res
		}
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("subscription channel closed")
			}
			switch v := ev.(type) {
			case *redis.Message:
				res.received = append(res.received, v.Payload)
			case *redis.Subscription:
				res.resubscribes++
			}
		case <-doneCh:
			published, doneCh = true, nil
		case <-grace:
			t.Fatalf("received %d of %d messages", len(res.received), want)
		}
	}
}

func requireRefreshes(t *testing.T, cp *countingProvider, before int64) {
	t.Helper()
	if got := cp.pushed.Load() - before; got < refreshes {
		t.Fatalf("token refreshes pushed during the run = %d, want >= %d", got, refreshes)
	}
}

// RESP3: every refresh is applied in place (AUTH on the subscribed
// connection), so the subscription never reconnects and loses nothing.
func TestPubSubStandaloneRESP3(t *testing.T) {
	cfg := loadConfig(t)
	cp := newProvider(t, cfg)
	client := redis.NewClient(standaloneOptions(t, cfg, cp, 3))
	defer client.Close()

	ps := client.Subscribe(context.Background(), "entraid-ps-resp3")
	defer ps.Close()
	before := cp.pushed.Load()
	res := runStream(t, ps, []string{"entraid-ps-resp3"}, func(ctx context.Context, ch, msg string) error {
		return client.Publish(ctx, ch, msg).Err()
	})
	requireRefreshes(t, cp, before)
	if res.resubscribes != 0 {
		t.Fatalf("RESP3 subscription resubscribed %d times, want 0 (re-auth must be in place)", res.resubscribes)
	}
}

// RESP2: a subscribed connection cannot run AUTH, so each refresh
// replaces it; the subscription must still receive every message
// published after each replacement settled.
func TestPubSubStandaloneRESP2(t *testing.T) {
	cfg := loadConfig(t)
	cp := newProvider(t, cfg)
	client := redis.NewClient(standaloneOptions(t, cfg, cp, 2))
	defer client.Close()

	ps := client.Subscribe(context.Background(), "entraid-ps-resp2")
	defer ps.Close()
	before := cp.pushed.Load()
	res := runStream(t, ps, []string{"entraid-ps-resp2"}, func(ctx context.Context, ch, msg string) error {
		return client.Publish(ctx, ch, msg).Err()
	})
	requireRefreshes(t, cp, before)
	if res.resubscribes == 0 {
		t.Fatal("RESP2 subscription never resubscribed; refreshes were not applied")
	}
}

// Idle: no messages across several refreshes; the subscription must
// still work afterwards.
func TestPubSubStandaloneIdle(t *testing.T) {
	cfg := loadConfig(t)
	cp := newProvider(t, cfg)
	client := redis.NewClient(standaloneOptions(t, cfg, cp, 3))
	defer client.Close()

	ctx := context.Background()
	ps := client.Subscribe(ctx, "entraid-ps-idle")
	defer ps.Close()
	if _, err := ps.ReceiveTimeout(ctx, 10*time.Second); err != nil {
		t.Fatalf("subscribe confirmation: %v", err)
	}
	before := cp.pushed.Load()
	time.Sleep(runFor)
	requireRefreshes(t, cp, before)

	if err := client.Publish(ctx, "entraid-ps-idle", "after-idle").Err(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	msg, err := ps.ReceiveMessage(ctx)
	if err != nil || msg.Payload != "after-idle" {
		t.Fatalf("after idle refreshes: msg %v, err %v", msg, err)
	}
}

// Cluster SUBSCRIBE (broadcast connection) and SSUBSCRIBE on channels in
// several slots (per-node connections), both across refreshes.
func TestPubSubCluster(t *testing.T) {
	cfg := loadConfig(t)
	for _, protocol := range []int{3, 2} {
		t.Run(fmt.Sprintf("RESP%d", protocol), func(t *testing.T) {
			cp := newProvider(t, cfg)
			client := redis.NewClusterClient(clusterOptions(t, cfg, cp, protocol))
			defer client.Close()
			ctx := context.Background()
			if err := client.Ping(ctx).Err(); err != nil {
				t.Fatalf("ping: %v", err)
			}

			t.Run("Subscribe", func(t *testing.T) {
				ch := fmt.Sprintf("entraid-ps-cluster-%d", protocol)
				ps := client.Subscribe(ctx, ch)
				defer ps.Close()
				before := cp.pushed.Load()
				runStream(t, ps, []string{ch}, func(ctx context.Context, ch, msg string) error {
					return client.Publish(ctx, ch, msg).Err()
				})
				requireRefreshes(t, cp, before)
			})

			t.Run("SSubscribe", func(t *testing.T) {
				// Different hash tags spread the channels over slots (and,
				// on a multi-shard cluster, over nodes).
				var channels []string
				for _, tag := range []string{"a", "b", "c", "d", "e", "f"} {
					channels = append(channels, fmt.Sprintf("{%s}entraid-ps-sharded-%d", tag, protocol))
				}
				ps := client.SSubscribe(ctx, channels...)
				defer ps.Close()
				before := cp.pushed.Load()
				runStream(t, ps, channels, func(ctx context.Context, ch, msg string) error {
					return client.SPublish(ctx, ch, msg).Err()
				})
				requireRefreshes(t, cp, before)
			})
		})
	}
}
