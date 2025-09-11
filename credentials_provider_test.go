package entraid

// This file contains comprehensive tests for the StreamingCredentialsProvider deadlock bug.
//
// Bug Summary:
// A deadlock occurs when listener callbacks (OnNext/OnError) are invoked while holding RLock,
// and the listener callback triggers an unsubscribe operation that tries to acquire Lock.
// Since RWMutex doesn't allow upgrading read lock to write lock, this causes a deadlock.
//
// Real-world scenario:
// 1. Provider calls onTokenNext/onTokenError while holding RLock
// 2. ReAuthCredentialsListener.OnNext/OnError triggers re-authentication
// 3. Re-auth fails and closes the Redis connection
// 4. Connection close triggers provider's unsubscribe function
// 5. Unsubscribe tries to acquire Lock while RLock is still held
// 6. Deadlock occurs, blocking token refresh indefinitely
//
// To reproduce the bug in real scenarios:
// 1. Set up Redis with authentication
// 2. Use StreamingCredentialsProvider with token refresh
// 3. Simulate authentication failures that trigger connection close
// 4. Observe that token refresh hangs indefinitely

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis-entraid/identity"
	"github.com/redis/go-redis-entraid/manager"
	"github.com/redis/go-redis-entraid/shared"
	"github.com/redis/go-redis-entraid/token"
	"github.com/redis/go-redis/v9/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCredentialsProviderErrorScenarios(t *testing.T) {
	t.Run("token manager start error", func(t *testing.T) {
		// Create a test provider with invalid options
		options := ConfidentialCredentialsProviderOptions{
			CredentialsProviderOptions: CredentialsProviderOptions{
				TokenManagerOptions: manager.TokenManagerOptions{
					ExpirationRefreshRatio: 0.7,
				},
			},
			ConfidentialIdentityProviderOptions: identity.ConfidentialIdentityProviderOptions{
				ClientID:        "test-client-id",
				CredentialsType: "invalid-type", // Invalid credentials type
				ClientSecret:    "test-secret",
				Scopes:          []string{identity.RedisScopeDefault},
				Authority:       identity.AuthorityConfiguration{},
			},
		}

		provider, err := NewConfidentialCredentialsProvider(options)
		assert.Error(t, err)
		assert.Nil(t, provider)
	})

	t.Run("token manager get token error", func(t *testing.T) {
		// Create a test provider with invalid options
		options := ConfidentialCredentialsProviderOptions{
			CredentialsProviderOptions: CredentialsProviderOptions{
				TokenManagerOptions: manager.TokenManagerOptions{
					ExpirationRefreshRatio: 0.7,
				},
			},
			ConfidentialIdentityProviderOptions: identity.ConfidentialIdentityProviderOptions{
				ClientID:        "test-client-id",
				CredentialsType: identity.ClientSecretCredentialType,
				ClientSecret:    "", // Empty client secret
				Scopes:          []string{identity.RedisScopeDefault},
				Authority:       identity.AuthorityConfiguration{},
			},
		}

		provider, err := NewConfidentialCredentialsProvider(options)
		assert.Error(t, err)
		assert.Nil(t, provider)
	})

	t.Run("concurrent error handling", func(t *testing.T) {
		// Create a test provider with invalid options
		options := ManagedIdentityCredentialsProviderOptions{
			CredentialsProviderOptions: CredentialsProviderOptions{
				TokenManagerOptions: manager.TokenManagerOptions{
					ExpirationRefreshRatio: 0.7,
				},
			},
			ManagedIdentityProviderOptions: identity.ManagedIdentityProviderOptions{
				ManagedIdentityType: "invalid-type", // Invalid managed identity type
				Scopes:              []string{identity.RedisScopeDefault},
			},
		}

		provider, err := NewManagedIdentityCredentialsProvider(options)
		assert.Error(t, err)
		assert.Nil(t, provider)
	})

	t.Run("concurrent token updates", func(t *testing.T) {
		// Create a test provider with invalid options
		options := DefaultAzureCredentialsProviderOptions{
			CredentialsProviderOptions: CredentialsProviderOptions{
				TokenManagerOptions: manager.TokenManagerOptions{
					ExpirationRefreshRatio: 0.7,
				},
			},
			DefaultAzureIdentityProviderOptions: identity.DefaultAzureIdentityProviderOptions{
				Scopes: []string{}, // Empty scopes
			},
		}

		provider, err := NewDefaultAzureCredentialsProvider(options)
		// bad options - empty scopes
		assert.Error(t, err)
		assert.Nil(t, provider)
	})
}

