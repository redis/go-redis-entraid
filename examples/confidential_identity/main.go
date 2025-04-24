package main

import (
	"context"
	"fmt"

	entraid "github.com/redis-developer/go-redis-entraid"
	"github.com/redis-developer/go-redis-entraid/identity"
	redis "github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()

	// Create a confidential identity credentials provider
	// This example uses client secret authentication
	cp, err := entraid.NewConfidentialCredentialsProvider(entraid.ConfidentialCredentialsProviderOptions{
		ConfidentialIdentityProviderOptions: identity.ConfidentialIdentityProviderOptions{
			ClientID:        "your-client-id",
			ClientSecret:    "your-client-secret",
			CredentialsType: "ClientSecret",
			Authority: identity.AuthorityConfiguration{
				AuthorityType: identity.AuthorityTypeMultiTenant,
				TenantID:      "your-tenant-id",
			},
			Scopes: []string{"https://redis.azure.com/.default"},
		},
	})
	if err != nil {
		panic(fmt.Errorf("failed to create confidential identity credentials provider: %w", err))
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
