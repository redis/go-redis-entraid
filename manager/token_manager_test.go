package manager

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	"github.com/redis-developer/go-redis-entraid/shared"
	"github.com/redis-developer/go-redis-entraid/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var assertFuncNameMatches = func(t *testing.T, func1, func2 interface{}) {
	funcName1 := runtime.FuncForPC(reflect.ValueOf(func1).Pointer()).Name()
	funcName2 := runtime.FuncForPC(reflect.ValueOf(func2).Pointer()).Name()
	assert.Equal(t, funcName1, funcName2)
}

func TestTokenManager(t *testing.T) {
	t.Parallel()
	t.Run("Without IDP", func(t *testing.T) {
		t.Parallel()
		tokenManager, err := NewTokenManager(nil,
			TokenManagerOptions{},
		)
		assert.Error(t, err)
		assert.Nil(t, tokenManager)
	})

	t.Run("With IDP", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
	})
}

func TestTokenManagerWithOptions(t *testing.T) {
	t.Parallel()
	t.Run("Bad Expiration Refresh Ration", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		options := TokenManagerOptions{
			ExpirationRefreshRatio: 5,
		}
		tokenManager, err := NewTokenManager(idp, options)
		assert.Error(t, err)
		assert.Nil(t, tokenManager)
	})
	t.Run("With IDP and Options", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		options := TokenManagerOptions{
			ExpirationRefreshRatio: 0.5,
		}
		tokenManager, err := NewTokenManager(idp, options)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Equal(t, 0.5, tm.expirationRefreshRatio)
	})
	t.Run("Default Options", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		options := TokenManagerOptions{}
		tokenManager, err := NewTokenManager(idp, options)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Equal(t, DefaultExpirationRefreshRatio, tm.expirationRefreshRatio)
		assert.NotNil(t, tm.retryOptions.IsRetryable)
		assertFuncNameMatches(t, tm.retryOptions.IsRetryable, defaultIsRetryable)
		assert.Equal(t, DefaultRetryOptionsMaxAttempts, tm.retryOptions.MaxAttempts)
		assert.Equal(t, DefaultRetryOptionsInitialDelay, tm.retryOptions.InitialDelay)
		assert.Equal(t, DefaultRetryOptionsMaxDelay, tm.retryOptions.MaxDelay)
		assert.Equal(t, DefaultRetryOptionsBackoffMultiplier, tm.retryOptions.BackoffMultiplier)
	})
}

func TestDefaultIdentityProviderResponseParserOr(t *testing.T) {
	t.Parallel()
	var f shared.IdentityProviderResponseParser = &mockIdentityProviderResponseParser{}

	result := defaultIdentityProviderResponseParserOr(f)
	assert.NotNil(t, result)
	assert.Equal(t, result, f)

	defaultParser := defaultIdentityProviderResponseParserOr(nil)
	assert.NotNil(t, defaultParser)
	assert.NotEqual(t, defaultParser, f)
	assert.Equal(t, entraidIdentityProviderResponseParser, defaultParser)
}

func TestDefaultIsRetryable(t *testing.T) {
	t.Parallel()
	// with network error timeout
	t.Run("Non-Retryable Error", func(t *testing.T) {
		t.Parallel()
		err := &azcore.ResponseError{
			StatusCode: 500,
		}
		is := defaultIsRetryable(err)
		assert.False(t, is)
	})

	t.Run("Nil Error", func(t *testing.T) {
		t.Parallel()
		var err error
		is := defaultIsRetryable(err)
		assert.True(t, is)

		is = defaultIsRetryable(nil)
		assert.True(t, is)
	})

	t.Run("Retryable Error with Timeout", func(t *testing.T) {
		t.Parallel()
		err := newMockError(true)
		result := defaultIsRetryable(err)
		assert.True(t, result)
	})
	t.Run("Retryable Error with Temporary", func(t *testing.T) {
		t.Parallel()
		err := newMockError(true)
		result := defaultIsRetryable(err)
		assert.True(t, result)
	})

	t.Run("Retryable Error with err parent of os.ErrDeadlineExceeded", func(t *testing.T) {
		t.Parallel()
		err := fmt.Errorf("timeout: %w", os.ErrDeadlineExceeded)
		res := defaultIsRetryable(err)
		assert.True(t, res)
	})
}

func TestTokenManager_Close(t *testing.T) {
	t.Parallel()
	t.Run("Close", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		mParser := &mockIdentityProviderResponseParser{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				IdentityProviderResponseParser: mParser,
			},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)
		assert.NotPanics(t, func() {
			err = tokenManager.Stop()
			assert.Error(t, err)
		})
		rawResponse, err := shared.NewIDPResponse(shared.ResponseTypeRawToken, "test")
		assert.NoError(t, err)

		idp.On("RequestToken", mock.Anything).Return(rawResponse, nil)
		mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
		listener.On("OnNext", testTokenValid).Return()

		assert.NotPanics(t, func() {
			cancel, err := tokenManager.Start(listener)
			assert.NotNil(t, cancel)
			assert.NoError(t, err)
		})
		assert.NotNil(t, tm.listener)

		err = tokenManager.Stop()
		assert.Nil(t, tm.listener)
		assert.NoError(t, err)

		assert.NotPanics(t, func() {
			err = tokenManager.Stop()
			assert.Error(t, err)
		})
	})

	t.Run("Close with Cancel", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		mParser := &mockIdentityProviderResponseParser{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				IdentityProviderResponseParser: mParser,
			},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		rawResponse, err := shared.NewIDPResponse(shared.ResponseTypeRawToken, "test")
		assert.NoError(t, err)

		idp.On("RequestToken", mock.Anything).Return(rawResponse, nil)
		mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
		listener.On("OnNext", testTokenValid).Return()

		assert.NotPanics(t, func() {
			cancel, err := tokenManager.Start(listener)
			assert.NotNil(t, cancel)
			assert.NoError(t, err)
			assert.NotNil(t, tm.listener)
			err = cancel()
			assert.NoError(t, err)
			assert.Nil(t, tm.listener)
			err = cancel()
			assert.Error(t, err)
			assert.Nil(t, tm.listener)
		})
	})
	t.Run("Close in multiple threads", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		mParser := &mockIdentityProviderResponseParser{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				IdentityProviderResponseParser: mParser,
			},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		rawResponse, err := shared.NewIDPResponse(shared.ResponseTypeRawToken, "test")
		assert.NoError(t, err)

		idp.On("RequestToken", mock.Anything).Return(rawResponse, nil)
		mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
		listener.On("OnNext", testTokenValid).Return()

		assert.NotPanics(t, func() {
			cancel, err := tokenManager.Start(listener)
			assert.NotNil(t, cancel)
			assert.NoError(t, err)
			assert.NotNil(t, tm.listener)
			var hasStopped int
			var alreadyStopped int32
			wg := &sync.WaitGroup{}

			// Start 50000 goroutines to close the token manager
			// and check if the listener is nil after each close.
			numExecutions := 50000
			for i := 0; i < numExecutions; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					time.Sleep(time.Duration(int64(rand.Intn(100)) * int64(time.Millisecond)))
					err := tokenManager.Stop()
					if err == nil {
						hasStopped += 1
						return
					} else {
						atomic.AddInt32(&alreadyStopped, 1)
					}
					assert.Nil(t, tm.listener)
					assert.Error(t, err)
				}()
			}
			wg.Wait()
			assert.Nil(t, tm.listener)
			assert.Equal(t, 1, hasStopped)
			assert.Equal(t, int32(numExecutions-1), atomic.LoadInt32(&alreadyStopped))
		})
	})
}

