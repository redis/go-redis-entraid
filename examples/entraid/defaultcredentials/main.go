package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"config"

	entraid "github.com/redis-developer/go-redis-entraid"
	"github.com/redis-developer/go-redis-entraid/identity"
	"github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()

	// Load configuration
	cfg, err := config.LoadConfig(os.Getenv("REDIS_ENDPOINTS_CONFIG_PATH"))
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create a default credentials provider
	// This will try different authentication methods in sequence:
	// 1. Environment variables (AZURE_CLIENT_ID, AZURE_CLIENT_SECRET, etc.)
	// 2. Managed Identity (system-assigned or user-assigned)
	// 3. Azure CLI credentials
	// 4. Visual Studio Code credentials
	cp, err := entraid.NewDefaultAzureCredentialsProvider(entraid.DefaultAzureCredentialsProviderOptions{
		DefaultAzureIdentityProviderOptions: identity.DefaultAzureIdentityProviderOptions{
			Scopes: []string{"https://redis.azure.com/.default"},
		},
	})
	if err != nil {
		log.Fatalf("Failed to create credentials provider: %v", err)
	}

	// Create Redis client with streaming credentials provider
	opts, err := redis.ParseURL(cfg.Endpoints["standalone-entraid-acl"].Endpoints[0])
	if err != nil {
		log.Fatalf("Failed to parse Redis URL: %v", err)
	}
	opts.StreamingCredentialsProvider = cp
	redisClient := redis.NewClient(opts)

	// Create second Redis client for cluster
	clusterOpts, err := redis.ParseURL(cfg.Endpoints["cluster-entraid-acl"].Endpoints[0])
	if err != nil {
		log.Fatalf("Failed to parse Redis URL: %v", err)
	}
	clusterClient := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:                        []string{clusterOpts.Addr},
		StreamingCredentialsProvider: cp,
	})

	pong, err := redisClient.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Failed to ping Redis: %v", err)
	}
	fmt.Printf("Successfully connected to Redis standalone: %s\n", pong)

	// Test cluster connection
	clusterPong, err := clusterClient.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Failed to ping Redis cluster: %v", err)
	}
	fmt.Printf("Successfully connected to Redis cluster: %s\n", clusterPong)

	// Set a test key
	err = redisClient.Set(ctx, "test-key", "test-value", 0).Err()
	if err != nil {
		log.Fatalf("Failed to set test key: %v", err)
	}

	// Get the test key
	val, err := redisClient.Get(ctx, "test-key").Result()
	if err != nil {
		log.Fatalf("Failed to get test key: %v", err)
	}
	fmt.Printf("Retrieved value from standalone: %s\n", val)

	// Set a test key in cluster
	err = clusterClient.Set(ctx, "test-key", "test-value", 0).Err()
	if err != nil {
		log.Fatalf("Failed to set test key in cluster: %v", err)
	}

	// Get the test key from cluster
	clusterVal, err := clusterClient.Get(ctx, "test-key").Result()
	if err != nil {
		log.Fatalf("Failed to get test key from cluster: %v", err)
	}
	fmt.Printf("Retrieved value from cluster: %s\n", clusterVal)
}
