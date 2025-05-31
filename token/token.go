package token

import (
	"time"

	"github.com/redis/go-redis/v9/auth"
)

// Ensure Token implements the auth.Credentials interface.
var _ auth.Credentials = (*Token)(nil)

// New creates a new token with the specified username, password, raw token, expiration time, received at time, and time to live.
// NOTE: The caller is responsible for ensuring the token is valid.
// If the token is invalid, the behavior is undefined.
// - if expiresOn is zero, New returns nil
// - if receivedAt is zero, it will be set to the current time and TTL will be recalculated
// Expiration time and TTL are used to determine when the token should be refreshed.
// TTL is in milliseconds.
// receivedAt + ttl should be within a millisecond of expiresOn
func New(username, password, rawToken string, expiresOn, receivedAt time.Time, ttl int64) *Token {
	if expiresOn.IsZero() {
		return nil
	}
	if receivedAt.IsZero() {
		receivedAt = time.Now()
		ttl = expiresOn.Sub(receivedAt).Milliseconds()
	}

	return &Token{
		username:   username,
		password:   password,
		rawToken:   rawToken,
		expiresOn:  expiresOn,
		receivedAt: receivedAt,
		ttl:        ttl,
	}
}

// Token represents parsed authentication token used to access the Redis server.
// It implements the auth.Credentials interface.
//
// WARNING: Use New() to create a new token.
// Creating a token with Token{} is invalid and will undefined behavior in the TokenManager.
// The zero value of Token is not valid.
type Token struct {
	// username is the username of the user.
	username string
	// password is the password of the user.
	password string
	// expiresOn is the expiration time of the token.
	expiresOn time.Time
	// ttl is the time to live of the token in milliseconds.
	ttl int64
	// rawToken is the authentication token.
	rawToken string
	// receivedAt is the time when the token was received
	receivedAt time.Time
}

// BasicAuth returns the username and password for basic authentication.
func (t *Token) BasicAuth() (string, string) {
	return t.username, t.password
}

// RawCredentials returns the raw credentials for authentication.
func (t *Token) RawCredentials() string {
	return t.RawToken()
}

// RawToken returns the raw token.
func (t *Token) RawToken() string {
	return t.rawToken
}

// ReceivedAt returns the time when the token was received.
func (t *Token) ReceivedAt() time.Time {
	return t.receivedAt
}

// ExpirationOn returns the expiration time of the token.
func (t *Token) ExpirationOn() time.Time {
	return t.expiresOn
}

// TTL returns the time to live of the token.
func (t *Token) TTL() int64 {
	return t.ttl
}

// Copy creates a copy of the token.
func (t *Token) Copy() *Token {
	return copyToken(t)
}

// copyToken creates a copy of the token.
func copyToken(token *Token) *Token {
	if token == nil {
		return nil
	}
	return New(token.username, token.password, token.rawToken, token.expiresOn, token.receivedAt, token.ttl)
}