func TestTokenManager_Start(t *testing.T) {
	t.Parallel()
	t.Run("Start in multiple threads", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		mParser := &mockIdentityProviderResponseParser{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				IdentityProviderResponseParser: mParser,
			},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		rawResponse, err := shared.NewIDPResponse(shared.ResponseTypeRawToken, "test")
		assert.NoError(t, err)

		idp.On("RequestToken", mock.Anything).Return(rawResponse, nil)
		mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
		listener.On("OnNext", testTokenValid).Return()

		assert.NotPanics(t, func() {
			var hasStarted int
			var alreadyStarted int32
			wg := &sync.WaitGroup{}

			numExecutions := 50000
			for i := 0; i < numExecutions; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					time.Sleep(time.Duration(int64(rand.Intn(100)) * int64(time.Millisecond)))
					_, err := tokenManager.Start(listener)
					if err == nil {
						hasStarted += 1
						return
					} else {
						atomic.AddInt32(&alreadyStarted, 1)
					}
					assert.NotNil(t, tm.listener)
					assert.Error(t, err)
				}()
			}
			wg.Wait()
			assert.NotNil(t, tm.listener)
			assert.Equal(t, 1, hasStarted)
			assert.Equal(t, int32(numExecutions-1), atomic.LoadInt32(&alreadyStarted))
			cancel, err := tokenManager.Start(listener)
			assert.Nil(t, cancel)
			assert.Error(t, err)
			assert.NotNil(t, tm.listener)
		})
	})

	t.Run("concurrent stress token manager", func(t *testing.T) {
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		mParser := &mockIdentityProviderResponseParser{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				IdentityProviderResponseParser: mParser,
			},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		rawResponse, err := shared.NewIDPResponse(shared.ResponseTypeRawToken, "test")
		assert.NoError(t, err)

		assert.NotPanics(t, func() {
			last := &atomic.Int32{}
			wg := &sync.WaitGroup{}

			idp.On("RequestToken", mock.Anything).Return(rawResponse, nil)
			mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
			listener.On("OnNext", testTokenValid).Return()
			numExecutions := int32(50000)
			for i := int32(0); i < numExecutions; i++ {
				wg.Add(1)
				go func(num int32) {
					defer wg.Done()
					var err error
					time.Sleep(time.Duration(int64(rand.Intn(1000)+(300-int(num)/2)) * int64(time.Millisecond)))
					last.Store(num)
					if num%2 == 0 {
						err = tokenManager.Stop()
					} else {
						l := &mockTokenListener{Id: num}
						l.On("OnNext", testTokenValid).Return()
						_, err = tokenManager.Start(l)
					}
					if err != nil {
						if err != ErrTokenManagerAlreadyStopped && err != ErrTokenManagerAlreadyStarted {
							// this is un unexpected error, fail the test
							assert.Error(t, err)
						}
					}
				}(i)
			}
			wg.Wait()
			lastExecution := last.Load()
			if lastExecution%2 == 0 {
				if tm.listener != nil {
					l := tm.listener.(*mockTokenListener)
					log.Printf("FAILING WITH lastExecution [STARTED]:[LISTENER:%d]: %d", l.Id, lastExecution)
				}
				assert.Nil(t, tm.listener)
			} else {
				if tm.listener == nil {
					log.Printf("FAILING WITH lastExecution[STOPPED]: %d", lastExecution)
				}
				assert.NotNil(t, tm.listener)
				cancel, err := tokenManager.Start(listener)
				assert.Nil(t, cancel)
				assert.Error(t, err)
				// Close the token manager
				err = tokenManager.Stop()
				assert.Nil(t, err)
			}
			assert.Nil(t, tm.listener)
		})
	})
}

