package main

import (
	"context"
	"fmt"

	entraid "github.com/redis/go-redis-entraid"
	"github.com/redis/go-redis-entraid/identity"
	redis "github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()

	// Create a default azure identity credentials provider
	// This example uses the default Azure identity chain
	cp, err := entraid.NewDefaultAzureCredentialsProvider(entraid.DefaultAzureCredentialsProviderOptions{
		DefaultAzureIdentityProviderOptions: identity.DefaultAzureIdentityProviderOptions{
			Scopes: []string{"https://redis.azure.com/.default"},
		},
	})
	if err != nil {
		panic(fmt.Errorf("failed to create default azure identity credentials provider: %w", err))
	}

	// Create Redis client with the credentials provider
	redisClient := redis.NewClient(&redis.Options{
		Addr:                         "your-redis-host:6379",
		StreamingCredentialsProvider: cp,
	})

	// Test the connection
	ok, err := redisClient.Ping(ctx).Result()
	if err != nil {
		panic(fmt.Errorf("failed to ping Redis: %w", err))
	}
	fmt.Println("Ping result:", ok)
}
