package token

import (
	"time"

	"github.com/redis/go-redis/v9/auth"
)

// Ensure Token implements the auth.Credentials interface.
var _ auth.Credentials = (*Token)(nil)

// New creates a new token with the specified username, password, raw token, expiration time, received at time, and time to live.
// NOTE: This won't do any validation on the token, expiresOn, receivedAt, or ttl. It will simply create a new token instance.
func New(username, password, rawToken string, expiresOn, receivedAt time.Time, ttl int64) *Token {
	return &Token{
		username:   username,
		password:   password,
		expiresOn:  expiresOn,
		receivedAt: receivedAt,
		ttl:        ttl,
		rawToken:   rawToken,
	}
}

// Token represents parsed authentication token used to access the Redis server.
// It implements the auth.Credentials interface.
type Token struct {
	// username is the username of the user.
	username string
	// password is the password of the user.
	password string
	// expiresOn is the expiration time of the token.
	expiresOn time.Time
	// ttl is the time to live of the token.
	ttl int64
	// rawToken is the authentication token.
	rawToken string
	// receivedAt is the time when the token was received.
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

// Copy creates a copy of the token.
func (t *Token) Copy() *Token {
	return copyToken(t)
}

// compareCredentials two tokens if they are the same credentials
func (t *Token) compareCredentials(token *Token) bool {
	return t.username == token.username && t.password == token.password
}

// compareRawCredentials two tokens if they are the same raw credentials
func (t *Token) compareRawCredentials(token *Token) bool {
	return t.rawToken == token.rawToken
}

// compareToken compares two tokens if they are the same token
func (t *Token) compareToken(token *Token) bool {
	return t.compareCredentials(token) && t.compareRawCredentials(token)
}

// copyToken creates a copy of the token.
func copyToken(token *Token) *Token {
	if token == nil {
		return nil
	}
	return New(token.username, token.password, token.rawToken, token.expiresOn, token.receivedAt, token.ttl)
}