func TestDefaultIdentityProviderResponseParser(t *testing.T) {
	t.Parallel()
	parser := &defaultIdentityProviderResponseParser{}
	t.Run("Default IdentityProviderResponseParser with type AuthResult", func(t *testing.T) {
		t.Parallel()
		authResultVal := testAuthResult(time.Now().Add(time.Hour).UTC())
		idpResponse := &authResult{
			ResultType:    shared.ResponseTypeAuthResult,
			AuthResultVal: authResultVal,
		}
		token1, err := parser.ParseResponse(idpResponse)
		assert.NoError(t, err)
		assert.NotNil(t, token1)
		assert.Equal(t, authResultVal.ExpiresOn, token1.ExpirationOn())
	})
	t.Run("Default IdentityProviderResponseParser with type AccessToken", func(t *testing.T) {
		t.Parallel()
		accessToken := &azcore.AccessToken{
			Token:     testJWTToken,
			ExpiresOn: time.Now().Add(time.Hour).UTC(),
		}
		idpResponse := &authResult{
			ResultType:     shared.ResponseTypeAccessToken,
			AccessTokenVal: accessToken,
		}
		token1, err := parser.ParseResponse(idpResponse)
		assert.NoError(t, err)
		assert.NotNil(t, token1)
		assert.Equal(t, accessToken.ExpiresOn, token1.ExpirationOn())
		assert.Equal(t, accessToken.Token, token1.RawCredentials())
	})
	t.Run("Default IdentityProviderResponseParser with type RawToken", func(t *testing.T) {
		t.Parallel()
		idpResponse := &authResult{
			ResultType:  shared.ResponseTypeRawToken,
			RawTokenVal: testJWTToken,
		}
		token1, err := parser.ParseResponse(idpResponse)
		assert.NoError(t, err)
		assert.NotNil(t, token1)
	})

	t.Run("Default IdentityProviderResponseParser with expired JWT Token", func(t *testing.T) {
		t.Parallel()
		idpResponse := &authResult{
			ResultType:  shared.ResponseTypeRawToken,
			RawTokenVal: testJWTExpiredToken,
		}
		token1, err := parser.ParseResponse(idpResponse)
		assert.Error(t, err)
		assert.Nil(t, token1)
	})

	t.Run("Default IdentityProviderResponseParser with zero expiry JWT Token", func(t *testing.T) {
		t.Parallel()
		idpResponse := &authResult{
			ResultType:  shared.ResponseTypeRawToken,
			RawTokenVal: testJWTWithZeroExpiryToken,
		}
		token1, err := parser.ParseResponse(idpResponse)
		assert.Error(t, err)
		assert.Nil(t, token1)
	})

	t.Run("Default IdentityProviderResponseParser with type Unknown", func(t *testing.T) {
		t.Parallel()
		idpResponse := &authResult{
			ResultType: "Unknown",
		}
		token1, err := parser.ParseResponse(idpResponse)
		assert.Error(t, err)
		assert.Nil(t, token1)
	})

	types := []string{
		shared.ResponseTypeAuthResult,
		shared.ResponseTypeAccessToken,
		shared.ResponseTypeRawToken,
	}
	for _, rt := range types {
		t.Run(fmt.Sprintf("Default IdentityProviderResponseParser with response type %s and nil value", rt), func(t *testing.T) {
			idpResponse := &authResult{
				ResultType: rt,
			}
			token1, err := parser.ParseResponse(idpResponse)
			assert.Error(t, err)
			assert.Nil(t, token1)
		})
	}

	t.Run("Default IdentityProviderResponseParser with response nil", func(t *testing.T) {
		t.Parallel()
		token1, err := parser.ParseResponse(nil)
		assert.Error(t, err)
		assert.Nil(t, token1)
	})
	t.Run("Default IdentityProviderResponseParser with expired token", func(t *testing.T) {
		t.Parallel()
		authResultVal := testAuthResult(time.Now().Add(-time.Hour))
		idpResponse := &authResult{
			ResultType:    shared.ResponseTypeAuthResult,
			AuthResultVal: authResultVal,
		}
		token1, err := parser.ParseResponse(idpResponse)
		assert.Error(t, err)
		assert.Nil(t, token1)
	})
}

func TestEntraidTokenManager_GetToken(t *testing.T) {
	t.Parallel()
	t.Run("GetToken", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		mParser := &mockIdentityProviderResponseParser{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				IdentityProviderResponseParser: mParser,
			},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		rawResponse := &authResult{
			ResultType:  shared.ResponseTypeRawToken,
			RawTokenVal: "test",
		}

		idp.On("RequestToken", mock.Anything).Return(rawResponse, nil)
		mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
		listener.On("OnNext", testTokenValid).Return()

		cancel, err := tokenManager.Start(listener)
		assert.NotNil(t, cancel)
		assert.NoError(t, err)
		assert.NotNil(t, tm.listener)

		token1, err := tokenManager.GetToken(false)
		assert.NoError(t, err)
		assert.NotNil(t, token1)
	})

	t.Run("GetToken with parse error", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		mParser := &mockIdentityProviderResponseParser{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				IdentityProviderResponseParser: mParser,
			},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		rawResponse := &authResult{
			ResultType:  shared.ResponseTypeRawToken,
			RawTokenVal: "test",
		}

		idp.On("RequestToken", mock.Anything).Return(rawResponse, nil)
		mParser.On("ParseResponse", rawResponse).Return(nil, fmt.Errorf("parse error"))
		listener.On("OnError", mock.Anything).Return()

		cancel, err := tokenManager.Start(listener)
		assert.Error(t, err)
		assert.Nil(t, cancel)
		assert.Nil(t, tm.listener)
	})
	t.Run("GetToken with expired token", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{},
		)
		assert.NoError(t, err)

		authResultVal := testAuthResult(time.Now().Add(-time.Hour))
		idpResponse := &authResult{
			ResultType:    shared.ResponseTypeAuthResult,
			AuthResultVal: authResultVal,
		}
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		idp.On("RequestToken", mock.Anything).Return(idpResponse, nil)

		token1, err := tokenManager.GetToken(false)
		assert.Error(t, err)
		assert.Nil(t, token1)
	})

	t.Run("GetToken with nil token", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		_, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)

		rawResponse, err := shared.NewIDPResponse(shared.ResponseTypeRawToken, "test")
		assert.NoError(t, err)

		idp.On("RequestToken", mock.Anything).Return(rawResponse, nil)

		token1, err := tokenManager.GetToken(false)
		assert.Error(t, err)
		assert.Nil(t, token1)
	})

	t.Run("GetToken with nil from parser", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		mParser := &mockIdentityProviderResponseParser{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				IdentityProviderResponseParser: mParser,
			},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		_, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)

		idpResponse, err := shared.NewIDPResponse(shared.ResponseTypeRawToken, "test")
		assert.NoError(t, err)
		idp.On("RequestToken", mock.Anything).Return(idpResponse, nil)
		mParser.On("ParseResponse", idpResponse).Return(nil, nil)

		token1, err := tokenManager.GetToken(false)
		assert.Error(t, err)
		assert.Nil(t, token1)
	})

	t.Run("GetToken with idp error", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		mParser := &mockIdentityProviderResponseParser{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				IdentityProviderResponseParser: mParser,
			},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		_, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)

		idp.On("RequestToken", mock.Anything).Return(nil, fmt.Errorf("idp error"))

		token1, err := tokenManager.GetToken(false)
		assert.Error(t, err)
		assert.Nil(t, token1)
	})
}

