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

	// Example 1: System-assigned managed identity
	// This is the simplest configuration, as it uses the system-assigned identity
	// automatically managed by Azure
	systemAssignedCP, err := entraid.NewManagedIdentityCredentialsProvider(entraid.ManagedIdentityCredentialsProviderOptions{
		ManagedIdentityProviderOptions: identity.ManagedIdentityProviderOptions{
			ManagedIdentityType: identity.SystemAssignedIdentity,
			Scopes:              []string{"https://redis.azure.com/.default"},
		},
	})
	if err != nil {
		panic(fmt.Errorf("failed to create system-assigned managed identity credentials provider: %w", err))
	}

	// Example 2: User-assigned managed identity
	// Uncomment and use this example if you want to use a user-assigned managed identity
	/*
		userAssignedCP, err := entraid.NewManagedIdentityCredentialsProvider(entraid.ManagedIdentityCredentialsProviderOptions{
			ManagedIdentityProviderOptions: identity.ManagedIdentityProviderOptions{
				ManagedIdentityType:   identity.UserAssignedIdentity,
				UserAssignedClientID: "your-user-assigned-identity-client-id",
				Scopes:               []string{"https://redis.azure.com/.default"},
			},
		})
		if err != nil {
			panic(fmt.Errorf("failed to create user-assigned managed identity credentials provider: %w", err))
		}
	*/

	// Create Redis client with the credentials provider
	redisClient := redis.NewClient(&redis.Options{
		Addr:                         "your-redis-host:6379",
		StreamingCredentialsProvider: systemAssignedCP, // Change to userAssignedCP if using user-assigned identity
	})

	// Test the connection
	ok, err := redisClient.Ping(ctx).Result()
	if err != nil {
		panic(fmt.Errorf("failed to ping Redis: %w", err))
	}
	fmt.Println("Ping result:", ok)
}
