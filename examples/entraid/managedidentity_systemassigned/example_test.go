package main

import (
	"context"
	"os"
	"testing"
	"time"

	"config"

	entraid "github.com/redis/go-redis-entraid"
	"github.com/redis/go-redis-entraid/identity"
	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis/v9"
)

func TestManagedIdentitySystemAssigned(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	cfg, err := config.LoadConfig(os.Getenv("REDIS_ENDPOINTS_CONFIG_PATH"))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Create a managed identity credentials provider for system-assigned identity
	cp, err := entraid.NewManagedIdentityCredentialsProvider(entraid.ManagedIdentityCredentialsProviderOptions{
		CredentialsProviderOptions: entraid.CredentialsProviderOptions{
			TokenManagerOptions: manager.TokenManagerOptions{
				ExpirationRefreshRatio: 0.001,           // Set to refresh very early
				LowerRefreshBound:      time.Second * 1, // Set lower bound to 1 second
			},
		},
		ManagedIdentityProviderOptions: identity.ManagedIdentityProviderOptions{
			Scopes:              cfg.GetRedisScopes(),
			ManagedIdentityType: identity.SystemAssignedIdentity,
		},
	})
	if err != nil {
		t.Fatalf("Failed to create credentials provider: %v", err)
	}

	// Create Redis client with streaming credentials provider
	opts, err := redis.ParseURL(cfg.Endpoints["standalone-entraid-acl"].Endpoints[0])
	if err != nil {
		t.Fatalf("Failed to parse Redis URL: %v", err)
	}
	opts.StreamingCredentialsProvider = cp
	redisClient := redis.NewClient(opts)

	// Create second Redis client for cluster
	clusterOpts, err := redis.ParseURL(cfg.Endpoints["cluster-entraid-acl"].Endpoints[0])
	if err != nil {
		t.Fatalf("Failed to parse Redis URL: %v", err)
	}
	clusterClient := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:                        []string{clusterOpts.Addr},
		StreamingCredentialsProvider: cp,
	})

	// Test the connection
	pong, err := redisClient.Ping(ctx).Result()
	if err != nil {
		t.Fatalf("Failed to ping Redis: %v", err)
	}
	if pong != "PONG" {
		t.Errorf("Expected PONG, got %s", pong)
	}
	t.Logf("Successfully connected to Redis standalone: %s", pong)

	// Test cluster connection
	clusterPong, err := clusterClient.Ping(ctx).Result()
	if err != nil {
		t.Fatalf("Failed to ping Redis cluster: %v", err)
	}
	if clusterPong != "PONG" {
		t.Errorf("Expected PONG, got %s", clusterPong)
	}
	t.Logf("Successfully connected to Redis cluster: %s", clusterPong)

	// Set a test key
	err = redisClient.Set(ctx, "test-key", "test-value", 0).Err()
	if err != nil {
		t.Fatalf("Failed to set test key: %v", err)
	}

	// Get the test key
	val, err := redisClient.Get(ctx, "test-key").Result()
	if err != nil {
		t.Fatalf("Failed to get test key: %v", err)
	}
	if val != "test-value" {
		t.Errorf("Expected test-value, got %s", val)
	}
	t.Logf("Retrieved value from standalone: %s", val)

	// Set a test key in cluster
	err = clusterClient.Set(ctx, "test-key", "test-value", 0).Err()
	if err != nil {
		t.Fatalf("Failed to set test key in cluster: %v", err)
	}

	// Get the test key from cluster
	clusterVal, err := clusterClient.Get(ctx, "test-key").Result()
	if err != nil {
		t.Fatalf("Failed to get test key from cluster: %v", err)
	}
	if clusterVal != "test-value" {
		t.Errorf("Expected test-value, got %s", clusterVal)
	}
	t.Logf("Retrieved value from cluster: %s", clusterVal)

	// Wait for token to expire
	t.Log("Waiting for token to expire...")
	time.Sleep(3 * time.Second)

	// Test token refresh by retrying operations
	t.Log("Testing token refresh...")

	// Retry standalone operations
	var pingSuccess bool
	for i := 0; i < 3; i++ {
		pong, err = redisClient.Ping(ctx).Result()
		if err != nil {
			t.Logf("Failed to ping Redis (attempt %d): %v", i+1, err)
			continue
		}
		t.Logf("Successfully pinged Redis standalone after token refresh: %s", pong)
		pingSuccess = true
		break
	}
	if !pingSuccess {
		t.Error("Failed to ping Redis standalone after token refresh")
	}

	// Retry cluster operations
	var clusterPingSuccess bool
	for i := 0; i < 3; i++ {
		clusterPong, err = clusterClient.Ping(ctx).Result()
		if err != nil {
			t.Logf("Failed to ping Redis cluster (attempt %d): %v", i+1, err)
			continue
		}
		t.Logf("Successfully pinged Redis cluster after token refresh: %s", clusterPong)
		clusterPingSuccess = true
		break
	}
	if !clusterPingSuccess {
		t.Error("Failed to ping Redis cluster after token refresh")
	}
}

