package main

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"testing"
	"time"

	"config"

	entraid "github.com/redis/go-redis-entraid"
	"github.com/redis/go-redis-entraid/identity"
	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis/v9"
)

func TestClientCert(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	cfg, err := config.LoadConfig(os.Getenv("REDIS_ENDPOINTS_CONFIG_PATH"))
	if err != nil {
		t.Logf("Failed to load config: %v", err)
	}

	pk, err := parsePrivateKeyTest(cfg.AzurePrivateKey)
	if err != nil {
		t.Fatalf("Failed to parse private key: %v", err)
	}

	// Create a confidential identity credentials provider with certificate authentication
	cp, err := entraid.NewConfidentialCredentialsProvider(entraid.ConfidentialCredentialsProviderOptions{
		CredentialsProviderOptions: entraid.CredentialsProviderOptions{
			TokenManagerOptions: manager.TokenManagerOptions{
				ExpirationRefreshRatio: 0.001,           // Set to refresh very early
				LowerRefreshBound:      time.Second * 1, // Set lower bound to 1 second
			},
		},
		ConfidentialIdentityProviderOptions: identity.ConfidentialIdentityProviderOptions{
			ClientID:        cfg.AzureClientID,
			ClientSecret:    cfg.AzureClientSecret,
			CredentialsType: identity.ClientCertificateCredentialType,
			Authority: identity.AuthorityConfiguration{
				AuthorityType: identity.AuthorityTypeMultiTenant,
				TenantID:      cfg.AzureTenantID,
			},
			Scopes:           cfg.GetRedisScopes(),
			ClientCert:       parseCertificatesTest(cfg.AzureCert),
			ClientPrivateKey: pk,
		},
	})
	if err != nil {
		t.Logf("Failed to create credentials provider: %v", err)
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

func decodeBase64PemTest(pemData string) string {
	decoded, err := base64.StdEncoding.DecodeString(pemData)
	if err != nil {
		panic(fmt.Sprintf("Failed to decode base64: %v", err))
	}
	return string(decoded)
}

func parsePrivateKeyTest(base64data string) (*rsa.PrivateKey, error) {
	var privateKey *rsa.PrivateKey
	decoded := decodeBase64PemTest(base64data)
	pk, err := x509.ParsePKCS8PrivateKey([]byte(decoded))
	if err != nil {
		return nil, fmt.Errorf("failed to parse pkcs8 key: %w", err)
	}
	privateKey, _ = pk.(*rsa.PrivateKey)
	if privateKey == nil {
		pk, err = x509.ParsePKCS1PrivateKey([]byte(decoded))
		if err != nil {
			return nil, fmt.Errorf("failed to parse pkcs1 key: %w", err)
		}
		privateKey, _ = pk.(*rsa.PrivateKey)
	}
	return privateKey, nil
}

func parseCertificatesTest(pemData string) []*x509.Certificate {
	var certs []*x509.Certificate
	decoded := decodeBase64PemTest(pemData)
	for {
		block, rest := pem.Decode([]byte(decoded))
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				// Log but don't fail
				continue
			}
			certs = append(certs, cert)
		}
		decoded = string(rest)
	}
	if len(certs) == 0 {
		decoded := decodeBase64PemTest(pemData)
		cert, err := x509.ParseCertificate([]byte(decoded))
		if err == nil {
			certs = append(certs, cert)
		}
	}
	return certs
}