func TestCredentialsProviderWithMockIdentityProvider(t *testing.T) {
	t.Parallel()

	t.Run("Subscribe and Unsubscribe", func(t *testing.T) {
		t.Parallel()

		// Create mock token manager
		tm := &fakeTokenManager{
			token: token.New(
				"test",
				"test",
				"test-token",
				time.Now().Add(time.Hour),
				time.Now(),
				time.Hour.Milliseconds(),
			),
		}

		// Create credentials provider
		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		assert.NoError(t, err)
		assert.NotNil(t, cp)

		// Create mock listener
		listener := &mockCredentialsListener{
			LastTokenCh: make(chan string),
			LastErrCh:   make(chan error),
		}

		// Subscribe listener
		credentials, cancel, err := cp.Subscribe(listener)
		assert.NoError(t, err)
		assert.NotNil(t, credentials)
		assert.NotNil(t, cancel)

		// Wait for initial token
		tk, err := listener.readWithTimeout(time.Second)
		assert.NoError(t, err)
		assert.Equal(t, "test-token", tk)

		// Unsubscribe
		err = cancel()
		assert.NoError(t, err)
	})

	t.Run("Multiple Listeners", func(t *testing.T) {
		t.Parallel()

		// Create mock token manager
		tm := &fakeTokenManager{
			token: token.New(
				"test",
				"test",
				"test-token",
				time.Now().Add(time.Hour),
				time.Now(),
				time.Hour.Milliseconds(),
			),
		}

		// Create credentials provider
		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		assert.NoError(t, err)
		assert.NotNil(t, cp)

		// Create multiple mock listeners
		listener1 := &mockCredentialsListener{
			LastTokenCh: make(chan string),
			LastErrCh:   make(chan error),
		}
		listener2 := &mockCredentialsListener{
			LastTokenCh: make(chan string),
			LastErrCh:   make(chan error),
		}

		// Subscribe first listener
		credentials1, cancel1, err := cp.Subscribe(listener1)
		assert.NoError(t, err)
		assert.NotNil(t, credentials1)
		assert.NotNil(t, cancel1)

		// Subscribe second listener
		credentials2, cancel2, err := cp.Subscribe(listener2)
		assert.NoError(t, err)
		assert.NotNil(t, credentials2)
		assert.NotNil(t, cancel2)

		// Wait for initial tokens
		token1, err := listener1.readWithTimeout(time.Second)
		assert.NoError(t, err)
		assert.Equal(t, "test-token", token1)

		token2, err := listener2.readWithTimeout(time.Second)
		assert.NoError(t, err)
		assert.Equal(t, "test-token", token2)

		// Unsubscribe first listener
		err = cancel1()
		assert.NoError(t, err)

		// Unsubscribe second listener
		err = cancel2()
		assert.NoError(t, err)
	})

	t.Run("Token Updates", func(t *testing.T) {
		t.Parallel()

		// Create mock token manager
		tm := &fakeTokenManager{
			token: token.New(
				"test",
				"test",
				"initial-token",
				time.Now().Add(time.Hour),
				time.Now(),
				time.Hour.Milliseconds(),
			),
		}

		// Create credentials provider
		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		assert.NoError(t, err)
		assert.NotNil(t, cp)

		// Create mock listener
		listener := &mockCredentialsListener{
			LastTokenCh: make(chan string),
			LastErrCh:   make(chan error),
		}

		// Subscribe listener
		credentials, cancel, err := cp.Subscribe(listener)
		assert.NoError(t, err)
		assert.NotNil(t, credentials)
		assert.NotNil(t, cancel)

		// Wait for initial token
		tk, err := listener.readWithTimeout(time.Second)
		assert.NoError(t, err)
		assert.Equal(t, "initial-token", tk)

		tm.lock.Lock()
		// Update token
		tm.token = token.New(
			"test",
			"test",
			"updated-token",
			time.Now().Add(time.Hour),
			time.Now(),
			time.Hour.Milliseconds(),
		)
		tm.lock.Unlock()

		// Wait for token update
		tk, err = listener.readWithTimeout(time.Second)
		assert.NoError(t, err)
		assert.Equal(t, "updated-token", tk)

		// Unsubscribe
		err = cancel()
		assert.NoError(t, err)
	})

	t.Run("Error Handling", func(t *testing.T) {
		t.Parallel()

		// Create mock token manager with error
		tm := &fakeTokenManager{
			err: assert.AnError,
		}

		// Create credentials provider
		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		assert.Error(t, err)
		assert.Nil(t, cp)
	})
}

