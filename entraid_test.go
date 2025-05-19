package entraid

import (
	"errors"
	"flag"
	"sync"
	"testing"
	"time"

	"github.com/redis-developer/go-redis-entraid/manager"
	"github.com/redis-developer/go-redis-entraid/shared"
	"github.com/redis-developer/go-redis-entraid/token"
	"github.com/redis/go-redis/v9/auth"
	"github.com/stretchr/testify/mock"
)

// fakeTokenManager implements the TokenManager interface for testing
type fakeTokenManager struct {
	token *token.Token
	err   error
	lock  sync.Mutex
}

const rawTokenString = "mock-token"

// numListeners is set to 3 for short tests and 12 for long tests
var numListeners = 12

// tokenExpiration is set to 100ms for long tests and 10ms for short tests
var tokenExpiration = 100 * time.Millisecond

func init() {
	testing.Init()
	flag.Parse()
	tokenExpiration = 100 * time.Millisecond
	numListeners = 12
	if testing.Short() {
		tokenExpiration = 10 * time.Millisecond
		numListeners = 3
	}
}

func (m *fakeTokenManager) GetToken(forceRefresh bool) (*token.Token, error) {
	if forceRefresh {
		m.token = token.New(
			"test",
			"test",
			rawTokenString,
			time.Now().Add(tokenExpiration),
			time.Now(),
			int64(tokenExpiration.Seconds()),
		)
	}
	return m.token, m.err
}

func (m *fakeTokenManager) Start(listener manager.TokenListener) (manager.StopFunc, error) {
	if m.err != nil {
		return nil, m.err
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-time.After(tokenExpiration):
				m.lock.Lock()
				if m.err != nil {
					listener.OnError(m.err)
					return
				}
				listener.OnNext(m.token)
				m.lock.Unlock()
			case <-done:
				// Exit the loop if done channel is closed
				return

			}
		}
	}()

	return func() error {
		close(done)
		return nil
	}, nil
}

func (m *fakeTokenManager) stop() error {
	return nil
}

// mockCredentialsListener implements the CredentialsListener interface for testing
type mockCredentialsListener struct {
	LastTokenCh chan string
	LastErrCh   chan error
}

func (m *mockCredentialsListener) readWithTimeout(timeout time.Duration) (string, error) {
	select {
	case tk := <-m.LastTokenCh:
		return tk, nil
	case err := <-m.LastErrCh:
		return "", err
	case <-time.After(timeout):
		return "", errors.New("timeout waiting for token")
	}
}

func (m *mockCredentialsListener) OnNext(credentials auth.Credentials) {
	if m.LastTokenCh == nil {
		m.LastTokenCh = make(chan string)
	}
	m.LastTokenCh <- credentials.RawCredentials()
}

func (m *mockCredentialsListener) OnError(err error) {
	if m.LastErrCh == nil {
		m.LastErrCh = make(chan error)
	}
	m.LastErrCh <- err
}

// testFakeTokenManagerFactory is a factory function that returns a mock token manager
func testFakeTokenManagerFactory(tk *token.Token, err error) func(shared.IdentityProvider, manager.TokenManagerOptions) (manager.TokenManager, error) {
	return func(provider shared.IdentityProvider, options manager.TokenManagerOptions) (manager.TokenManager, error) {
		return &fakeTokenManager{
			token: tk,
			err:   err,
		}, nil
	}
}

// mockTokenManager is a mock implementation of the TokenManager interface
type mockTokenManager struct {
	mock.Mock
	idp      shared.IdentityProvider
	done     chan struct{}
	options  manager.TokenManagerOptions
	listener manager.TokenListener
	lock     sync.Mutex
}

func (m *mockTokenManager) GetToken(forceRefresh bool) (*token.Token, error) {
	args := m.Called(forceRefresh)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*token.Token), args.Error(1)
}

func (m *mockTokenManager) Start(listener manager.TokenListener) (manager.StopFunc, error) {
	args := m.Called(listener)
	m.lock.Lock()
	if m.done == nil {
		m.done = make(chan struct{})
	}
	if m.listener != nil {
		defer m.lock.Unlock()
		return nil, manager.ErrTokenManagerAlreadyStarted
	}
	if m.listener == nil {
		m.listener = listener
	}
	m.lock.Unlock()
	return args.Get(0).(manager.StopFunc), args.Error(1)
}
func (m *mockTokenManager) stop() error {
	m.lock.Lock()
	defer m.lock.Unlock()
	if m.listener == nil {
		return manager.ErrTokenManagerAlreadyStopped
	}
	if m.listener != nil {
		m.listener = nil
	}
	if m.done != nil {
		close(m.done)
		m.done = nil
	}
	return nil
}

// mockTokenManagerFactory is a factory function that returns a mock token manager
func mockTokenManagerFactory(mtm *mockTokenManager) func(shared.IdentityProvider, manager.TokenManagerOptions) (manager.TokenManager, error) {
	return func(provider shared.IdentityProvider, options manager.TokenManagerOptions) (manager.TokenManager, error) {
		mtm.idp = provider
		mtm.options = options
		return mtm, nil
	}
}

var errTokenError = errors.New("token error")

func mockTokenManagerLoop(mtm *mockTokenManager, tokenExpiration time.Duration, testToken *token.Token, err error) func(args mock.Arguments) {
	return func(args mock.Arguments) {
		go func() {
			for {
				select {
				case <-mtm.done:
					return
				case <-time.After(tokenExpiration):
					mtm.lock.Lock()
					if err != nil {
						mtm.listener.OnError(err)
					} else {
						mtm.listener.OnNext(testToken)
					}
					mtm.lock.Unlock()
				}
			}
		}()
	}
}