func TestEntraidTokenManager_durationToRenewal(t *testing.T) {
	t.Parallel()
	t.Run("durationToRenewal", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		tokenManager, err := NewTokenManager(idp, TokenManagerOptions{
			LowerRefreshBound: time.Hour,
		})
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)

		result := tm.durationToRenewal()
		// returns 0 for nil token
		assert.Equal(t, time.Duration(0), result)

		// get token that expires before the lower bound
		assert.NotPanics(t, func() {
			expiresSoon := testAuthResult(time.Now().Add(tm.lowerBoundDuration - time.Minute).UTC())
			idpResponse, err := shared.NewIDPResponse(shared.ResponseTypeAuthResult,
				expiresSoon)
			assert.NoError(t, err)
			idp.On("RequestToken", mock.Anything).Return(idpResponse, nil).Once()
			tm.token = nil
			_, err = tm.GetToken(false)
			assert.NoError(t, err)
			assert.NotNil(t, tm.token)

			// return zero, should happen now since it expires before the lower bound
			result = tm.durationToRenewal()
			assert.Equal(t, time.Duration(0), result)
		})

		// get token that expires after the lower bound and expirationRefreshRatio to 1
		assert.NotPanics(t, func() {
			tm.expirationRefreshRatio = 1
			expiresAfterlb := testAuthResult(time.Now().Add(tm.lowerBoundDuration + time.Hour).UTC())
			idpResponse, err := shared.NewIDPResponse(shared.ResponseTypeAuthResult,
				expiresAfterlb)
			assert.NoError(t, err)
			idp.On("RequestToken", mock.Anything).Return(idpResponse, nil).Once()
			tm.token = nil
			_, err = tm.GetToken(false)
			assert.NoError(t, err)
			assert.NotNil(t, tm.token)

			// return time to lower bound, if the returned time will be after the lower bound
			result = tm.durationToRenewal()
			assert.InEpsilon(t, time.Until(tm.token.ExpirationOn().Add(-1*tm.lowerBoundDuration)), result, float64(time.Second))
		})

	})
}

