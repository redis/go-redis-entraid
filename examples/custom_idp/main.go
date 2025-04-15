package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	entraid "github.com/redis-developer/go-redis-entraid"
	"github.com/redis-developer/go-redis-entraid/manager"
	"github.com/redis-developer/go-redis-entraid/shared"
	"github.com/redis-developer/go-redis-entraid/token"
	redis "github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()
	idp := NewFakeIdentityProvider("local", "pass")
	parser := &fakeIdentityProviderResponseParser{}
	// create token manager

	tm, err := manager.NewTokenManager(idp, manager.TokenManagerOptions{
		IdentityProviderResponseParser: parser,
	})

	cp, err := entraid.NewCredentialsProvider(tm, entraid.CredentialsProviderOptions{})
	if err != nil {
		panic(err)
	}

	redis := redis.NewClient(&redis.Options{
		Addr:                         ":6379",
		StreamingCredentialsProvider: cp,
	})

	ok, err := redis.Ping(ctx).Result()
	if err != nil {
		panic(err)
	}
	fmt.Println("Ping result:", ok)
}

var _ entraid.IdentityProvider = (*FakeIdentityProvider)(nil)

type FakeIdentityProvider struct {
	username string
	password string
}

// RequestToken simulates a request to an identity provider and returns a fake token.
// In a real implementation, this would involve making a network request to the identity provider.
func (f *FakeIdentityProvider) RequestToken() (entraid.IdentityProviderResponse, error) {
	// Simulate a successful token request
	return shared.NewIDPResponse(
		shared.ResponseTypeRawToken,
		fmt.Sprintf("%s:%s:%d", f.username, f.password, time.Now().Add(1*time.Hour).Unix()),
	)
}

// NewFakeIdentityProvider creates a new instance of FakeIdentityProvider with the given username and password.
func NewFakeIdentityProvider(username, password string) *FakeIdentityProvider {
	return &FakeIdentityProvider{
		username: username,
		password: password,
	}
}

type fakeIdentityProviderResponseParser struct {
}

// ParseResponse simulates the parsing of a response from an identity provider.
func (f *fakeIdentityProviderResponseParser) ParseResponse(response entraid.IdentityProviderResponse) (*token.Token, error) {
	if response.Type() == shared.ResponseTypeRawToken {
		rawToken := response.RawToken()
		username, password := "", ""
		var expiresOnUnix int64

		// parse the raw token string
		// assuming the format is "username:password:expiresOnUnix"
		// where expiresOnUnix is a unix timestamp
		parts := strings.Split(rawToken, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid raw token format")
		}
		username = parts[0]
		password = parts[1]
		expiresOnUnix, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse raw token: %w", err)
		}

		// convert the unix timestamp to time.Time
		expiresOn := time.Unix(expiresOnUnix, 0)
		now := time.Now()
		return token.New(username, password, rawToken, expiresOn, now, int64(expiresOn.Sub(now).Seconds())), nil
	}
	return nil, fmt.Errorf("unsupported response type: %s", response.Type())
}

var _ entraid.IdentityProviderResponseParser = (*fakeIdentityProviderResponseParser)(nil)