func TestCredentialsProviderOptions(t *testing.T) {
	t.Run("default token manager factory", func(t *testing.T) {
		options := CredentialsProviderOptions{}
		factory := options.getTokenManagerFactory()
		assert.NotNil(t, factory)
	})

	t.Run("custom token manager factory", func(t *testing.T) {
		m := &fakeTokenManager{}
		customFactory := func(shared.IdentityProvider, manager.TokenManagerOptions) (manager.TokenManager, error) {
			return m, nil
		}
		options := CredentialsProviderOptions{
			tokenManagerFactory: customFactory,
		}
		tm, err := options.getTokenManagerFactory()(nil, manager.TokenManagerOptions{})
		assert.NotNil(t, tm)
		assert.NoError(t, err)
		assert.Equal(t, m, tm)
	})
}

func TestCredentialsProviderSubscribe(t *testing.T) {
	// Create a test provider
	opts := ConfidentialCredentialsProviderOptions{
		CredentialsProviderOptions: CredentialsProviderOptions{
			TokenManagerOptions: manager.TokenManagerOptions{
				ExpirationRefreshRatio: 0.7,
			},
		},
		ConfidentialIdentityProviderOptions: identity.ConfidentialIdentityProviderOptions{
			ClientID:        "test-client-id",
			CredentialsType: identity.ClientSecretCredentialType,
			ClientSecret:    "test-secret",
			Scopes:          []string{identity.RedisScopeDefault},
			Authority:       identity.AuthorityConfiguration{},
		},
	}
	t.Run("double subscribe and cancel resubscribe", func(t *testing.T) {
		t.Parallel()
		testToken := token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(tokenExpiration),
			time.Now(),
			tokenExpiration.Milliseconds(),
		)

		listener := &mockCredentialsListener{
			LastTokenCh: make(chan string, 1),
			LastErrCh:   make(chan error, 1),
		}
		mtm := &mockTokenManager{done: make(chan struct{}), lock: &sync.Mutex{}}
		// Set the token manager factory in the options
		options := opts
		options.tokenManagerFactory = mockTokenManagerFactory(mtm)
		mtm.On("GetToken", false).Return(testToken, nil)
		mtm.On("Start", mock.Anything).
			Run(mockTokenManagerLoop(mtm, tokenExpiration, testToken, nil)).
			Return(manager.StopFunc(mtm.stop), nil)
		provider, err := NewConfidentialCredentialsProvider(options)
		require.NoError(t, err)
		require.NotNil(t, provider)
		// Subscribe the listener
		tk, cancel, err := provider.Subscribe(listener)
		require.NoError(t, err)
		require.NotNil(t, tk)
		require.NotNil(t, cancel)
		// try to subscribe the same listener again
		tk2, cancel2, err := provider.Subscribe(listener)
		require.NoError(t, err)
		require.NotNil(t, tk2)
		require.NotNil(t, cancel2)
		// Verify the listener received the token once
		select {
		case tk := <-listener.LastTokenCh:
			assert.Equal(t, rawTokenString, tk, "listener received wrong token")
		case err := <-listener.LastErrCh:
			t.Fatalf("listener received error: %v", err)
		case <-time.After(3 * tokenExpiration):
			t.Fatalf("listener timed out waiting for token")
		}
		// verify it is not received again
		select {
		case tk := <-listener.LastTokenCh:
			t.Fatalf("listener received unexpected token: %v", tk)
		case err := <-listener.LastErrCh:
			t.Fatalf("listener received unexpected error: %v", err)
		case <-time.After(tokenExpiration / 2):
			// No message received, which is expected
		}

	})

	t.Run("concurrent subscribe and cancel with error ", func(t *testing.T) {
		t.Parallel()
		testToken := token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(tokenExpiration),
			time.Now(),
			tokenExpiration.Milliseconds(),
		)
		mtm := &mockTokenManager{done: make(chan struct{}), lock: &sync.Mutex{}}
		// Set the token manager factory in the options
		options := opts
		options.tokenManagerFactory = mockTokenManagerFactory(mtm)
		mtm.On("GetToken", false).Return(testToken, nil)

		mtm.On("Start", mock.Anything).
			Run(mockTokenManagerLoop(mtm, tokenExpiration, nil, errTokenError)).
			Return(manager.StopFunc(mtm.stop), nil)
		provider, err := NewConfidentialCredentialsProvider(options)
		require.NoError(t, err)
		require.NotNil(t, provider)
		var wg sync.WaitGroup
		listeners := make([]*mockCredentialsListener, numListeners)
		cancels := make([]auth.UnsubscribeFunc, numListeners)

		// Subscribe multiple listeners concurrently
		for i := 0; i < numListeners; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				listener := &mockCredentialsListener{
					LastTokenCh: make(chan string, 1),
					LastErrCh:   make(chan error, 1),
				}
				listeners[idx] = listener
				_, cancel, err := provider.Subscribe(listener)
				require.NoError(t, err)
				cancels[idx] = cancel
			}(i)
		}
		wg.Wait()

		// Verify all listeners received the token
		for i, listener := range listeners {
			select {
			case tk := <-listener.LastTokenCh:
				t.Fatalf("listener %d received token: %v", i, tk)
			case err := <-listener.LastErrCh:
				assert.Equal(t, errTokenError.Error(), err.Error(), "listener %d received wrong error", i)
			case <-time.After(3 * tokenExpiration):
				t.Fatalf("listener %d timed out waiting for token", i)
			}
		}

		// Cancel all subscriptions concurrently
		for i := 0; i < numListeners; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				err := cancels[idx]()
				require.NoError(t, err)
			}(i)
		}
		wg.Wait()

		// Verify no more tokens are sent after cancellation
		for i, listener := range listeners {
			select {
			case tk := <-listener.LastTokenCh:
				t.Fatalf("listener %d received unexpected token after cancellation: %s", i, tk)
			case err := <-listener.LastErrCh:
				t.Fatalf("listener %d received unexpected error after cancellation: %v", i, err)
			case <-time.After(3 * tokenExpiration):
				// No message received, which is expected
			}
		}
	})

	t.Run("concurrent subscribe and get token error ", func(t *testing.T) {
		t.Parallel()
		mtm := &mockTokenManager{done: make(chan struct{}), lock: &sync.Mutex{}}
		// Set the token manager factory in the options
		options := opts
		options.tokenManagerFactory = mockTokenManagerFactory(mtm)
		mtm.On("GetToken", false).Return(nil, assert.AnError)

		mtm.On("Start", mock.Anything).
			Run(mockTokenManagerLoop(mtm, tokenExpiration, nil, errTokenError)).
			Return(manager.StopFunc(mtm.stop), nil)
		provider, err := NewConfidentialCredentialsProvider(options)
		require.NoError(t, err)
		require.NotNil(t, provider)

		var wg sync.WaitGroup
		listeners := make([]*mockCredentialsListener, numListeners)

		// Subscribe multiple listeners concurrently
		for i := 0; i < numListeners; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				listener := &mockCredentialsListener{
					LastTokenCh: make(chan string, 1),
					LastErrCh:   make(chan error, 1),
				}
				listeners[idx] = listener
				tk, cancel, err := provider.Subscribe(listener)
				require.Nil(t, tk)
				require.Error(t, err)
				require.Nil(t, cancel)
			}(i)
		}
		wg.Wait()

		// Verify no more tokens are sent after cancellation
		for i, listener := range listeners {
			select {
			case tk := <-listener.LastTokenCh:
				t.Fatalf("listener %d received unexpected token after cancellation: %s", i, tk)
			case err := <-listener.LastErrCh:
				t.Fatalf("listener %d received unexpected error after cancellation: %v", i, err)
			case <-time.After(3 * tokenExpiration):
				// No message received, which is expected
			}
		}
	})

	t.Run("concurrent subscribe and cancel", func(t *testing.T) {
		t.Parallel()
		testToken := token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(tokenExpiration),
			time.Now(),
			tokenExpiration.Milliseconds(),
		)
		// Set the token manager factory in the options
		options := opts
		options.tokenManagerFactory = testFakeTokenManagerFactory(testToken, nil)

		provider, err := NewConfidentialCredentialsProvider(options)
		require.NoError(t, err)
		require.NotNil(t, provider)
		var wg sync.WaitGroup
		listeners := make([]*mockCredentialsListener, numListeners)
		cancels := make([]auth.UnsubscribeFunc, numListeners)

		// Subscribe multiple listeners concurrently
		for i := 0; i < numListeners; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				listener := &mockCredentialsListener{
					LastTokenCh: make(chan string, 1),
					LastErrCh:   make(chan error, 1),
				}
				listeners[idx] = listener
				_, cancel, err := provider.Subscribe(listener)
				require.NoError(t, err)
				cancels[idx] = cancel
			}(i)
		}
		wg.Wait()

		// Verify all listeners received the token
		for i, listener := range listeners {
			select {
			case tk := <-listener.LastTokenCh:
				assert.Equal(t, rawTokenString, tk, "listener %d received wrong token", i)
			case err := <-listener.LastErrCh:
				t.Fatalf("listener %d received error: %v", i, err)
			case <-time.After(3 * tokenExpiration):
				t.Fatalf("listener %d timed out waiting for token", i)
			}
		}

		// Cancel all subscriptions concurrently
		for i := 0; i < numListeners; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				err := cancels[idx]()
				require.NoError(t, err)
			}(i)
		}
		wg.Wait()

		// Verify no more tokens are sent after cancellation
		for i, listener := range listeners {
			select {
			case tk := <-listener.LastTokenCh:
				t.Fatalf("listener %d received unexpected token after cancellation: %s", i, tk)
			case err := <-listener.LastErrCh:
				t.Fatalf("listener %d received unexpected error after cancellation: %v", i, err)
			case <-time.After(3 * tokenExpiration):
				// No message received, which is expected
			}
		}
	})
}