func TestEntraidTokenManager_Streaming(t *testing.T) {
	t.Parallel()
	t.Run("Start and Close", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		mParser := &mockIdentityProviderResponseParser{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				IdentityProviderResponseParser: mParser,
			},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		expiresIn := time.Second
		expiresOn := time.Now().Add(expiresIn).UTC()
		authResultVal := testAuthResult(expiresOn)
		idpResponse := &authResult{
			ResultType:    shared.ResponseTypeAuthResult,
			AuthResultVal: authResultVal,
		}

		idp.On("RequestToken", mock.Anything).Return(idpResponse, nil).Once()
		token1 := token.New(
			"test",
			"test",
			"test",
			expiresOn,
			time.Now(),
			int64(time.Until(expiresOn)),
		)

		mParser.On("ParseResponse", idpResponse).Return(token1, nil).Once()
		listener.On("OnNext", token1).Return().Once()

		cancel, err := tokenManager.Start(listener)
		assert.NotNil(t, cancel)
		assert.NoError(t, err)
		assert.NotNil(t, tm.listener)

		toRenewal := tm.durationToRenewal()
		assert.NotEqual(t, time.Duration(0), toRenewal)
		assert.NotEqual(t, expiresIn, toRenewal)
		assert.True(t, expiresIn > toRenewal)
		<-time.After(toRenewal / 10)
		assert.NotNil(t, tm.listener)
		assert.NoError(t, tokenManager.Stop())
		assert.Nil(t, tm.listener)
		assert.Panics(t, func() {
			close(tm.closedChan)
		})

		<-time.After(toRenewal)
		assert.Error(t, tokenManager.Stop())
		mock.AssertExpectationsForObjects(t, idp, mParser, listener)
	})

	t.Run("Start and Listen with 0 renewal duration", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				LowerRefreshBound: time.Hour,
			},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		assert.NoError(t, err)

		expiresIn := time.Second
		expiresOn := time.Now().Add(expiresIn).UTC()

		res := testAuthResult(expiresOn)
		idpResponse := &authResult{
			ResultType:    shared.ResponseTypeAuthResult,
			AuthResultVal: res,
		}
		done := make(chan struct{})
		var twice int32
		var start, stop time.Time
		idp.On("RequestToken", mock.Anything).Run(func(args mock.Arguments) {
			expiresOn := time.Now().Add(expiresIn).UTC()
			res := testAuthResult(expiresOn)
			idpResponse.AuthResultVal = res
			if atomic.LoadInt32(&twice) == 1 {
				stop = time.Now()
				close(done)
				return
			} else {
				atomic.StoreInt32(&twice, 1)
				start = time.Now()
			}
		}).Return(idpResponse, nil)

		listener.On("OnNext", mock.AnythingOfType("*token.Token")).Return()

		cancel, err := tokenManager.Start(listener)
		assert.NotNil(t, cancel)
		assert.NoError(t, err)
		assert.NotNil(t, tm.listener)

		toRenewal := tm.durationToRenewal()
		assert.Equal(t, time.Duration(0), toRenewal)
		assert.True(t, expiresIn > toRenewal)

		// wait for request token to be called
		<-done
		// wait a bit for listener to be notified
		<-time.After(10 * time.Millisecond)
		assert.NoError(t, cancel())

		assert.InDelta(t, stop.Sub(start), tm.retryOptions.InitialDelay, float64(200*time.Millisecond))

		idp.AssertNumberOfCalls(t, "RequestToken", 2)
		listener.AssertNumberOfCalls(t, "OnNext", 2)
		mock.AssertExpectationsForObjects(t, idp, listener)
	})

	t.Run("Start and Listen with 0 renewal duration and closing the token", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				LowerRefreshBound: time.Hour,
				RetryOptions: RetryOptions{
					InitialDelay: 5 * time.Second,
				},
			},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		assert.NoError(t, err)

		expiresIn := time.Second
		expiresOn := time.Now().Add(expiresIn).UTC()
		res := testAuthResult(expiresOn)
		idpResponse := &authResult{
			ResultType:    shared.ResponseTypeAuthResult,
			AuthResultVal: res,
		}
		idp.On("RequestToken", mock.Anything).Run(func(args mock.Arguments) {
			expiresOn := time.Now().Add(expiresIn).UTC()
			res := testAuthResult(expiresOn)
			idpResponse.AuthResultVal = res
		}).Return(idpResponse, nil)

		listener.On("OnNext", mock.AnythingOfType("*token.Token")).Return()

		cancel, err := tokenManager.Start(listener)
		assert.NotNil(t, cancel)
		assert.NoError(t, err)
		assert.NotNil(t, tm.listener)

		toRenewal := tm.durationToRenewal()
		assert.Equal(t, time.Duration(0), toRenewal)
		assert.True(t, expiresIn > toRenewal)

		<-time.After(time.Duration(tm.retryOptions.InitialDelay / 2))
		assert.NoError(t, cancel())
		assert.Nil(t, tm.listener)
		assert.Panics(t, func() {
			close(tm.closedChan)
		})

		// called only once since the token manager was closed prior to initial delay passing
		idp.AssertNumberOfCalls(t, "RequestToken", 1)
		listener.AssertNumberOfCalls(t, "OnNext", 1)
		mock.AssertExpectationsForObjects(t, idp, listener)
	})

	t.Run("Start and Listen", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		assert.NoError(t, err)

		expiresIn := time.Second
		expiresOn := time.Now().Add(expiresIn).UTC()

		res := testAuthResult(expiresOn)
		idpResponse := &authResult{
			ResultType:    shared.ResponseTypeAuthResult,
			AuthResultVal: res,
		}
		idp.On("RequestToken", mock.Anything).Run(func(args mock.Arguments) {
			expiresOn := time.Now().Add(expiresIn).UTC()
			res := testAuthResult(expiresOn)
			idpResponse.AuthResultVal = res
		}).Return(idpResponse, nil)

		listener.On("OnNext", mock.AnythingOfType("*token.Token")).Return()

		cancel, err := tokenManager.Start(listener)
		assert.NotNil(t, cancel)
		assert.NoError(t, err)
		assert.NotNil(t, tm.listener)

		toRenewal := tm.durationToRenewal()
		assert.NotEqual(t, time.Duration(0), toRenewal)
		assert.NotEqual(t, expiresIn, toRenewal)
		assert.True(t, expiresIn > toRenewal)

		<-time.After(toRenewal + time.Second)

		mock.AssertExpectationsForObjects(t, idp, listener)
	})

	t.Run("Start and Listen with retriable error", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		assert.NoError(t, err)

		expiresIn := time.Second
		expiresOn := time.Now().Add(expiresIn).UTC()
		res := testAuthResult(expiresOn)
		idpResponse := &authResult{
			ResultType:    shared.ResponseTypeAuthResult,
			AuthResultVal: res,
		}

		noErrCall := idp.On("RequestToken", mock.Anything).Run(func(args mock.Arguments) {
			expiresOn := time.Now().Add(expiresIn).UTC()
			res := testAuthResult(expiresOn)
			idpResponse.AuthResultVal = res
		}).Return(idpResponse, nil)

		listener.On("OnNext", mock.AnythingOfType("*token.Token")).Return()
		listener.On("OnError", mock.Anything).Run(func(args mock.Arguments) {
			err := args.Get(0)
			assert.NotNil(t, err)
		}).Return().Maybe()

		cancel, err := tokenManager.Start(listener)
		assert.NotNil(t, cancel)
		assert.NoError(t, err)
		assert.NotNil(t, tm.listener)

		noErrCall.Unset()
		returnErr := newMockError(true)
		idp.On("RequestToken", mock.Anything).Return(nil, returnErr)

		toRenewal := tm.durationToRenewal()
		assert.NotEqual(t, time.Duration(0), toRenewal)
		assert.NotEqual(t, expiresIn, toRenewal)
		assert.True(t, expiresIn > toRenewal)
		<-time.After(toRenewal + 100*time.Millisecond)
		idp.AssertNumberOfCalls(t, "RequestToken", 2)
		listener.AssertNumberOfCalls(t, "OnNext", 1)
		mock.AssertExpectationsForObjects(t, idp, listener)
	})

	t.Run("Start and Listen with NOT retriable error", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		assert.NoError(t, err)

		expiresIn := time.Second
		expiresOn := time.Now().Add(expiresIn).UTC()
		res := testAuthResult(expiresOn)
		idpResponse := &authResult{
			ResultType:    shared.ResponseTypeAuthResult,
			AuthResultVal: res,
		}

		noErrCall := idp.On("RequestToken", mock.Anything).Run(func(args mock.Arguments) {
			expiresOn := time.Now().Add(expiresIn).UTC()
			res := testAuthResult(expiresOn)
			idpResponse.AuthResultVal = res
		}).Return(idpResponse, nil)

		listener.On("OnNext", mock.AnythingOfType("*token.Token")).Return()
		listener.On("OnError", mock.Anything).Run(func(args mock.Arguments) {
			err := args.Get(0).(error)
			assert.NotNil(t, err)
		}).Return()

		cancel, err := tokenManager.Start(listener)
		assert.NotNil(t, cancel)
		assert.NoError(t, err)
		assert.NotNil(t, tm.listener)

		noErrCall.Unset()
		returnErr := newMockError(false)
		idp.On("RequestToken", mock.Anything).Return(nil, returnErr)

		toRenewal := tm.durationToRenewal()
		assert.NotEqual(t, time.Duration(0), toRenewal)
		assert.NotEqual(t, expiresIn, toRenewal)
		assert.True(t, expiresIn > toRenewal)
		<-time.After(toRenewal + 100*time.Millisecond)

		idp.AssertNumberOfCalls(t, "RequestToken", 2)
		listener.AssertNumberOfCalls(t, "OnNext", 1)
		listener.AssertNumberOfCalls(t, "OnError", 1)
		mock.AssertExpectationsForObjects(t, idp, listener)
	})

	t.Run("Start and Listen with retriable error - max retries and max delay", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		maxAttempts := 3
		maxDelay := 500 * time.Millisecond
		initialDelay := 100 * time.Millisecond
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				RetryOptions: RetryOptions{
					MaxAttempts:       maxAttempts,
					MaxDelay:          maxDelay,
					InitialDelay:      initialDelay,
					BackoffMultiplier: 10,
				},
			},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		assert.NoError(t, err)

		expiresIn := time.Second
		expiresOn := time.Now().Add(expiresIn).UTC()
		res := testAuthResult(expiresOn)
		res.IDToken.Oid = "test"
		idpResponse := &authResult{
			ResultType:    shared.ResponseTypeAuthResult,
			AuthResultVal: res,
		}

		noErrCall := idp.On("RequestToken", mock.Anything).Run(func(args mock.Arguments) {
			expiresOn := time.Now().Add(expiresIn).UTC()
			res := testAuthResult(expiresOn)
			res.IDToken.Oid = "test"
			idpResponse.AuthResultVal = res
		}).Return(idpResponse, nil)
		var start, end time.Time
		var elapsed time.Duration

		_ = listener.
			On("OnNext", mock.AnythingOfType("*token.Token")).
			Run(func(_ mock.Arguments) {
				start = time.Now()
			}).Return()
		maxAttemptsReached := make(chan struct{})
		listener.On("OnError", mock.Anything).Run(func(args mock.Arguments) {
			err := args.Get(0).(error)
			end = time.Now()
			elapsed = end.Sub(start)
			assert.NotNil(t, err)
			assert.ErrorContains(t, err, "max attempts reached")
			close(maxAttemptsReached)
		}).Return()

		cancel, err := tokenManager.Start(listener)
		assert.NotNil(t, cancel)
		assert.NoError(t, err)
		assert.NotNil(t, tm.listener)
		toRenewal := tm.durationToRenewal()
		assert.NotEqual(t, time.Duration(0), toRenewal)
		assert.NotEqual(t, expiresIn, toRenewal)
		assert.True(t, expiresIn > toRenewal)

		noErrCall.Unset()
		returnErr := newMockError(true)

		idp.On("RequestToken", mock.Anything).Return(nil, returnErr)

		select {
		case <-time.After(toRenewal + time.Duration(maxAttempts)*maxDelay):
			assert.Fail(t, "Timeout - max retries not reached")
		case <-maxAttemptsReached:
		}

		// initialRenewal window, maxAttempts - 1 * max delay + the initial one which was lower than max delay
		allDelaysShouldBe := toRenewal
		allDelaysShouldBe += initialDelay
		allDelaysShouldBe += time.Duration(maxAttempts-1) * maxDelay

		assert.InEpsilon(t, elapsed, allDelaysShouldBe, float64(10*time.Millisecond))

		idp.AssertNumberOfCalls(t, "RequestToken", tm.retryOptions.MaxAttempts+1)
		listener.AssertNumberOfCalls(t, "OnNext", 1)
		listener.AssertNumberOfCalls(t, "OnError", 1)
		mock.AssertExpectationsForObjects(t, idp, listener)
	})
	t.Run("Start and Listen and close during retries", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				RetryOptions: RetryOptions{
					MaxAttempts: 100,
				},
			},
		)
		assert.NoError(t, err)
		assert.NotNil(t, tokenManager)
		tm, ok := tokenManager.(*entraidTokenManager)
		assert.True(t, ok)
		assert.Nil(t, tm.listener)

		assert.NoError(t, err)

		expiresIn := time.Second
		expiresOn := time.Now().Add(expiresIn).UTC()
		res := testAuthResult(expiresOn)
		idpResponse := &authResult{
			ResultType:    shared.ResponseTypeAuthResult,
			AuthResultVal: res,
		}

		noErrCall := idp.On("RequestToken", mock.Anything).Run(func(args mock.Arguments) {
			expiresOn := time.Now().Add(expiresIn).UTC()
			res := testAuthResult(expiresOn)
			idpResponse.AuthResultVal = res
		}).Return(idpResponse, nil)

		listener.On("OnNext", mock.AnythingOfType("*token.Token")).Return()
		maxAttemptsReached := make(chan struct{})
		listener.On("OnError", mock.Anything).Run(func(args mock.Arguments) {
			err := args.Get(0).(error)
			assert.NotNil(t, err)
			assert.ErrorContains(t, err, "max attempts reached")
			close(maxAttemptsReached)
		}).Return().Maybe()

		cancel, err := tokenManager.Start(listener)
		assert.NotNil(t, cancel)
		assert.NoError(t, err)
		assert.NotNil(t, tm.listener)

		noErrCall.Unset()
		returnErr := newMockError(true)
		idp.On("RequestToken", mock.Anything).Return(nil, returnErr)

		toRenewal := tm.durationToRenewal()
		assert.NotEqual(t, time.Duration(0), toRenewal)
		assert.NotEqual(t, expiresIn, toRenewal)
		assert.True(t, expiresIn > toRenewal)

		<-time.After(toRenewal + 500*time.Millisecond)
		assert.Nil(t, cancel())

		select {
		case <-maxAttemptsReached:
			assert.Fail(t, "Max retries reached, token manager not closed")
		case <-tm.closedChan:
		}

		<-time.After(50 * time.Millisecond)

		// maxAttempts + the initial one
		idp.AssertNumberOfCalls(t, "RequestToken", 2)
		listener.AssertNumberOfCalls(t, "OnError", 0)
		mock.AssertExpectationsForObjects(t, idp, listener)
	})
}

