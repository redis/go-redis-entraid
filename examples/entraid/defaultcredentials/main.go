package main

import (
	"context"
	"fmt"
	"log"

	"config"

	entraid "github.com/redis-developer/go-redis-entraid"
	"github.com/redis-developer/go-redis-entraid/identity"
	"github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()

	// Load configuration
	cfg, err := config.LoadConfig("")
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
	redisClient := redis.NewClusterClient(&redis.ClusterOptions{
		DisableIdentity:              true,
		Addrs:                        []string{cfg.Endpoints["standalone-entraid-acl"].Endpoints[0]},
		StreamingCredentialsProvider: cp,
	})

	// Test the connection
	pong, err := redisClient.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Failed to ping Redis: %v", err)
	}
	fmt.Printf("Successfully connected to Redis: %s\n", pong)

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
	fmt.Printf("Retrieved value: %s\n", val)
}