// TestCredentialsProviderDeadlockScenario tests the deadlock scenario described in the bug report.
//
// Bug Description:
// A deadlock occurs in StreamingCredentialsProvider when listener callbacks (OnNext/OnError)
// are invoked while holding RLock. If re-auth fails, go-redis may close the connection and
// trigger the provider's unsubscribe, which then tries to acquire Lock on the same RWMutex.
// Since RWMutex doesn't allow upgrading a read lock to a write lock, this leads to a deadlock.
//
// Reproduction Steps:
// 1. Provider receives a new token and calls onTokenNext
// 2. onTokenNext acquires RLock and invokes listener.OnNext(t)
// 3. ReAuthCredentialsListener.OnNext calls re-auth; on error it triggers onAuthenticationErr
// 4. onAuthenticationErr closes the connection (e.g. bad conn)
// 5. Conn.Close() triggers the provider's unsubscribe
// 6. unsubscribe tries to acquire Lock, while RLock is still held
// 7. Deadlock occurs
//
// Expected Behavior:
// - These tests should FAIL when the deadlock bug is present (current state)
// - These tests should PASS when the deadlock bug is fixed
//
// This test reproduces the deadlock by creating a listener that calls unsubscribe
// during the OnNext callback, simulating the real-world scenario.
func TestCredentialsProviderDeadlockScenario(t *testing.T) {
	t.Run("deadlock on unsubscribe during OnNext", func(t *testing.T) {
		// Create a test token
		testToken := token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(time.Hour),
			time.Now(),
			time.Hour.Milliseconds(),
		)

		// Create credentials provider with mock token manager
		tm := &fakeTokenManager{
			token: testToken,
		}

		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		require.NoError(t, err)
		require.NotNil(t, cp)

		// Create a deadlock-inducing listener that calls unsubscribe during OnNext
		deadlockListener := &deadlockInducingListener{
			provider:    cp.(*entraidCredentialsProvider),
			unsubscribe: nil, // Will be set after subscription
		}

		// Subscribe the deadlock listener
		credentials, cancel, err := cp.Subscribe(deadlockListener)
		require.NoError(t, err)
		require.NotNil(t, credentials)
		require.NotNil(t, cancel)

		// Set the unsubscribe function in the listener
		deadlockListener.unsubscribe = cancel

		// Use a timeout to detect deadlock
		done := make(chan bool, 1)
		timeout := time.After(5 * time.Second)

		go func() {
			// Trigger token update which should cause deadlock
			cp.(*entraidCredentialsProvider).onTokenNext(testToken)
			done <- true
		}()

		select {
		case <-done:
			// Test passes - no deadlock occurred (this means the bug is fixed)
			t.Log("No deadlock detected - operation completed successfully")
		case <-timeout:
			// Test fails - deadlock occurred (this means the bug is present)
			t.Fatal("Deadlock detected: operation timed out due to RWMutex deadlock in onTokenNext")
		}
	})

	t.Run("concurrent token update and unsubscribe stress test", func(t *testing.T) {
		// This test verifies that concurrent token updates and unsubscribes
		// can cause deadlocks under stress conditions
		testToken := token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(time.Hour),
			time.Now(),
			time.Hour.Milliseconds(),
		)

		tm := &fakeTokenManager{
			token: testToken,
		}

		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		require.NoError(t, err)
		require.NotNil(t, cp)

		provider := cp.(*entraidCredentialsProvider)

		// Create multiple listeners that will trigger unsubscribe during OnNext
		numListeners := 10
		listeners := make([]*deadlockInducingListener, numListeners)
		cancels := make([]auth.UnsubscribeFunc, numListeners)

		// Subscribe all listeners
		for i := 0; i < numListeners; i++ {
			listener := &deadlockInducingListener{
				provider:    provider,
				unsubscribe: nil,
			}
			listeners[i] = listener

			_, cancel, err := cp.Subscribe(listener)
			require.NoError(t, err)
			cancels[i] = cancel
			listener.unsubscribe = cancel
		}

		// Use a timeout to detect deadlock
		done := make(chan bool, 1)
		timeout := time.After(10 * time.Second)

		go func() {
			// Trigger multiple concurrent token updates
			var wg sync.WaitGroup
			for i := 0; i < 5; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					provider.onTokenNext(testToken)
				}()
			}
			wg.Wait()
			done <- true
		}()

		select {
		case <-done:
			// Test passes - no deadlock occurred (this means the bug is fixed)
			t.Log("No deadlock detected in stress test - operation completed successfully")
		case <-timeout:
			// Test fails - deadlock occurred (this means the bug is present)
			t.Fatal("Deadlock detected in stress test: operation timed out due to RWMutex deadlock")
		}
	})
}

