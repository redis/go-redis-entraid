package token

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	t.Parallel()
	token := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	assert.Equal(t, "username", token.username)
	assert.Equal(t, "password", token.password)
	assert.Equal(t, "rawToken", token.rawToken)
	assert.Equal(t, int64(3600), token.ttl)
}

func TestBasicAuth(t *testing.T) {
	t.Parallel()
	token := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	username, password := token.BasicAuth()
	assert.Equal(t, "username", username)
	assert.Equal(t, "password", password)
}

func TestRawCredentials(t *testing.T) {
	t.Parallel()
	token := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	rawCredentials := token.RawCredentials()
	assert.Equal(t, "rawToken", rawCredentials)
}

func TestExpirationOn(t *testing.T) {
	t.Parallel()
	token := New("username", "password", "rawToken", time.Now().Add(1*time.Hour), time.Now(), 3600)
	expirationOn := token.ExpirationOn()
	assert.True(t, expirationOn.After(time.Now()))
}

func TestTokenExpiration(t *testing.T) {
	t.Parallel()
	token := New("username", "password", "rawToken", time.Now().Add(1*time.Hour), time.Now(), 3600)
	assert.True(t, token.ExpirationOn().After(time.Now()))

	token.expiresOn = time.Now().Add(-1 * time.Hour)
	assert.False(t, token.ExpirationOn().After(time.Now()))
}

func TestTokenReceivedAt(t *testing.T) {
	t.Parallel()
	token := New("username", "password", "rawToken", time.Now(), time.Now().Add(1*time.Hour), 3600)
	assert.True(t, token.receivedAt.After(time.Now().Add(-1*time.Hour)))
	assert.True(t, token.receivedAt.Before(time.Now().Add(1*time.Hour)))
}

func TestTokenTTL(t *testing.T) {
	t.Parallel()
	token := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	assert.Equal(t, int64(3600), token.ttl)

	token.ttl = 7200
	assert.Equal(t, int64(7200), token.ttl)
}

func TestCopyToken(t *testing.T) {
	t.Parallel()
	token := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
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
	copiedToken = copyToken(nil)
	assert.Nil(t, copiedToken)
	// copy empty token
	copiedToken = copyToken(&Token{})
	assert.NotNil(t, copiedToken)
	anotherCopy := copiedToken.Copy()
	anotherCopy.rawToken = "changed"
	assert.NotEqual(t, copiedToken, anotherCopy)
}

func TestTokenCompare(t *testing.T) {
	t.Parallel()
	// Create two tokens with the same credentials
	token1 := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	token2 := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	assert.True(t, token1.compareCredentials(token2))
	assert.True(t, token1.compareRawCredentials(token2))
	assert.True(t, token1.compareToken(token2))

	// Create two tokens with different credentials and different raw credentials
	token3 := New("username", "differentPassword", "differentRawToken", time.Now(), time.Now(), 3600)
	assert.False(t, token1.compareCredentials(token3))
	assert.False(t, token1.compareRawCredentials(token3))
	assert.False(t, token1.compareToken(token3))

	// Create token with same credentials but different rawCredentials
	token4 := New("username", "password", "differentRawToken", time.Now(), time.Now(), 3600)
	assert.False(t, token1.compareRawCredentials(token4))
	assert.False(t, token1.compareToken(token4))
	assert.True(t, token1.compareCredentials(token4))
}

func BenchmarkNew(b *testing.B) {
	now := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		New("username", "password", "rawToken", now, now, 3600)
	}
}

func BenchmarkBasicAuth(b *testing.B) {
	token := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token.BasicAuth()
	}
}

func BenchmarkRawCredentials(b *testing.B) {
	token := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token.RawCredentials()
	}
}

func BenchmarkExpirationOn(b *testing.B) {
	token := New("username", "password", "rawToken", time.Now().Add(1*time.Hour), time.Now(), 3600)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token.ExpirationOn()
	}
}

func BenchmarkCopyToken(b *testing.B) {
	token := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token.Copy()
	}
}

func BenchmarkCompareCredentials(b *testing.B) {
	token1 := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	token2 := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token1.compareCredentials(token2)
	}
}

func BenchmarkCompareRawCredentials(b *testing.B) {
	token1 := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	token2 := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token1.compareRawCredentials(token2)
	}
}

func BenchmarkCompareToken(b *testing.B) {
	token1 := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	token2 := New("username", "password", "rawToken", time.Now(), time.Now(), 3600)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token1.compareToken(token2)
	}
}
