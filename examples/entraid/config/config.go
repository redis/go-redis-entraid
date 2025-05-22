package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// RedisEndpoint represents a Redis endpoint configuration
// It is loaded from a JSON file
type RedisEndpoint struct {
	Username             string   `json:"username,omitempty"`
	Password             string   `json:"password,omitempty"`
	TLS                  bool     `json:"tls"`
	CertificatesLocation string   `json:"certificatesLocation,omitempty"`
	Endpoints            []string `json:"endpoints"`
}

// EntraidConfig represents the configuration for Entra ID authentication
// It is loaded from a both JSON file and environment variables
type EntraidConfig struct {
	// JSON config fields
	Endpoints map[string]RedisEndpoint `json:"endpoints"`

	// Azure environment variables
	AzureClientID              string `json:"-"`
	AzureClientSecret          string `json:"-"`
	AzureTenantID              string `json:"-"`
	AzureAuthority             string `json:"-"`
	AzureRedisScopes           string `json:"-"`
	AzureCert                  string `json:"-"`
	AzurePrivateKey            string `json:"-"`
	AzureUserAssignedManagedID string `json:"-"`
}

// LoadConfig loads the configuration from both JSON file and environment variables
func LoadConfig(configPath string) (*EntraidConfig, error) {
	config := &EntraidConfig{}

	// Load from JSON file first
	if configPath == "" {
		configPath = "../endpoints.json" // Default path if not set
	}

	file, err := os.Open(configPath)
	if err != nil {
		file, err = os.Open("endpoints.json")
		if err != nil {
			return nil, fmt.Errorf("failed to open configuration file: %v", err)
		}
	}

	defer file.Close()
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config.Endpoints)
	if err != nil {
		return nil, fmt.Errorf("failed to decode configuration file: %v", err)
	}
	// Override with environment variables if they exist
	if envClientID := os.Getenv("AZURE_CLIENT_ID"); envClientID != "" {
		config.AzureClientID = envClientID
	}
	if envClientSecret := os.Getenv("AZURE_CLIENT_SECRET"); envClientSecret != "" {
		config.AzureClientSecret = envClientSecret
	}
	if envTenantID := os.Getenv("AZURE_TENANT_ID"); envTenantID != "" {
		config.AzureTenantID = envTenantID
	}
	if envAuthority := os.Getenv("AZURE_AUTHORITY"); envAuthority != "" {
		config.AzureAuthority = envAuthority
	}
	if envRedisScopes := os.Getenv("AZURE_REDIS_SCOPES"); envRedisScopes != "" {
		config.AzureRedisScopes = envRedisScopes
	}
	if envCert := os.Getenv("AZURE_CERT"); envCert != "" {
		config.AzureCert = envCert
	}
	if envPrivateKey := os.Getenv("AZURE_PRIVATE_KEY"); envPrivateKey != "" {
		config.AzurePrivateKey = envPrivateKey
	}
	if envManagedID := os.Getenv("AZURE_USER_ASSIGNED_MANAGED_ID"); envManagedID != "" {
		config.AzureUserAssignedManagedID = envManagedID
	}

	return config, nil
}

// GetRedisScopes returns the Redis scopes as a string slice
func (c *EntraidConfig) GetRedisScopes() []string {
	if c.AzureRedisScopes == "" {
		return []string{"https://redis.azure.com/.default"} // Default scope
	}
	return strings.Split(c.AzureRedisScopes, ",")
}
