package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := t.TempDir()

	// Test cases
	tests := []struct {
		name           string
		configContent  string
		envVars        map[string]string
		expectedConfig *EntraidConfig
		expectError    bool
		configPath     string // Add configPath to test different paths
	}{
		{
			name: "valid config file",
			configContent: `{
				"endpoint1": {
					"username": "testuser",
					"password": "testpass",
					"tls": true,
					"certificatesLocation": "/path/to/certs",
					"endpoints": ["redis1:6379", "redis2:6379"]
				}
			}`,
			expectedConfig: &EntraidConfig{
				Endpoints: map[string]RedisEndpoint{
					"endpoint1": {
						Username:             "testuser",
						Password:             "testpass",
						TLS:                  true,
						CertificatesLocation: "/path/to/certs",
						Endpoints:            []string{"redis1:6379", "redis2:6379"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid JSON",
			configContent: `{
				"endpoint1": {
					"username": "testuser",
					"password": "testpass",
					"tls": true,
					"certificatesLocation": "/path/to/certs",
					"endpoints": ["redis1:6379", "redis2:6379"]
				}
			`, // Missing closing brace
			expectError: true,
		},
		{
			name: "config with environment variables",
			configContent: `{
				"endpoint1": {
					"endpoints": ["redis1:6379"]
				}
			}`,
			envVars: map[string]string{
				"AZURE_CLIENT_ID":     "test-client-id",
				"AZURE_CLIENT_SECRET": "test-client-secret",
				"AZURE_TENANT_ID":     "test-tenant-id",
				"AZURE_AUTHORITY":     "test-authority",
				"AZURE_REDIS_SCOPES":  "scope1,scope2",
			},
			expectedConfig: &EntraidConfig{
				Endpoints: map[string]RedisEndpoint{
					"endpoint1": {
						Endpoints: []string{"redis1:6379"},
					},
				},
				AzureClientID:     "test-client-id",
				AzureClientSecret: "test-client-secret",
				AzureTenantID:     "test-tenant-id",
				AzureAuthority:    "test-authority",
				AzureRedisScopes:  "scope1,scope2",
			},
			expectError: false,
		},
		{
			name:        "non-existent config file",
			configPath:  "non_existent.json",
			expectError: true,
		},
		{
			name:          "empty config file",
			configContent: `{}`,
			expectedConfig: &EntraidConfig{
				Endpoints: map[string]RedisEndpoint{},
			},
			expectError: false,
		},
		{
			name: "config with all Azure environment variables",
			configContent: `{
				"endpoint1": {
					"endpoints": ["redis1:6379"]
				}
			}`,
			envVars: map[string]string{
				"AZURE_CLIENT_ID":                "test-client-id",
				"AZURE_CLIENT_SECRET":            "test-client-secret",
				"AZURE_TENANT_ID":                "test-tenant-id",
				"AZURE_AUTHORITY":                "test-authority",
				"AZURE_REDIS_SCOPES":             "scope1,scope2",
				"AZURE_CERT":                     "test-cert",
				"AZURE_PRIVATE_KEY":              "test-key",
				"AZURE_USER_ASSIGNED_MANAGED_ID": "test-managed-id",
			},
			expectedConfig: &EntraidConfig{
				Endpoints: map[string]RedisEndpoint{
					"endpoint1": {
						Endpoints: []string{"redis1:6379"},
					},
				},
				AzureClientID:              "test-client-id",
				AzureClientSecret:          "test-client-secret",
				AzureTenantID:              "test-tenant-id",
				AzureAuthority:             "test-authority",
				AzureRedisScopes:           "scope1,scope2",
				AzureCert:                  "test-cert",
				AzurePrivateKey:            "test-key",
				AzureUserAssignedManagedID: "test-managed-id",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test config file if content is provided
			var configPath string
			if tt.configContent != "" {
				configPath = filepath.Join(tempDir, "endpoints.json")
				if err := os.WriteFile(configPath, []byte(tt.configContent), 0644); err != nil {
					t.Fatalf("Failed to create test config file: %v", err)
				}
			} else if tt.configPath != "" {
				configPath = tt.configPath
			}

			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}
			defer func() {
				// Clean up environment variables
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			// Load config
			config, err := LoadConfig(configPath)

			// Check error
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Verify config
			if len(config.Endpoints) != len(tt.expectedConfig.Endpoints) {
				t.Errorf("Expected %d endpoints, got %d", len(tt.expectedConfig.Endpoints), len(config.Endpoints))
			}

			// Check environment variables
			if tt.expectedConfig.AzureClientID != "" && config.AzureClientID != tt.expectedConfig.AzureClientID {
				t.Errorf("Expected AzureClientID %s, got %s", tt.expectedConfig.AzureClientID, config.AzureClientID)
			}
			if tt.expectedConfig.AzureClientSecret != "" && config.AzureClientSecret != tt.expectedConfig.AzureClientSecret {
				t.Errorf("Expected AzureClientSecret %s, got %s", tt.expectedConfig.AzureClientSecret, config.AzureClientSecret)
			}
			if tt.expectedConfig.AzureTenantID != "" && config.AzureTenantID != tt.expectedConfig.AzureTenantID {
				t.Errorf("Expected AzureTenantID %s, got %s", tt.expectedConfig.AzureTenantID, config.AzureTenantID)
			}
			if tt.expectedConfig.AzureAuthority != "" && config.AzureAuthority != tt.expectedConfig.AzureAuthority {
				t.Errorf("Expected AzureAuthority %s, got %s", tt.expectedConfig.AzureAuthority, config.AzureAuthority)
			}
			if tt.expectedConfig.AzureRedisScopes != "" && config.AzureRedisScopes != tt.expectedConfig.AzureRedisScopes {
				t.Errorf("Expected AzureRedisScopes %s, got %s", tt.expectedConfig.AzureRedisScopes, config.AzureRedisScopes)
			}
			if tt.expectedConfig.AzureCert != "" && config.AzureCert != tt.expectedConfig.AzureCert {
				t.Errorf("Expected AzureCert %s, got %s", tt.expectedConfig.AzureCert, config.AzureCert)
			}
			if tt.expectedConfig.AzurePrivateKey != "" && config.AzurePrivateKey != tt.expectedConfig.AzurePrivateKey {
				t.Errorf("Expected AzurePrivateKey %s, got %s", tt.expectedConfig.AzurePrivateKey, config.AzurePrivateKey)
			}
			if tt.expectedConfig.AzureUserAssignedManagedID != "" && config.AzureUserAssignedManagedID != tt.expectedConfig.AzureUserAssignedManagedID {
				t.Errorf("Expected AzureUserAssignedManagedID %s, got %s", tt.expectedConfig.AzureUserAssignedManagedID, config.AzureUserAssignedManagedID)
			}
		})
	}
}

func TestGetRedisScopes(t *testing.T) {
	tests := []struct {
		name           string
		config         *EntraidConfig
		expectedScopes []string
	}{
		{
			name: "default scope",
			config: &EntraidConfig{
				AzureRedisScopes: "",
			},
			expectedScopes: []string{"https://redis.azure.com/.default"},
		},
		{
			name: "custom scopes",
			config: &EntraidConfig{
				AzureRedisScopes: "scope1,scope2,scope3",
			},
			expectedScopes: []string{"scope1", "scope2", "scope3"},
		},
		{
			name: "single scope",
			config: &EntraidConfig{
				AzureRedisScopes: "single-scope",
			},
			expectedScopes: []string{"single-scope"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scopes := tt.config.GetRedisScopes()
			if len(scopes) != len(tt.expectedScopes) {
				t.Errorf("Expected %d scopes, got %d", len(tt.expectedScopes), len(scopes))
				return
			}
			for i, scope := range scopes {
				if scope != tt.expectedScopes[i] {
					t.Errorf("Expected scope %s at index %d, got %s", tt.expectedScopes[i], i, scope)
				}
			}
		})
	}
}

func TestLoadConfigDefaultPath(t *testing.T) {
	// Test loading config from default path
	config, err := LoadConfig("")
	if err != nil {
		t.Logf("Expected error when loading from default path: %v", err)
	} else {
		t.Log("Successfully loaded config from default path")
	}
	_ = config // Use config to avoid unused variable warning
}
