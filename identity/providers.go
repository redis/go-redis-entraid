package identity

// CredentialsProviderOptions is a struct that holds the options for the credentials provider.

const (
	// SystemAssignedIdentity is the type of identity that is automatically managed by Azure.
	SystemAssignedIdentity = "SystemAssigned"
	// UserAssignedObjectID is the type of identity that is managed by the user.
	UserAssignedObjectID = "UserAssignedObjectID"

	// ClientSecretCredentialType is the type of credentials that uses a client secret to authenticate.
	ClientSecretCredentialType = "ClientSecret"
	// ClientCertificateCredentialType is the type of credentials that uses a client certificate to authenticate.
	ClientCertificateCredentialType = "ClientCertificate"

	// RedisScopeDefault is the default scope for Redis.
	// This is used to specify the scope that the identity has access to when requesting a token.
	// The scope is typically the URL of the resource that the identity has access to.
	RedisScopeDefault = "https://redis.azure.com/.default"

	// RedisResource is the default resource for Redis.
	// This is used to specify the resource that the identity has access to when requesting a token.
	// The resource is typically the URL of the resource that the identity has access to.
	RedisResource = "https://redis.azure.com"
)