// deadlockInducingListener is a mock listener that simulates the deadlock scenario
// by calling unsubscribe during OnNext, which mimics what happens when
// ReAuthCredentialsListener fails re-auth and closes the connection
type deadlockInducingListener struct {
	provider    *entraidCredentialsProvider
	unsubscribe auth.UnsubscribeFunc
}

func (d *deadlockInducingListener) OnNext(credentials auth.Credentials) {
	// Simulate the scenario where re-auth fails and connection is closed
	// This triggers unsubscribe while we're still in the OnNext callback
	// which is called while holding RLock
	if d.unsubscribe != nil {
		// This call will try to acquire Lock while RLock is held, causing deadlock
		// We call it directly (not in a goroutine) to reproduce the actual deadlock
		_ = d.unsubscribe()
	}
}

func (d *deadlockInducingListener) OnError(err error) {
	// Simulate the scenario where error handling also triggers unsubscribe
	// This can also cause deadlock if called while holding RLock
	if d.unsubscribe != nil {
		_ = d.unsubscribe()
	}
}

// TestCredentialsProviderDeadlockOnError tests deadlock scenario during error handling
func TestCredentialsProviderDeadlockOnError(t *testing.T) {
	t.Run("deadlock on unsubscribe during OnError", func(t *testing.T) {
		// Create a test token
		testToken := token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(time.Hour),
			time.Now(),
			time.Hour.Milliseconds(),
		)

		// Create credentials provider with mock token manager
		tm := &fakeTokenManager{
			token: testToken,
		}

		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		require.NoError(t, err)
		require.NotNil(t, cp)

		// Create a deadlock-inducing listener that calls unsubscribe during OnError
		deadlockListener := &deadlockInducingListener{
			provider:    cp.(*entraidCredentialsProvider),
			unsubscribe: nil, // Will be set after subscription
		}

		// Subscribe the deadlock listener
		credentials, cancel, err := cp.Subscribe(deadlockListener)
		require.NoError(t, err)
		require.NotNil(t, credentials)
		require.NotNil(t, cancel)

		// Set the unsubscribe function in the listener
		deadlockListener.unsubscribe = cancel

		// Use a timeout to detect deadlock
		done := make(chan bool, 1)
		timeout := time.After(5 * time.Second)

		go func() {
			// Trigger error which should cause deadlock
			testError := errors.New("test authentication error")
			cp.(*entraidCredentialsProvider).onTokenError(testError)
			done <- true
		}()

		select {
		case <-done:
			// Test passes - no deadlock occurred (this means the bug is fixed)
			t.Log("No deadlock detected during error handling - operation completed successfully")
		case <-timeout:
			// Test fails - deadlock occurred (this means the bug is present)
			t.Fatal("Deadlock detected during error handling: operation timed out due to RWMutex deadlock in onTokenError")
		}
	})
}

