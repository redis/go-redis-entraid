// Command provider_lifecycle shows how an application owns a credentials
// provider that several Redis clients share:
//
//   - create the provider once and pass it to every client;
//   - closing one client does not stop the shared provider;
//   - close the provider after its clients, also when Redis dialing failed.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"config"

	entraid "github.com/redis/go-redis-entraid"
	"github.com/redis/go-redis-entraid/identity"
	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/auth"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

// run returns errors instead of exiting, so the deferred Close calls run.
func run(ctx context.Context) error {
	cfg, err := config.LoadConfig(os.Getenv("REDIS_ENDPOINTS_CONFIG_PATH"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	endpoint := cfg.Endpoints["standalone-entraid-acl"].Endpoints[0]

	cp, err := entraid.NewManagedIdentityCredentialsProvider(entraid.ManagedIdentityCredentialsProviderOptions{
		ManagedIdentityProviderOptions: identity.ManagedIdentityProviderOptions{
			ManagedIdentityType: identity.SystemAssignedIdentity,
		},
	})
	if err != nil {
		return fmt.Errorf("create credentials provider: %w", err)
	}

	// The application owns the provider. Register its cleanup first: deferred
	// calls run in reverse order, so the clients below close before it.
	if closer, ok := cp.(entraid.CredentialsProviderCloser); ok {
		defer func() {
			if err := closer.Close(); err != nil {
				log.Printf("close credentials provider: %v", err)
			}
		}()
	}

	first, err := newClient(endpoint, cp)
	if err != nil {
		return err
	}
	defer first.Close()

	second, err := newClient(endpoint, cp)
	if err != nil {
		return err
	}
	defer second.Close()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	for name, client := range map[string]*redis.Client{"first": first, "second": second} {
		if err := client.Ping(ctx).Err(); err != nil {
			return fmt.Errorf("ping %s client: %w", name, err)
		}
	}
	fmt.Println("Both clients connected with the shared provider")

	// Closing a client only unsubscribes its own connections. The provider
	// keeps renewing tokens for the other client.
	if err := first.Close(); err != nil {
		return fmt.Errorf("close first client: %w", err)
	}
	if err := second.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping second client after closing the first: %w", err)
	}
	fmt.Println("Second client still works after the first one was closed")
	return nil
}

// newClient parses the URL for each client: every client needs its own Options.
func newClient(url string, cp auth.StreamingCredentialsProvider) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse Redis URL: %w", err)
	}
	opts.StreamingCredentialsProvider = cp
	return redis.NewClient(opts), nil
}