func testAuthResult(expiersOn time.Time) *public.AuthResult {
	r := &public.AuthResult{
		ExpiresOn: expiersOn,
	}
	r.IDToken.Oid = "test"
	return r
}

func BenchmarkTokenManager_GetToken(b *testing.B) {
	idp := &mockIdentityProvider{}
	mParser := &mockIdentityProviderResponseParser{}
	tokenManager, err := NewTokenManager(idp,
		TokenManagerOptions{
			IdentityProviderResponseParser: mParser,
		},
	)
	if err != nil {
		b.Fatal(err)
	}

	rawResponse, err := shared.NewIDPResponse(shared.ResponseTypeRawToken, "test")
	if err != nil {
		b.Fatal(err)
	}

	idp.On("RequestToken", mock.Anything).Return(rawResponse, nil)
	mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tokenManager.GetToken(false)
	}
}

func BenchmarkTokenManager_Start(b *testing.B) {
	idp := &mockIdentityProvider{}
	listener := &mockTokenListener{}
	mParser := &mockIdentityProviderResponseParser{}
	tokenManager, err := NewTokenManager(idp,
		TokenManagerOptions{
			IdentityProviderResponseParser: mParser,
		},
	)
	if err != nil {
		b.Fatal(err)
	}

	rawResponse, err := shared.NewIDPResponse(shared.ResponseTypeRawToken, "test")
	if err != nil {
		b.Fatal(err)
	}

	idp.On("RequestToken", mock.Anything).Return(rawResponse, nil)
	mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
	listener.On("OnNext", testTokenValid).Return()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tokenManager.Start(listener)
	}
}