// TestCredentialsProviderRaceCondition tests for race conditions in concurrent scenarios
func TestCredentialsProviderRaceCondition(t *testing.T) {
	t.Run("race condition between subscribe and token update", func(t *testing.T) {
		testToken := token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(time.Hour),
			time.Now(),
			time.Hour.Milliseconds(),
		)

		tm := &fakeTokenManager{
			token: testToken,
		}

		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		require.NoError(t, err)
		require.NotNil(t, cp)

		provider := cp.(*entraidCredentialsProvider)

		// Run with race detector enabled
		var wg sync.WaitGroup
		numGoroutines := 10

		// Concurrent subscriptions
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				listener := &mockCredentialsListener{
					LastTokenCh: make(chan string, 1),
					LastErrCh:   make(chan error, 1),
				}
				_, cancel, err := cp.Subscribe(listener)
				if err == nil && cancel != nil {
					// Immediately unsubscribe to create more contention
					_ = cancel()
				}
			}(i)
		}

		// Concurrent token updates
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				provider.onTokenNext(testToken)
			}()
		}

		// Wait for all goroutines to complete
		wg.Wait()
	})
}

// TestCredentialsProviderDeadlockFix demonstrates the expected behavior after the deadlock is fixed.
// This test shows how the provider should handle unsubscribe calls during listener callbacks
// without causing deadlocks.
func TestCredentialsProviderDeadlockFix(t *testing.T) {
	// Note: The deadlock bug has been fixed! The fix involves:
	// 1. Copying the listeners slice while holding RLock
	// 2. Releasing RLock before calling listener callbacks
	// 3. This allows unsubscribe operations to acquire Lock without deadlock

	t.Run("no deadlock after fix - unsubscribe during OnNext", func(t *testing.T) {
		// This test would pass after implementing the fix
		// The fix should involve:
		// 1. Not holding RLock while calling listener callbacks, OR
		// 2. Using a different synchronization mechanism that allows safe unsubscribe during callbacks, OR
		// 3. Deferring unsubscribe operations to avoid the deadlock

		testToken := token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(time.Hour),
			time.Now(),
			time.Hour.Milliseconds(),
		)

		tm := &fakeTokenManager{
			token: testToken,
		}

		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		require.NoError(t, err)
		require.NotNil(t, cp)

		// Create a listener that calls unsubscribe during OnNext
		deadlockListener := &deadlockInducingListener{
			provider:    cp.(*entraidCredentialsProvider),
			unsubscribe: nil,
		}

		credentials, cancel, err := cp.Subscribe(deadlockListener)
		require.NoError(t, err)
		require.NotNil(t, credentials)
		require.NotNil(t, cancel)

		deadlockListener.unsubscribe = cancel

		// After the fix, this should complete without deadlock
		done := make(chan bool, 1)
		timeout := time.After(2 * time.Second)

		go func() {
			cp.(*entraidCredentialsProvider).onTokenNext(testToken)
			done <- true
		}()

		select {
		case <-done:
			// Test passes - no deadlock occurred
			t.Log("Success: No deadlock detected after fix")
		case <-timeout:
			t.Fatal("Deadlock still present - fix not working correctly")
		}
	})
}

