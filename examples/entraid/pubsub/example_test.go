package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
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

// clientName gives each test's connections a unique name, so CLIENT LIST
// can tell them apart from every other connection on the server.
func clientName(t *testing.T) string {
	r := strings.NewReplacer("/", "-", " ", "-")
	return fmt.Sprintf("entraid-ps-%s-%d", r.Replace(t.Name()), time.Now().UnixNano())
}

func standaloneOptions(t *testing.T, cfg *config.EntraidConfig, cp auth.StreamingCredentialsProvider, protocol int) *redis.Options {
	t.Helper()
	opts, err := redis.ParseURL(cfg.Endpoints["standalone-entraid-acl"].Endpoints[0])
	if err != nil {
		t.Fatalf("parse standalone URL: %v", err)
	}
	opts.StreamingCredentialsProvider = cp
	opts.Protocol = protocol
	opts.ClientName = clientName(t)
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
		ClientName:                   clientName(t),
	}
}

// connTracker polls CLIENT LIST for the subscribed connections named
// name and records every connection ID it sees. On RESP3 a refresh is
// applied in place, so the IDs never change. On RESP2 every refresh
// replaces the subscribed connection, so new IDs appear: the number of
// IDs beyond the first poll counts the rotations. A rotation does not
// show up as subscribe events, so the events cannot be used for this.
type connTracker struct {
	name string
	list func(ctx context.Context) ([]string, error)

	mu      sync.Mutex
	initial int
	ids     map[string]struct{}
	err     error
	stop    chan struct{}
	done    chan struct{}
}

func trackStandalone(client *redis.Client, name string) *connTracker {
	return newConnTracker(name, func(ctx context.Context) ([]string, error) {
		s, err := client.ClientList(ctx).Result()
		return []string{s}, err
	})
}

func trackCluster(client *redis.ClusterClient, name string) *connTracker {
	return newConnTracker(name, func(ctx context.Context) ([]string, error) {
		var mu sync.Mutex
		var out []string
		err := client.ForEachShard(ctx, func(ctx context.Context, node *redis.Client) error {
			s, err := node.ClientList(ctx).Result()
			if err != nil {
				return err
			}
			mu.Lock()
			out = append(out, s)
			mu.Unlock()
			return nil
		})
		return out, err
	})
}

func newConnTracker(name string, list func(ctx context.Context) ([]string, error)) *connTracker {
	return &connTracker{
		name: name,
		list: list,
		ids:  make(map[string]struct{}),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

// subscribedIDs returns the IDs of the subscribed connections named
// c.name in the given CLIENT LIST outputs.
func (c *connTracker) subscribedIDs(lists []string) []string {
	var ids []string
	for _, list := range lists {
		for _, line := range strings.Split(list, "\n") {
			var id, name string
			subs := 0
			for _, f := range strings.Fields(line) {
				k, v, ok := strings.Cut(f, "=")
				if !ok {
					continue
				}
				switch k {
				case "id":
					id = v
				case "name":
					name = v
				case "sub", "psub", "ssub":
					n, _ := strconv.Atoi(v)
					subs += n
				}
			}
			if name == c.name && subs > 0 {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// start records the subscribed connections present now, then keeps
// polling until finish.
func (c *connTracker) start(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	lists, err := c.list(ctx)
	if err != nil {
		c.err = err
		close(c.done)
		t.Logf("CLIENT LIST is not available (%v); rotations are not counted", err)
		return
	}
	for _, id := range c.subscribedIDs(lists) {
		c.ids[id] = struct{}{}
	}
	c.initial = len(c.ids)
	go func() {
		defer close(c.done)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-c.stop:
				return
			case <-tick.C:
			}
			lists, err := c.list(ctx)
			c.mu.Lock()
			if err != nil {
				c.err = err
				c.mu.Unlock()
				return
			}
			for _, id := range c.subscribedIDs(lists) {
				c.ids[id] = struct{}{}
			}
			c.mu.Unlock()
		}
	}()
}

// finish stops polling and returns the connections seen at the start and
// the rotations seen since; ok is false when CLIENT LIST failed.
func (c *connTracker) finish() (initial, rotations int, ok bool) {
	select {
	case <-c.done:
	default:
		close(c.stop)
		<-c.done
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil || c.initial == 0 {
		return 0, 0, false
	}
	return c.initial, len(c.ids) - c.initial, true
}

// streamResult is what a subscriber saw while messages were published.
type streamResult struct {
	published  int
	unique     int
	duplicates int
}

// runStream waits for the initial confirmations, starts tr, publishes
// messages on every channel through publish (one round every 200ms for
// runFor), and collects what arrives until every published message is in
// or a grace period passes. A message that never arrives fails the test.
func runStream(t *testing.T, ps *redis.PubSub, channels []string, tr *connTracker, publish func(ctx context.Context, channel, msg string) error) streamResult {
	t.Helper()
	ctx := context.Background()

	events := ps.ChannelWithSubscriptions()
	confirms := 0
	deadline := time.After(10 * time.Second)
	for confirms < len(channels) {
		select {
		case ev := <-events:
			if _, ok := ev.(*redis.Subscription); ok {
				confirms++
			}
		case <-deadline:
			t.Fatalf("got %d of %d subscribe confirmations", confirms, len(channels))
		}
	}
	tr.start(t)

	var mu sync.Mutex
	sent := map[string]bool{}
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
				sent[msg] = true
				mu.Unlock()
			}
			time.Sleep(200 * time.Millisecond)
		}
	}()

	seen := map[string]int{}
	var res streamResult
	grace := time.After(runFor + 10*time.Second)
	published := false
	doneCh := done
	for {
		mu.Lock()
		want := len(sent)
		mu.Unlock()
		if published && len(seen) >= want {
			res.published, res.unique = want, len(seen)
			return res
		}
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("subscription channel closed")
			}
			if m, ok := ev.(*redis.Message); ok {
				seen[m.Payload]++
				if seen[m.Payload] > 1 {
					res.duplicates++
				}
			}
		case <-doneCh:
			published, doneCh = true, nil
		case <-grace:
			t.Fatalf("received %d of %d messages", len(seen), want)
		}
	}
}

