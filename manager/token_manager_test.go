package manager

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"reflect"
	"runtime"
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
		assert.Equal(t, DefaultRetryOptionsInitialDelayMs, tm.retryOptions.InitialDelayMs)
		assert.Equal(t, DefaultRetryOptionsMaxDelayMs, tm.retryOptions.MaxDelayMs)
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
			err = tokenManager.Close()
			assert.Error(t, err)
		})
		rawResponse, err := shared.NewIDPResponse(shared.ResponseTypeRawToken, "test")
		assert.NoError(t, err)

		idp.On("RequestToken").Return(rawResponse, nil)
		mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
		listener.On("OnTokenNext", testTokenValid).Return()

		assert.NotPanics(t, func() {
			cancel, err := tokenManager.Start(listener)
			assert.NotNil(t, cancel)
			assert.NoError(t, err)
		})
		assert.NotNil(t, tm.listener)

		err = tokenManager.Close()
		assert.Nil(t, tm.listener)
		assert.NoError(t, err)

		assert.NotPanics(t, func() {
			err = tokenManager.Close()
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

		idp.On("RequestToken").Return(rawResponse, nil)
		mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
		listener.On("OnTokenNext", testTokenValid).Return()

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

		idp.On("RequestToken").Return(rawResponse, nil)
		mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
		listener.On("OnTokenNext", testTokenValid).Return()

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
					err := tokenManager.Close()
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

		idp.On("RequestToken").Return(rawResponse, nil)
		mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
		listener.On("OnTokenNext", testTokenValid).Return()

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

			idp.On("RequestToken").Return(rawResponse, nil)
			mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
			listener.On("OnTokenNext", testTokenValid).Return()
			numExecutions := int32(50000)
			for i := int32(0); i < numExecutions; i++ {
				wg.Add(1)
				go func(num int32) {
					defer wg.Done()
					var err error
					time.Sleep(time.Duration(int64(rand.Intn(1000)+(300-int(num)/2)) * int64(time.Millisecond)))
					last.Store(num)
					if num%2 == 0 {
						err = tokenManager.Close()
					} else {
						l := &mockTokenListener{Id: num}
						l.On("OnTokenNext", testTokenValid).Return()
						_, err = tokenManager.Start(l)
					}
					if err != nil {
						if err != ErrTokenManagerAlreadyCanceled && err != ErrTokenManagerAlreadyStarted {
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
				err = tokenManager.Close()
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

		idp.On("RequestToken").Return(rawResponse, nil)
		mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
		listener.On("OnTokenNext", testTokenValid).Return()

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

		idp.On("RequestToken").Return(rawResponse, nil)
		mParser.On("ParseResponse", rawResponse).Return(nil, fmt.Errorf("parse error"))
		listener.On("OnTokenError", mock.Anything).Return()

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

		idp.On("RequestToken").Return(idpResponse, nil)

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

		idp.On("RequestToken").Return(rawResponse, nil)

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
		idp.On("RequestToken").Return(idpResponse, nil)
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

		idp.On("RequestToken").Return(nil, fmt.Errorf("idp error"))

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
			LowerRefreshBoundMs: 1000 * 60 * 60, // 1 hour
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
			idp.On("RequestToken").Return(idpResponse, nil).Once()
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
			idp.On("RequestToken").Return(idpResponse, nil).Once()
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

		idp.On("RequestToken").Return(idpResponse, nil).Once()
		token1 := token.New(
			"test",
			"test",
			"test",
			expiresOn,
			time.Now(),
			int64(time.Until(expiresOn)),
		)

		mParser.On("ParseResponse", idpResponse).Return(token1, nil).Once()
		listener.On("OnTokenNext", token1).Return().Once()

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
		assert.NoError(t, tokenManager.Close())
		assert.Nil(t, tm.listener)
		assert.Panics(t, func() {
			close(tm.closedChan)
		})

		<-time.After(toRenewal)
		assert.Error(t, tokenManager.Close())
		mock.AssertExpectationsForObjects(t, idp, mParser, listener)
	})

	t.Run("Start and Listen with 0 renewal duration", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				LowerRefreshBoundMs: 1000 * 60 * 60, // 1 hour
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
		idp.On("RequestToken").Run(func(args mock.Arguments) {
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

		listener.On("OnTokenNext", mock.AnythingOfType("*token.Token")).Return()

		cancel, err := tokenManager.Start(listener)
		assert.NotNil(t, cancel)
		assert.NoError(t, err)
		assert.NotNil(t, tm.listener)

		toRenewal := tm.durationToRenewal()
		assert.Equal(t, time.Duration(0), toRenewal)
		assert.True(t, expiresIn > toRenewal)

		<-done
		assert.NoError(t, cancel())

		assert.InDelta(t, stop.Sub(start), time.Duration(tm.retryOptions.InitialDelayMs)*time.Millisecond, float64(200*time.Millisecond))

		idp.AssertNumberOfCalls(t, "RequestToken", 2)
		listener.AssertNumberOfCalls(t, "OnTokenNext", 2)
		mock.AssertExpectationsForObjects(t, idp, listener)
	})

	t.Run("Start and Listen with 0 renewal duration and closing the token", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				LowerRefreshBoundMs: 1000 * 60 * 60, // 1 hour
				RetryOptions: RetryOptions{
					InitialDelayMs: 5000, // 5 seconds
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
		idp.On("RequestToken").Run(func(args mock.Arguments) {
			expiresOn := time.Now().Add(expiresIn).UTC()
			res := testAuthResult(expiresOn)
			idpResponse.AuthResultVal = res
		}).Return(idpResponse, nil)

		listener.On("OnTokenNext", mock.AnythingOfType("*token.Token")).Return()

		cancel, err := tokenManager.Start(listener)
		assert.NotNil(t, cancel)
		assert.NoError(t, err)
		assert.NotNil(t, tm.listener)

		toRenewal := tm.durationToRenewal()
		assert.Equal(t, time.Duration(0), toRenewal)
		assert.True(t, expiresIn > toRenewal)

		<-time.After(time.Duration(tm.retryOptions.InitialDelayMs/2) * time.Millisecond)
		assert.NoError(t, cancel())
		assert.Nil(t, tm.listener)
		assert.Panics(t, func() {
			close(tm.closedChan)
		})

		// called only once since the token manager was closed prior to initial delay passing
		idp.AssertNumberOfCalls(t, "RequestToken", 1)
		listener.AssertNumberOfCalls(t, "OnTokenNext", 1)
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
		idp.On("RequestToken").Run(func(args mock.Arguments) {
			expiresOn := time.Now().Add(expiresIn).UTC()
			res := testAuthResult(expiresOn)
			idpResponse.AuthResultVal = res
		}).Return(idpResponse, nil)

		listener.On("OnTokenNext", mock.AnythingOfType("*token.Token")).Return()

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

		noErrCall := idp.On("RequestToken").Run(func(args mock.Arguments) {
			expiresOn := time.Now().Add(expiresIn).UTC()
			res := testAuthResult(expiresOn)
			idpResponse.AuthResultVal = res
		}).Return(idpResponse, nil)

		listener.On("OnTokenNext", mock.AnythingOfType("*token.Token")).Return()
		listener.On("OnTokenError", mock.Anything).Run(func(args mock.Arguments) {
			err := args.Get(0)
			assert.NotNil(t, err)
		}).Return().Maybe()

		cancel, err := tokenManager.Start(listener)
		assert.NotNil(t, cancel)
		assert.NoError(t, err)
		assert.NotNil(t, tm.listener)

		noErrCall.Unset()
		returnErr := newMockError(true)
		idp.On("RequestToken").Return(nil, returnErr)

		toRenewal := tm.durationToRenewal()
		assert.NotEqual(t, time.Duration(0), toRenewal)
		assert.NotEqual(t, expiresIn, toRenewal)
		assert.True(t, expiresIn > toRenewal)
		<-time.After(toRenewal + 100*time.Millisecond)
		idp.AssertNumberOfCalls(t, "RequestToken", 2)
		listener.AssertNumberOfCalls(t, "OnTokenNext", 1)
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

		noErrCall := idp.On("RequestToken").Run(func(args mock.Arguments) {
			expiresOn := time.Now().Add(expiresIn).UTC()
			res := testAuthResult(expiresOn)
			idpResponse.AuthResultVal = res
		}).Return(idpResponse, nil)

		listener.On("OnTokenNext", mock.AnythingOfType("*token.Token")).Return()
		listener.On("OnTokenError", mock.Anything).Run(func(args mock.Arguments) {
			err := args.Get(0).(error)
			assert.NotNil(t, err)
		}).Return()

		cancel, err := tokenManager.Start(listener)
		assert.NotNil(t, cancel)
		assert.NoError(t, err)
		assert.NotNil(t, tm.listener)

		noErrCall.Unset()
		returnErr := newMockError(false)
		idp.On("RequestToken").Return(nil, returnErr)

		toRenewal := tm.durationToRenewal()
		assert.NotEqual(t, time.Duration(0), toRenewal)
		assert.NotEqual(t, expiresIn, toRenewal)
		assert.True(t, expiresIn > toRenewal)
		<-time.After(toRenewal + 100*time.Millisecond)

		idp.AssertNumberOfCalls(t, "RequestToken", 2)
		listener.AssertNumberOfCalls(t, "OnTokenNext", 1)
		listener.AssertNumberOfCalls(t, "OnTokenError", 1)
		mock.AssertExpectationsForObjects(t, idp, listener)
	})

	t.Run("Start and Listen with retriable error - max retries and max delay", func(t *testing.T) {
		t.Parallel()
		idp := &mockIdentityProvider{}
		listener := &mockTokenListener{}
		maxAttempts := 3
		maxDelayMs := 500
		initialDelayMs := 100
		tokenManager, err := NewTokenManager(idp,
			TokenManagerOptions{
				RetryOptions: RetryOptions{
					MaxAttempts:       maxAttempts,
					MaxDelayMs:        maxDelayMs,
					InitialDelayMs:    initialDelayMs,
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

		noErrCall := idp.On("RequestToken").Run(func(args mock.Arguments) {
			expiresOn := time.Now().Add(expiresIn).UTC()
			res := testAuthResult(expiresOn)
			res.IDToken.Oid = "test"
			idpResponse.AuthResultVal = res
		}).Return(idpResponse, nil)
		var start, end time.Time
		var elapsed time.Duration

		_ = listener.
			On("OnTokenNext", mock.AnythingOfType("*token.Token")).
			Run(func(_ mock.Arguments) {
				start = time.Now()
			}).Return()
		maxAttemptsReached := make(chan struct{})
		listener.On("OnTokenError", mock.Anything).Run(func(args mock.Arguments) {
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

		idp.On("RequestToken").Return(nil, returnErr)

		select {
		case <-time.After(toRenewal + time.Duration(maxAttempts*maxDelayMs)*time.Millisecond):
			assert.Fail(t, "Timeout - max retries not reached")
		case <-maxAttemptsReached:
		}

		// initialRenewal window, maxAttempts - 1 * max delay + the initial one which was lower than max delay
		allDelaysShouldBe := toRenewal
		allDelaysShouldBe += time.Duration(initialDelayMs) * time.Millisecond
		allDelaysShouldBe += time.Duration(maxAttempts-1) * time.Duration(maxDelayMs) * time.Millisecond

		assert.InEpsilon(t, elapsed, allDelaysShouldBe, float64(10*time.Millisecond))

		idp.AssertNumberOfCalls(t, "RequestToken", tm.retryOptions.MaxAttempts+1)
		listener.AssertNumberOfCalls(t, "OnTokenNext", 1)
		listener.AssertNumberOfCalls(t, "OnTokenError", 1)
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

		noErrCall := idp.On("RequestToken").Run(func(args mock.Arguments) {
			expiresOn := time.Now().Add(expiresIn).UTC()
			res := testAuthResult(expiresOn)
			idpResponse.AuthResultVal = res
		}).Return(idpResponse, nil)

		listener.On("OnTokenNext", mock.AnythingOfType("*token.Token")).Return()
		maxAttemptsReached := make(chan struct{})
		listener.On("OnTokenError", mock.Anything).Run(func(args mock.Arguments) {
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
		idp.On("RequestToken").Return(nil, returnErr)

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
		listener.AssertNumberOfCalls(t, "OnTokenError", 0)
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

	idp.On("RequestToken").Return(rawResponse, nil)
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

	idp.On("RequestToken").Return(rawResponse, nil)
	mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
	listener.On("OnTokenNext", testTokenValid).Return()

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

	idp.On("RequestToken").Return(rawResponse, nil)
	mParser.On("ParseResponse", rawResponse).Return(testTokenValid, nil)
	listener.On("OnTokenNext", testTokenValid).Return()

	_, err = tokenManager.Start(listener)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tokenManager.Close()
	}
}

func BenchmarkTokenManager_durationToRenewal(b *testing.B) {
	idp := &mockIdentityProvider{}
	tokenManager, err := NewTokenManager(idp, TokenManagerOptions{
		LowerRefreshBoundMs: 1000 * 60 * 60, // 1 hour
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

	idp.On("RequestToken").Return(idpResponse, nil)
	_, err = tm.GetToken(false)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm.durationToRenewal()
	}
}