// TestCredentialsProviderEdgeCases tests additional edge cases related to the deadlock bug
func TestCredentialsProviderEdgeCases(t *testing.T) {
	t.Run("multiple listeners with mixed unsubscribe behavior", func(t *testing.T) {
		testToken := token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(time.Hour),
			time.Now(),
			time.Hour.Milliseconds(),
		)

		tm := &fakeTokenManager{
			token: testToken,
		}

		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		require.NoError(t, err)
		require.NotNil(t, cp)

		// Create a mix of normal listeners and deadlock-inducing listeners
		normalListener := &mockCredentialsListener{
			LastTokenCh: make(chan string, 1),
			LastErrCh:   make(chan error, 1),
		}

		deadlockListener := &deadlockInducingListener{
			provider:    cp.(*entraidCredentialsProvider),
			unsubscribe: nil,
		}

		// Subscribe both listeners
		_, cancel1, err := cp.Subscribe(normalListener)
		require.NoError(t, err)
		defer cancel1()

		_, cancel2, err := cp.Subscribe(deadlockListener)
		require.NoError(t, err)
		deadlockListener.unsubscribe = cancel2

		// This should cause deadlock due to the deadlock-inducing listener
		done := make(chan bool, 1)
		timeout := time.After(3 * time.Second)

		go func() {
			cp.(*entraidCredentialsProvider).onTokenNext(testToken)
			done <- true
		}()

		select {
		case <-done:
			t.Log("No deadlock detected - this indicates the bug might be fixed")
		case <-timeout:
			t.Fatal("Deadlock detected with mixed listener types")
		}
	})

	t.Run("rapid subscribe and unsubscribe operations", func(t *testing.T) {
		testToken := token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(time.Hour),
			time.Now(),
			time.Hour.Milliseconds(),
		)

		tm := &fakeTokenManager{
			token: testToken,
		}

		cp, err := NewCredentialsProvider(tm, CredentialsProviderOptions{})
		require.NoError(t, err)
		require.NotNil(t, cp)

		// Rapidly subscribe and unsubscribe while triggering token updates
		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					listener := &mockCredentialsListener{
						LastTokenCh: make(chan string, 1),
						LastErrCh:   make(chan error, 1),
					}
					_, cancel, err := cp.Subscribe(listener)
					if err == nil && cancel != nil {
						_ = cancel()
					}
				}
			}()
		}

		// Concurrent token updates
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				cp.(*entraidCredentialsProvider).onTokenNext(testToken)
				time.Sleep(time.Millisecond)
			}
		}()

		// Wait for completion with timeout
		done := make(chan bool, 1)
		go func() {
			wg.Wait()
			done <- true
		}()

		select {
		case <-done:
			t.Log("Rapid subscribe/unsubscribe test completed successfully")
		case <-time.After(10 * time.Second):
			t.Fatal("Rapid subscribe/unsubscribe test timed out - possible deadlock")
		}
	})
}