func requireRefreshes(t *testing.T, cp *countingProvider, before int64) {
	t.Helper()
	if got := cp.pushed.Load() - before; got < refreshes {
		t.Fatalf("token refreshes pushed during the run = %d, want >= %d", got, refreshes)
	}
}

// checkRotations checks the connection IDs tr saw and the duplicates. On
// RESP3 the subscribed connections must never change. On RESP2 they must
// change, and each rotation may deliver at most one message twice: the
// old connection's UNSUBSCRIBE and the new one's SUBSCRIBE go out on two
// sockets, so the server can apply the SUBSCRIBE first.
func checkRotations(t *testing.T, tr *connTracker, res streamResult, protocol int) {
	t.Helper()
	initial, rotations, ok := tr.finish()
	t.Logf("subscribed connections=%d rotations=%d published=%d duplicates=%d",
		initial, rotations, res.published, res.duplicates)
	if protocol == 3 {
		if ok && rotations != 0 {
			t.Fatalf("RESP3 subscription changed connection %d times, want 0 (re-auth must be in place)", rotations)
		}
		if res.duplicates != 0 {
			t.Fatalf("RESP3 delivered %d messages twice, want 0", res.duplicates)
		}
		return
	}
	if !ok {
		return
	}
	if rotations == 0 {
		t.Fatal("RESP2 subscription never changed connection; refreshes were not applied")
	}
	if res.duplicates > rotations {
		t.Fatalf("RESP2 delivered %d messages twice, want at most %d (one per rotation)", res.duplicates, rotations)
	}
}

// RESP3: every refresh is applied in place (AUTH on the subscribed
// connection), so the subscription keeps its connection and loses nothing.
func TestPubSubStandaloneRESP3(t *testing.T) {
	cfg := loadConfig(t)
	cp := newProvider(t, cfg)
	opts := standaloneOptions(t, cfg, cp, 3)
	client := redis.NewClient(opts)
	defer client.Close()

	ps := client.Subscribe(context.Background(), "entraid-ps-resp3")
	defer ps.Close()
	tr := trackStandalone(client, opts.ClientName)
	before := cp.pushed.Load()
	res := runStream(t, ps, []string{"entraid-ps-resp3"}, tr, func(ctx context.Context, ch, msg string) error {
		return client.Publish(ctx, ch, msg).Err()
	})
	requireRefreshes(t, cp, before)
	checkRotations(t, tr, res, 3)
}

// RESP2: a subscribed connection cannot run AUTH, so each refresh
// replaces it; the subscription must still receive every message.
func TestPubSubStandaloneRESP2(t *testing.T) {
	cfg := loadConfig(t)
	cp := newProvider(t, cfg)
	opts := standaloneOptions(t, cfg, cp, 2)
	client := redis.NewClient(opts)
	defer client.Close()

	ps := client.Subscribe(context.Background(), "entraid-ps-resp2")
	defer ps.Close()
	tr := trackStandalone(client, opts.ClientName)
	before := cp.pushed.Load()
	res := runStream(t, ps, []string{"entraid-ps-resp2"}, tr, func(ctx context.Context, ch, msg string) error {
		return client.Publish(ctx, ch, msg).Err()
	})
	requireRefreshes(t, cp, before)
	checkRotations(t, tr, res, 2)
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
			opts := clusterOptions(t, cfg, cp, protocol)
			client := redis.NewClusterClient(opts)
			defer client.Close()
			ctx := context.Background()
			if err := client.Ping(ctx).Err(); err != nil {
				t.Fatalf("ping: %v", err)
			}

			t.Run("Subscribe", func(t *testing.T) {
				ch := fmt.Sprintf("entraid-ps-cluster-%d", protocol)
				ps := client.Subscribe(ctx, ch)
				defer ps.Close()
				tr := trackCluster(client, opts.ClientName)
				before := cp.pushed.Load()
				res := runStream(t, ps, []string{ch}, tr, func(ctx context.Context, ch, msg string) error {
					return client.Publish(ctx, ch, msg).Err()
				})
				requireRefreshes(t, cp, before)
				checkRotations(t, tr, res, protocol)
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
				tr := trackCluster(client, opts.ClientName)
				before := cp.pushed.Load()
				res := runStream(t, ps, channels, tr, func(ctx context.Context, ch, msg string) error {
					return client.SPublish(ctx, ch, msg).Err()
				})
				requireRefreshes(t, cp, before)
				checkRotations(t, tr, res, protocol)
			})
		})
	}
}