func BenchmarkTokenManager_Close(b *testing.B) {
	idp := &mockIdentityProvider{}
	listener := &mockTokenListener{}
	mParser := &mockIdentityProviderResponseParser{}
	tokenManager, err := NewTokenManager(idp,
		TokenManagerOptions{
			IdentityProviderResponseParser: mParser,
		},
	)
	if err != nil {
		b.Fatal(err)
	}

	rawResponse, err := shared.NewIDPResponse(shared.ResponseTypeRawToken, "test")
	if err != nil {
		b.Fatal(err)
	}

	idp.On("RequestToken", mock.Anything).Return(rawResponse, nil)
	mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
	listener.On("OnNext", testTokenValid).Return()

	_, err = tokenManager.Start(listener)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tokenManager.Stop()
	}
}

func BenchmarkTokenManager_durationToRenewal(b *testing.B) {
	idp := &mockIdentityProvider{}
	tokenManager, err := NewTokenManager(idp, TokenManagerOptions{
		LowerRefreshBound: time.Hour,
	})
	if err != nil {
		b.Fatal(err)
	}

	tm, ok := tokenManager.(*entraidTokenManager)
	if !ok {
		b.Fatal("failed to cast to entraidTokenManager")
	}

	expiresAfterlb := testAuthResult(time.Now().Add(tm.lowerBoundDuration + time.Hour).UTC())
	idpResponse, err := shared.NewIDPResponse(shared.ResponseTypeAuthResult, expiresAfterlb)
	if err != nil {
		b.Fatal(err)
	}

	idp.On("RequestToken", mock.Anything).Return(idpResponse, nil)
	_, err = tm.GetToken(false)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm.durationToRenewal()
	}
}

