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
			expected:      "https://login.microsoftonline.com/common",
			expectError:   false,
		},
		{
			name:          "Multi-Tenant Authority",
			authorityType: AuthorityTypeMultiTenant,
			tenantID:      "12345",
			expected:      "https://login.microsoftonline.com/12345",
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
			name:          "Missing Tenant ID for Multi-Tenant",
			authorityType: AuthorityTypeMultiTenant,
			expectError:   true,
		},
		{
			name:          "Missing Authority for Custom",
			authorityType: AuthorityTypeCustom,
			expectError:   true,
		},
		{
			name:          "Default Authority Type with Tenant ID",
			authorityType: AuthorityTypeDefault,
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
	ac := AuthorityConfiguration{}
	result, err := ac.getAuthority()
	assert.NoError(t, err)
	assert.Equal(t, "https://login.microsoftonline.com/common", result)
}

func TestAuthorityConfigurationMultiTenant(t *testing.T) {
	t.Parallel()
	ac := AuthorityConfiguration{
		AuthorityType: AuthorityTypeMultiTenant,
		TenantID:      "12345",
	}
	result, err := ac.getAuthority()
	assert.NoError(t, err)
	assert.Equal(t, "https://login.microsoftonline.com/12345", result)
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
		AuthorityType: AuthorityTypeMultiTenant,
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
	assert.Equal(t, "https://login.microsoftonline.com/common", result)
}

func TestAuthorityConfigurationDefaultAuthorityTypeWithTenantID(t *testing.T) {
	t.Parallel()
	ac := AuthorityConfiguration{
		AuthorityType: AuthorityTypeDefault,
		TenantID:      "12345",
	}
	result, err := ac.getAuthority()
	assert.NoError(t, err)
	assert.Equal(t, "https://login.microsoftonline.com/common", result)
}
