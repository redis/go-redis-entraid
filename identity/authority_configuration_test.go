package identity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAuthorityConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		authorityType string
		tenantID      string
		authority     string
		expected      string
		expectError   bool
	}{
		{
			name:          "Default Authority",
			authorityType: AuthorityTypeDefault,
			tenantID:      "12345",
			expected:      "https://login.microsoftonline.com/12345",
			expectError:   false,
		},
		{
			name:          "Multi-Tenant Authority",
			authorityType: AuthorityTypeMultiTenant,
			expected:      "https://login.microsoftonline.com/common",
			expectError:   false,
		},
		{
			name:          "Custom Authority",
			authorityType: AuthorityTypeCustom,
			authority:     "https://custom-authority.com",
			expected:      "https://custom-authority.com",
			expectError:   false,
		},
		{
			name:          "Invalid Authority Type",
			authorityType: "invalid",
			expectError:   true,
		},
		{
			name:          "Missing Tenant ID for Default",
			authorityType: AuthorityTypeDefault,
			expectError:   true,
		},
		{
			name:          "Missing Authority for Custom",
			authorityType: AuthorityTypeCustom,
			expectError:   true,
		},
		{
			name:          "Multi-Tenant Authority Type with Tenant ID",
			authorityType: AuthorityTypeMultiTenant,
			tenantID:      "12345",
			expected:      "https://login.microsoftonline.com/common",
			expectError:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ac := AuthorityConfiguration{
				AuthorityType: test.authorityType,
				TenantID:      test.tenantID,
				Authority:     test.authority,
			}
			result, err := ac.getAuthority()
			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, test.expected, result)
			}
		})
	}
}

func TestAuthorityConfigurationDefault(t *testing.T) {
	t.Parallel()
	ac := AuthorityConfiguration{
		AuthorityType: AuthorityTypeDefault,
		TenantID:      "12345",
	}
	result, err := ac.getAuthority()
	assert.NoError(t, err)
	assert.Equal(t, "https://login.microsoftonline.com/12345", result)
}

func TestAuthorityConfigurationMultiTenant(t *testing.T) {
	t.Parallel()
	ac := AuthorityConfiguration{
		AuthorityType: AuthorityTypeMultiTenant,
	}
	result, err := ac.getAuthority()
	assert.NoError(t, err)
	assert.Equal(t, "https://login.microsoftonline.com/common", result)
}

func TestAuthorityConfigurationCustom(t *testing.T) {
	t.Parallel()
	ac := AuthorityConfiguration{
		AuthorityType: AuthorityTypeCustom,
		Authority:     "https://custom-authority.com",
	}
	result, err := ac.getAuthority()
	assert.NoError(t, err)
	assert.Equal(t, "https://custom-authority.com", result)
}

func TestAuthorityConfigurationInvalid(t *testing.T) {
	t.Parallel()
	ac := AuthorityConfiguration{
		AuthorityType: "invalid",
	}
	result, err := ac.getAuthority()
	assert.Error(t, err)
	assert.Equal(t, "", result)
}

func TestAuthorityConfigurationMissingTenantID(t *testing.T) {
	t.Parallel()
	ac := AuthorityConfiguration{
		AuthorityType: AuthorityTypeDefault,
	}
	result, err := ac.getAuthority()
	assert.Error(t, err)
	assert.Equal(t, "", result)
}

func TestAuthorityConfigurationMissingAuthority(t *testing.T) {
	t.Parallel()
	ac := AuthorityConfiguration{
		AuthorityType: AuthorityTypeCustom,
	}
	result, err := ac.getAuthority()
	assert.Error(t, err)
	assert.Equal(t, "", result)
}

func TestAuthorityConfigurationDefaultAuthorityType(t *testing.T) {
	t.Parallel()
	ac := AuthorityConfiguration{
		TenantID: "12345",
	}
	result, err := ac.getAuthority()
	assert.NoError(t, err)
	assert.Equal(t, "https://login.microsoftonline.com/12345", result)
}

func TestAuthorityConfigurationDefaultAuthorityTypeWithTenantID(t *testing.T) {
	t.Parallel()
	ac := AuthorityConfiguration{
		AuthorityType: AuthorityTypeDefault,
		TenantID:      "12345",
	}
	result, err := ac.getAuthority()
	assert.NoError(t, err)
	assert.Equal(t, "https://login.microsoftonline.com/12345", result)
}

// TestIssueScenario tests the exact scenario reported in the GitHub issue:
// Single-tenant application should use AuthorityTypeDefault with a specific tenant ID
// and should produce a tenant-specific authority URL (not /common)
func TestIssueScenario(t *testing.T) {
	t.Parallel()

	// This is the configuration from the issue report
	// that should produce a tenant-specific authority URL
	tenantID := "test-tenant-id-123"

	// Single-tenant application configuration as documented
	ac := AuthorityConfiguration{
		AuthorityType: AuthorityTypeDefault,
		TenantID:      tenantID,
	}

	authority, err := ac.getAuthority()

	// Should not error
	assert.NoError(t, err)

	// Should produce tenant-specific URL, NOT /common
	expectedAuthority := "https://login.microsoftonline.com/test-tenant-id-123"
	assert.Equal(t, expectedAuthority, authority, "Single-tenant application should use tenant-specific authority URL")

	// Verify it's NOT the common endpoint
	assert.NotEqual(t, "https://login.microsoftonline.com/common", authority, "Single-tenant application should NOT use /common endpoint")
}

// TestMultiTenantScenario tests that multi-tenant applications use the common endpoint
func TestMultiTenantScenario(t *testing.T) {
	t.Parallel()

	// Multi-tenant application configuration as documented
	ac := AuthorityConfiguration{
		AuthorityType: AuthorityTypeMultiTenant,
	}

	authority, err := ac.getAuthority()

	// Should not error
	assert.NoError(t, err)

	// Should produce /common URL for multi-tenant
	expectedAuthority := "https://login.microsoftonline.com/common"
	assert.Equal(t, expectedAuthority, authority, "Multi-tenant application should use /common endpoint")
}