// TestConcurrentTokenManagerOperations tests concurrent operations on the TokenManager
// to verify there are no deadlocks or race conditions in the implementation.
func TestConcurrentTokenManagerOperations(t *testing.T) {
	t.Parallel()

	// Create a mock identity provider that returns predictable tokens
	mockIdp := &concurrentMockIdentityProvider{
		tokenCounter: 0,
	}

	// Create token manager with the mock provider
	options := TokenManagerOptions{
		ExpirationRefreshRatio: 0.7,
		LowerRefreshBound:      100 * time.Millisecond,
	}
	tm, err := NewTokenManager(mockIdp, options)
	assert.NoError(t, err)
	assert.NotNil(t, tm)

	// Number of concurrent operations to perform
	const numConcurrentOps = 50
	const numGoroutines = 1000

	// Channels to track received tokens and errors
	tokenCh := make(chan *token.Token, numConcurrentOps*numGoroutines)
	errorCh := make(chan error, numConcurrentOps*numGoroutines)

	// Channel to signal completion of all operations
	doneCh := make(chan struct{})

	// Track closers for cleanup
	var closers sync.Map

	// Start multiple goroutines that will concurrently interact with the token manager
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(routineID int) {
			defer wg.Done()

			for j := 0; j < numConcurrentOps; j++ {
				// Create a listener for this operation
				listener := &concurrentTestTokenListener{
					onNextFunc: func(t *token.Token) {
						select {
						case tokenCh <- t:
						default:
							// Channel full, ignore
						}
					},
					onErrorFunc: func(err error) {
						select {
						case errorCh <- err:
						default:
							// Channel full, ignore
						}
					},
				}

				// Choose operation based on a pattern
				// Using modulo for a deterministic pattern that exercises all operations
				opType := j % 3

				// t.Logf("Goroutine %d, Operation %d: Performing operation type %d", routineID, j, opType)

				switch opType {
				case 0:
					// Start the token manager with a new listener
					// t.Logf("Goroutine %d, Operation %d: Attempting to start token manager", routineID, j)
					closeFunc, err := tm.Start(listener)

					if err != nil {
						if err != ErrTokenManagerAlreadyStarted {
							// t.Logf("Goroutine %d, Operation %d: Start failed with error: %v", routineID, j, err)
							select {
							case errorCh <- fmt.Errorf("failed to start token manager: %w", err):
							default:
								t.Fatalf("Goroutine %d, Operation %d: Failed to start token manager: %v", routineID, j, err)
							}
						}
						continue
					}

					// t.Logf("Goroutine %d, Operation %d: Successfully started token manager", routineID, j)
					// Store the closer for later cleanup
					closerKey := fmt.Sprintf("closer-%d-%d", routineID, j)
					closers.Store(closerKey, closeFunc)

					// Simulate some work
					time.Sleep(time.Duration(500-rand.Intn(400)) * time.Millisecond)

				case 1:
					// Get current token
					//t.Logf("Goroutine %d, Operation %d: Getting token", routineID, j)
					token, err := tm.GetToken(false)
					if err != nil {
						//t.Logf("Goroutine %d, Operation %d: GetToken failed with error: %v", routineID, j, err)
						select {
						case errorCh <- fmt.Errorf("failed to get token: %w", err):
						default:
							t.Fatalf("Goroutine %d, Operation %d: Failed to get token: %v", routineID, j, err)
						}
					} else if token != nil {
						//t.Logf("Goroutine %d, Operation %d: Successfully got token, expires: %v", routineID, j, token.ExpirationOn())
						select {
						case tokenCh <- token:
						default:
							// Channel full, ignore
						}
					}

				case 2:
					// Close a previously created token manager listener
					// This simulates multiple subscriptions being created and destroyed
					//t.Logf("Goroutine %d, Operation %d: Attempting to close a token manager", routineID, j)
					closedAny := false

					closers.Range(func(key, value interface{}) bool {
						if j%10 > 7 { // Only close some of the time based on a pattern
							closedAny = true
							//t.Logf("Goroutine %d, Operation %d: Closing token manager with key %v", routineID, j, key)

							closeFunc := value.(StopFunc)
							if err := closeFunc(); err != nil {
								if err != ErrTokenManagerAlreadyStopped {
									// t.Logf("Goroutine %d, Operation %d: Close failed with error: %v", routineID, j, err)
									select {
									case errorCh <- fmt.Errorf("failed to close token manager: %w", err):
									default:
										t.Fatalf("Goroutine %d, Operation %d: Failed to close token manager: %v", routineID, j, err)
									}
								} else {
									//t.Logf("Goroutine %d, Operation %d: TokenManager was already stopped",  routineID, j)
								}
							} else {
								// t.Logf("Goroutine %d, Operation %d: Successfully closed token manager", routineID, j)
							}

							closers.Delete(key)
							return false // stop after finding one to close
						}
						return true
					})

					if !closedAny {
						//t.Logf("Goroutine %d, Operation %d: No token manager to close or condition not met",  routineID, j)
					}
				}
			}
		}(i)
	}

	// Wait for all operations to complete or timeout
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	// Use a timeout to detect deadlocks
	select {
	case <-doneCh:
		// All operations completed successfully
		t.Log("All concurrent operations completed successfully")
	case <-time.After(30 * time.Second):
		t.Fatal("test timed out, possible deadlock detected")
	}

	// Count operations by type
	var startCount, getTokenCount, closeCount int32

	// Collect all ops from goroutines
	for i := 0; i < numGoroutines; i++ {
		for j := 0; j < numConcurrentOps; j++ {
			opType := j % 3
			switch opType {
			case 0:
				atomic.AddInt32(&startCount, 1)
			case 1:
				atomic.AddInt32(&getTokenCount, 1)
			case 2:
				atomic.AddInt32(&closeCount, 1)
			}
		}
	}

	// Clean up any remaining closers
	closers.Range(func(key, value interface{}) bool {
		closeFunc := value.(StopFunc)
		_ = closeFunc() // Ignore errors during cleanup
		return true
	})

	// Close channels to avoid goroutine leaks
	close(tokenCh)
	close(errorCh)

	// Count tokens and check their validity
	var tokens []*token.Token
	for t := range tokenCh {
		tokens = append(tokens, t)
	}

	// Collect and categorize errors
	var startErrors, getTokenErrors, closeErrors, otherErrors []error
	for err := range errorCh {
		errStr := err.Error()
		if strings.Contains(errStr, "failed to start token manager") {
			startErrors = append(startErrors, err)
		} else if strings.Contains(errStr, "failed to get token") {
			getTokenErrors = append(getTokenErrors, err)
		} else if strings.Contains(errStr, "failed to close token manager") {
			closeErrors = append(closeErrors, err)
		} else {
			otherErrors = append(otherErrors, err)
			t.Fatalf("Unexpected error during concurrent operations: %v", err)
		}
	}

	totalOps := startCount + getTokenCount + closeCount
	expectedOps := int32(numGoroutines * numConcurrentOps)

	// Report operation counts
	t.Logf("Concurrent test summary:")
	t.Logf("- Total operations executed: %d (expected: %d)", totalOps, expectedOps)
	t.Logf("- Start operations: %d (with %d errors)", startCount, len(startErrors))
	t.Logf("- GetToken operations: %d (with %d errors, %d successful)",
		getTokenCount, len(getTokenErrors), len(tokens))
	t.Logf("- Close operations: %d (with %d errors)", closeCount, len(closeErrors))

	// Some errors are expected due to concurrent operations
	// but we should have received tokens successfully
	assert.Equal(t, expectedOps, totalOps, "All operations should be accounted for")
	assert.True(t, len(tokens) > 0, "Should have received tokens")

	// Verify the token manager still works after all the concurrent operations
	finalListener := &concurrentTestTokenListener{
		onNextFunc: func(t *token.Token) {
			// Just verify we get a token - don't use assert within this callback
			if t == nil {
				panic("Final token should not be nil")
			}
		},
		onErrorFunc: func(err error) {
			t.Errorf("Unexpected error in final listener: %v", err)
		},
	}

	closeFunc, err := tm.Start(finalListener)
	if err != nil && err != ErrTokenManagerAlreadyStarted {
		t.Fatalf("Failed to start token manager after concurrent operations: %v", err)
	}
	if closeFunc != nil {
		defer closeFunc()
	}

	// Get token one more time to verify everything still works
	finalToken, err := tm.GetToken(true)
	assert.NoError(t, err, "Should be able to get token after concurrent operations")
	assert.NotNil(t, finalToken, "Final token should not be nil")
}

// concurrentTestTokenListener is a test implementation of TokenListener for concurrent tests
type concurrentTestTokenListener struct {
	onNextFunc  func(*token.Token)
	onErrorFunc func(error)
}

func (l *concurrentTestTokenListener) OnNext(t *token.Token) {
	if l.onNextFunc != nil {
		l.onNextFunc(t)
	}
}

func (l *concurrentTestTokenListener) OnError(err error) {
	if l.onErrorFunc != nil {
		l.onErrorFunc(err)
	}
}

// concurrentMockIdentityProvider is a mock implementation of shared.IdentityProvider for concurrent tests
type concurrentMockIdentityProvider struct {
	tokenCounter int
	mutex        sync.Mutex
}

func (m *concurrentMockIdentityProvider) RequestToken(_ context.Context) (shared.IdentityProviderResponse, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.tokenCounter++

	// Use the existing test JWT token which is already properly formatted
	resp, err := shared.NewIDPResponse(shared.ResponseTypeRawToken, testJWTToken)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
