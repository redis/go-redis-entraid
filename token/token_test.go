package token

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	t.Parallel()
	expiration := time.Now().Add(1 * time.Hour)
	receivedAt := time.Now()
	ttl := expiration.UnixMilli() - receivedAt.UnixMilli()
	token := New("username", "password", "rawToken", expiration, receivedAt, ttl)
	assert.Equal(t, "username", token.username)
	assert.Equal(t, "password", token.password)
	assert.Equal(t, "rawToken", token.rawToken)
	assert.Equal(t, expiration, token.expiresOn)
	assert.Equal(t, receivedAt, token.receivedAt)
	assert.Equal(t, ttl, token.ttl)
}

func TestBasicAuth(t *testing.T) {
	t.Parallel()
	username := "username12"
	password := "password32"
	rawToken := fmt.Sprintf("%s:%s", username, password)
	expiration := time.Now().Add(1 * time.Hour)
	receivedAt := time.Now()
	ttl := expiration.UnixMilli() - receivedAt.UnixMilli()
	token := New(username, password, rawToken, expiration, receivedAt, ttl)
	baUsername, baPassword := token.BasicAuth()
	assert.Equal(t, username, baUsername)
	assert.Equal(t, password, baPassword)
}

func TestRawCredentials(t *testing.T) {
	t.Parallel()
	username := "username12"
	password := "password32"
	rawToken := fmt.Sprintf("%s:%s", username, password)
	expiration := time.Now().Add(1 * time.Hour)
	receivedAt := time.Now()
	ttl := expiration.UnixMilli() - receivedAt.UnixMilli()
	token := New(username, password, rawToken, expiration, receivedAt, ttl)
	rawCredentials := token.RawCredentials()
	assert.Equal(t, rawToken, rawCredentials)
	assert.Contains(t, rawCredentials, username)
	assert.Contains(t, rawCredentials, password)
}

func TestExpirationOn(t *testing.T) {
	t.Parallel()
	username := "username12"
	password := "password32"
	rawToken := fmt.Sprintf("%s:%s", username, password)
	expiration := time.Now().Add(1 * time.Hour)
	receivedAt := time.Now()
	ttl := expiration.UnixMilli() - receivedAt.UnixMilli()
	token := New(username, password, rawToken, expiration, receivedAt, ttl)
	expirationOn := token.ExpirationOn()
	assert.True(t, expirationOn.After(time.Now()))
	assert.Equal(t, expiration, expirationOn)
}

func TestTokenTTL(t *testing.T) {
	t.Parallel()
	username := "username12"
	password := "password32"
	rawToken := fmt.Sprintf("%s:%s", username, password)
	expiration := time.Now().Add(1 * time.Hour)
	receivedAt := time.Now()
	ttl := expiration.UnixMilli() - receivedAt.UnixMilli()
	token := New(username, password, rawToken, expiration, receivedAt, ttl)
	assert.Equal(t, ttl, token.TTL())
}

func TestCopyToken(t *testing.T) {
	t.Parallel()
	token := New("username", "password", "rawToken", time.Now(), time.Now(), time.Hour.Milliseconds())
	copiedToken := copyToken(token)

	assert.Equal(t, token.username, copiedToken.username)
	assert.Equal(t, token.password, copiedToken.password)
	assert.Equal(t, token.rawToken, copiedToken.rawToken)
	assert.Equal(t, token.ttl, copiedToken.ttl)
	assert.Equal(t, token.expiresOn, copiedToken.expiresOn)
	assert.Equal(t, token.receivedAt, copiedToken.receivedAt)

	// change the copied token
	copiedToken.expiresOn = time.Now().Add(-1 * time.Hour)
	assert.NotEqual(t, token.expiresOn, copiedToken.expiresOn)

	// copy nil
	nilToken := copyToken(nil)
	assert.Nil(t, nilToken)
	// copy empty token
	emptyToken := copyToken(&Token{})
	assert.Nil(t, emptyToken)
	anotherCopy := copiedToken.Copy()
	anotherCopy.rawToken = "changed"
	assert.NotEqual(t, copiedToken, anotherCopy)
	assert.NotEqual(t, copiedToken.rawToken, anotherCopy.rawToken)
}

func TestTokenReceivedAt(t *testing.T) {
	t.Parallel()
	// Create a token with a specific receivedAt time
	receivedAt := time.Now()
	token := New("username", "password", "rawToken", time.Now(), receivedAt, time.Hour.Milliseconds())

	assert.True(t, token.receivedAt.After(time.Now().Add(-1*time.Hour)))
	assert.True(t, token.receivedAt.Before(time.Now().Add(1*time.Hour)))

	// Check if the receivedAt time is set correctly
	assert.Equal(t, receivedAt, token.ReceivedAt())

	tcopiedToken := token.Copy()
	// Check if the copied token has the same receivedAt time
	assert.Equal(t, receivedAt, tcopiedToken.ReceivedAt())
	// Check if the copied token is not the same instance as the original token
	assert.NotSame(t, token, tcopiedToken)
	// Check if the copied token is a new instance
	assert.NotNil(t, tcopiedToken)

	emptyRecievedAt := New("username", "password", "rawToken", time.Now(), time.Time{}, time.Hour.Milliseconds())
	assert.True(t, emptyRecievedAt.ReceivedAt().After(time.Now().Add(-1*time.Hour)))
	assert.True(t, emptyRecievedAt.ReceivedAt().Before(time.Now().Add(1*time.Hour)))
}

func BenchmarkNew(b *testing.B) {
	now := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		New("username", "password", "rawToken", now, now, time.Hour.Milliseconds())
	}
}

func BenchmarkBasicAuth(b *testing.B) {
	token := New("username", "password", "rawToken", time.Now(), time.Now(), time.Hour.Milliseconds())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token.BasicAuth()
	}
}

func BenchmarkRawCredentials(b *testing.B) {
	token := New("username", "password", "rawToken", time.Now(), time.Now(), time.Hour.Milliseconds())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token.RawCredentials()
	}
}

func BenchmarkExpirationOn(b *testing.B) {
	token := New("username", "password", "rawToken", time.Now().Add(1*time.Hour), time.Now(), time.Hour.Milliseconds())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token.ExpirationOn()
	}
}

func BenchmarkCopyToken(b *testing.B) {
	token := New("username", "password", "rawToken", time.Now(), time.Now(), time.Hour.Milliseconds())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token.Copy()
	}
}
