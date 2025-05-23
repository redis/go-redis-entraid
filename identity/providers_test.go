package identity

import (
	"testing"
)

func TestConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{
			name:     "SystemAssignedIdentity",
			got:      SystemAssignedIdentity,
			expected: "SystemAssigned",
		},
		{
			name:     "UserAssignedObjectID",
			got:      UserAssignedObjectID,
			expected: "UserAssignedObjectID",
		},
		{
			name:     "ClientSecretCredentialType",
			got:      ClientSecretCredentialType,
			expected: "ClientSecret",
		},
		{
			name:     "ClientCertificateCredentialType",
			got:      ClientCertificateCredentialType,
			expected: "ClientCertificate",
		},
		{
			name:     "RedisScopeDefault",
			got:      RedisScopeDefault,
			expected: "https://redis.azure.com/.default",
		},
		{
			name:     "RedisResource",
			got:      RedisResource,
			expected: "https://redis.azure.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}
