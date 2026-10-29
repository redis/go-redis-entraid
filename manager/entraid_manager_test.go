package manager

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis-entraid/shared"
	"github.com/redis/go-redis-entraid/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const testDurationDelta = float64(10 * time.Millisecond)

func TestDurationToRenewal(t *testing.T) {
	tests := []struct {
		name               string
		token              *token.Token
		refreshRatio       float64
		lowerBoundDuration time.Duration
		expectedDuration   time.Duration
	}{
		{
			name:               "nil token returns 0",
			token:              nil,
			refreshRatio:       0.75,
			lowerBoundDuration: time.Second,
			expectedDuration:   0,
		},
		{
			name: "expired token returns 0",
			token: token.New(
				"username",
				"password",
				"rawToken",
				time.Now().Add(-time.Hour),
				time.Now().Add(-2*time.Hour),
				time.Hour.Milliseconds(),
			),
			refreshRatio:       0.75,
			lowerBoundDuration: time.Second,
			expectedDuration:   0,
		},
		{
			name: "token with TTL less than lower bound returns 0",
			token: token.New(
				"username",
				"password",
				"rawToken",
				time.Now().Add(500*time.Millisecond),
				time.Now().Add(-time.Hour),
				time.Hour.Milliseconds(),
			),
			refreshRatio:       0.75,
			lowerBoundDuration: time.Second,
			expectedDuration:   0,
		},
		{
			name: "token with TTL exactly at lower bound returns 0",
			token: token.New(
				"username",
				"password",
				"rawToken",
				time.Now().Add(time.Second),
				time.Now(),
				time.Second.Milliseconds(),
			),
			refreshRatio:       0.75,
			lowerBoundDuration: time.Second,
			expectedDuration:   0,
		},
		{
			name: "token with refresh time before lower bound",
			token: token.New(
				"username",
				"password",
				"rawToken",
				time.Now().Add(1*time.Hour),    // expires in 1 hour
				time.Now().Add(-1*time.Hour),   // received 1 hour ago
				(2 * time.Hour).Milliseconds(), // TTL is 2 hours
			),
			refreshRatio:       0.75,
			lowerBoundDuration: time.Second,
			// ReceivedAt is 1 hour in the past, TTL is 2 hours, so refresh is at ReceivedAt + 1.5h (75% of 2h).
			// Now is ReceivedAt + 1h, so time until refresh is 30 minutes.
			expectedDuration: 30 * time.Minute,
		},
		{
			name: "token with refresh time before lower bound and large lower bound",
			token: token.New(
				"username",
				"password",
				"rawToken",
				time.Now().Add(1*time.Hour),    // expires in 1 hour
				time.Now().Add(-1*time.Hour),   // received 1 hour ago
				(2 * time.Hour).Milliseconds(), // TTL is 2 hours
			),
			refreshRatio:       0.75,
			lowerBoundDuration: 45 * time.Minute,
			// ReceivedAt is 1 hour in the past, TTL is 2 hours, so refresh is at ReceivedAt + 1.5h (75% of 2h).
			// Now is ReceivedAt + 1h, so time until refresh is 30 minutes.
			// But lower bound is 45 minutes, so refresh is scheduled for 15 minutes from now.
			expectedDuration: 15 * time.Minute,
		},
		{
			name: "token with refresh time after lower bound and past ReceivedAt",
			token: token.New(
				"username",
				"password",
				"rawToken",
				time.Now().Add(1*time.Hour),       // expires in 1 hour
				time.Now().Add(-30*time.Minute),   // received 30 minutes ago
				(90 * time.Minute).Milliseconds(), // TTL is 1.5 hours
			),
			refreshRatio:       0.75,
			lowerBoundDuration: 10 * time.Minute,
			// ReceivedAt is 30 minutes in the past, TTL is 1.5 hours, so refresh is at ReceivedAt + 1.125h (75% of 1.5h).
			// Now is ReceivedAt + 0.5h, so time until refresh is 1.125h - 0.5h = 0.625h = 37.5 minutes.
			expectedDuration: 37*time.Minute + 30*time.Second,
		},
		{
			name: "token with refresh time after lower bound",
			token: token.New(
				"username",
				"password",
				"rawToken",
				time.Now().Add(time.Hour),
				time.Now(),
				time.Hour.Milliseconds(),
			),
			refreshRatio:       0.75,
			lowerBoundDuration: 60 * time.Second,
			expectedDuration:   45 * time.Minute,
		},
		{
			name: "token with refresh ratio 1 and lower bound 10 minutes",
			token: token.New(
				"username",
				"password",
				"rawToken",
				time.Now().Add(time.Hour),
				time.Now(),
				time.Hour.Milliseconds(),
			),
			refreshRatio:       1.0,
			lowerBoundDuration: 10 * time.Minute,
			expectedDuration:   50 * time.Minute,
		},
		{
			name: "token with zero refresh ratio",
			token: token.New(
				"username",
				"password",
				"rawToken",
				time.Now().Add(time.Hour),
				time.Now(),
				time.Hour.Milliseconds(),
			),
			refreshRatio:       0.0,
			lowerBoundDuration: time.Second,
			expectedDuration:   0,
		},
		{
			name: "token with negative refresh ratio",
			token: token.New(
				"username",
				"password",
				"rawToken",
				time.Now().Add(time.Hour),
				time.Now(),
				time.Hour.Milliseconds(),
			),
			refreshRatio:       -0.5,
			lowerBoundDuration: time.Second,
			expectedDuration:   0,
		},
		{
			name: "token with very large TTL",
			token: token.New(
				"username",
				"password",
				"rawToken",
				time.Now().Add(24*365*time.Hour),
				time.Now(),
				(24 * 365 * time.Hour).Milliseconds(),
			),
			refreshRatio:       0.75,
			lowerBoundDuration: time.Hour,
			expectedDuration:   24 * 365 * 45 * time.Minute,
		},
		{
			name: "token with lower bound equal to TTL",
			token: token.New(
				"username",
				"password",
				"rawToken",
				time.Now().Add(time.Hour),
				time.Now(),
				time.Hour.Milliseconds(),
			),
			refreshRatio:       0.75,
			lowerBoundDuration: time.Hour,
			expectedDuration:   0,
		},
		{
			name: "token with lower bound greater than TTL",
			token: token.New(
				"username",
				"password",
				"rawToken",
				time.Now().Add(time.Hour),
				time.Now(),
				time.Hour.Milliseconds(),
			),
			refreshRatio:       0.75,
			lowerBoundDuration: 2 * time.Hour,
			expectedDuration:   0,
		},
		{
			name: "token with refresh ratio resulting in zero refresh time",
			token: token.New(
				"username",
				"password",
				"rawToken",
				time.Now().Add(time.Second),
				time.Now(),
				time.Second.Milliseconds(),
			),
			refreshRatio:       0.0001,
			lowerBoundDuration: time.Millisecond,
			expectedDuration:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &entraidTokenManager{
				expirationRefreshRatio: tt.refreshRatio,
				lowerBoundDuration:     tt.lowerBoundDuration,
			}

			duration := manager.durationToRenewal(tt.token)
			assert.InDelta(t, float64(tt.expectedDuration), float64(duration), testDurationDelta,
				"%s: expected %v, got %v", tt.name, tt.expectedDuration, duration)
		})
	}
}

func TestDurationToRenewalMillisecondPrecision(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name               string
		token              *token.Token
		refreshRatio       float64
		lowerBoundDuration time.Duration
		expectedDuration   time.Duration
	}{
		{
			name: "exact millisecond TTL",
			token: token.New(
				"username",
				"password",
				"rawToken",
				now.Add(time.Second),
				now,
				1000, // 1 second in milliseconds
			),
			refreshRatio:       0.001,
			lowerBoundDuration: time.Millisecond,
			expectedDuration:   time.Millisecond, // 1ms refresh time
		},
		{
			name: "sub-millisecond TTL",
			token: token.New(
				"username",
				"password",
				"rawToken",
				now.Add(100*time.Millisecond),
				now,
				100, // 100ms
			),
			refreshRatio:       0.001,
			lowerBoundDuration: time.Millisecond,
			expectedDuration:   0,
		},
		{
			name: "odd millisecond TTL",
			token: token.New(
				"username",
				"password",
				"rawToken",
				now.Add(123*time.Millisecond),
				now,
				123, // 123ms
			),
			refreshRatio:       0.001,
			lowerBoundDuration: time.Millisecond,
			expectedDuration:   0, // 0.123ms rounds to 0ms
		},
		{
			name: "exact second TTL with millisecond refresh",
			token: token.New(
				"username",
				"password",
				"rawToken",
				now.Add(time.Second),
				now,
				1000, // 1 second in milliseconds
			),
			refreshRatio:       0.001,
			lowerBoundDuration: time.Millisecond,
			expectedDuration:   time.Millisecond, // 1ms refresh time
		},
		{
			name: "high precision refresh ratio",
			token: token.New(
				"username",
				"password",
				"rawToken",
				now.Add(time.Second),
				now,
				1000, // 1 second in milliseconds
			),
			refreshRatio:       0.0001, // 0.01%
			lowerBoundDuration: time.Millisecond,
			expectedDuration:   0, // 0.1ms rounds to 0ms
		},
		{
			name: "very small TTL with high precision ratio",
			token: token.New(
				"username",
				"password",
				"rawToken",
				now.Add(10*time.Millisecond),
				now,
				10, // 10ms
			),
			refreshRatio:       0.0001, // 0.01%
			lowerBoundDuration: time.Millisecond,
			expectedDuration:   0, // 0.001ms rounds to 0ms
		},
		{
			name: "large TTL with high precision ratio",
			token: token.New(
				"username",
				"password",
				"rawToken",
				now.Add(time.Hour),
				now,
				time.Hour.Milliseconds(),
			),
			refreshRatio:       0.0001, // 0.01%
			lowerBoundDuration: time.Millisecond,
			expectedDuration:   360 * time.Millisecond, // 0.01% of 1 hour = 360ms
		},
		{
			name: "boundary case: refresh time exactly 1ms",
			token: token.New(
				"username",
				"password",
				"rawToken",
				now.Add(100*time.Millisecond),
				now,
				100, // 100ms
			),
			refreshRatio:       0.01, // 1%
			lowerBoundDuration: time.Millisecond,
			expectedDuration:   time.Millisecond, // 1ms refresh time
		},
		{
			name: "boundary case: refresh time just below 1ms",
			token: token.New(
				"username",
				"password",
				"rawToken",
				now.Add(100*time.Millisecond),
				now,
				100, // 100ms
			),
			refreshRatio:       0.009, // 0.9%
			lowerBoundDuration: time.Millisecond,
			expectedDuration:   0, // 0.9ms rounds to 0ms
		},
		{
			name: "boundary case: refresh time just above 1ms",
			token: token.New(
				"username",
				"password",
				"rawToken",
				now.Add(100*time.Millisecond),
				now,
				100, // 100ms
			),
			refreshRatio:       0.011, // 1.1%
			lowerBoundDuration: time.Millisecond,
			expectedDuration:   time.Millisecond, // 1.1ms rounds to 1ms
		},
		{
			name: "large TTL with very small refresh ratio",
			token: token.New(
				"username",
				"password",
				"rawToken",
				now.Add(24*time.Hour),
				now,
				(24 * time.Hour).Milliseconds(),
			),
			refreshRatio:       0.0001, // 0.01%
			lowerBoundDuration: time.Millisecond,
			expectedDuration:   8*time.Second + 640*time.Millisecond, // 0.01% of 24 hours = 8.64s
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &entraidTokenManager{
				expirationRefreshRatio: tt.refreshRatio,
				lowerBoundDuration:     tt.lowerBoundDuration,
			}

			duration := manager.durationToRenewal(tt.token)
			assert.InDelta(t, float64(tt.expectedDuration), float64(duration), testDurationDelta,
				"%s: expected %v, got %v", tt.name, tt.expectedDuration, duration)
		})
	}
}

func TestDurationToRenewalConcurrent(t *testing.T) {
	manager := &entraidTokenManager{
		expirationRefreshRatio: 0.75,
		lowerBoundDuration:     time.Second,
	}

	token := token.New(
		"username",
		"password",
		"rawToken",
		time.Now().Add(time.Hour),
		time.Now(),
		time.Hour.Milliseconds(),
	)

	// Run multiple goroutines to test concurrent access
	const goroutines = 10
	results := make(chan time.Duration, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			results <- manager.durationToRenewal(token)
		}()
	}

	// Collect results
	var firstResult time.Duration
	for i := 0; i < goroutines; i++ {
		result := <-results
		if i == 0 {
			firstResult = result
		} else {
			// All results should be within 5ms of each other
			assert.InDelta(t, firstResult.Milliseconds(), result.Milliseconds(), 5)
		}
	}
}

type flightWaitObservedContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func (c *flightWaitObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestForcedCallerDoesNotJoinNonForcedCacheFlight(t *testing.T) {
	idp := &mockIdentityProvider{}
	parser := &mockIdentityProviderResponseParser{}
	fresh := token.New("user", "pass", "fresh", time.Now().Add(time.Hour), time.Now(), time.Hour.Milliseconds())
	idp.On("RequestToken", mock.Anything).Return(shared.IdentityProviderResponse(nil), nil).Once()
	parser.On("ParseResponse", shared.IdentityProviderResponse(nil)).Return(fresh, nil).Once()
	m := &entraidTokenManager{
		idp: idp, identityProviderResponseParser: parser,
		token:       token.New("user", "pass", "cached", time.Now().Add(time.Hour), time.Now(), time.Hour.Milliseconds()),
		tokenRWLock: &sync.RWMutex{}, expirationRefreshRatio: 0.75,
		lowerBoundDuration: time.Second, requestTimeout: time.Second, ctx: context.Background(),
	}
	m.tokenRWLock.Lock()
	nonforced := make(chan error, 1)
	go func() { _, err := m.GetTokenWithContext(context.Background(), false); nonforced <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		m.acquireMu.Lock()
		installed := m.acquire != nil
		m.acquireMu.Unlock()
		if installed {
			break
		}
		if time.Now().After(deadline) {
			m.tokenRWLock.Unlock()
			t.Fatal("non-forced flight was not installed")
		}
		time.Sleep(time.Millisecond)
	}
	forced := make(chan *token.Token, 1)
	forcedContext := &flightWaitObservedContext{Context: context.Background(), waiting: make(chan struct{})}
	go func() { tk, _ := m.GetTokenWithContext(forcedContext, true); forced <- tk }()
	select {
	case <-forcedContext.waiting:
	case <-time.After(time.Second):
		m.tokenRWLock.Unlock()
		t.Fatal("forced caller did not reach the active-flight wait")
	}
	m.tokenRWLock.Unlock()
	if err := <-nonforced; err != nil {
		t.Fatal(err)
	}
	select {
	case tk := <-forced:
		if tk == nil || tk.RawToken() != "fresh" {
			t.Fatalf("forced caller got %v, want refreshed token", tk)
		}
	case <-time.After(time.Second):
		t.Fatal("forced acquisition did not finish")
	}
	idp.AssertExpectations(t)
	parser.AssertExpectations(t)
}

type lifecycleTestListener struct{}

func (lifecycleTestListener) OnNext(*token.Token) {}
func (lifecycleTestListener) OnError(error)       {}

func TestRestartRejectsResultFromRetiredAcquisition(t *testing.T) {
	idp := &mockIdentityProvider{}
	parser := &mockIdentityProviderResponseParser{}
	entered, release := make(chan struct{}), make(chan struct{})
	fresh := token.New("user", "pass", "stale-result", time.Now().Add(time.Hour), time.Now(), time.Hour.Milliseconds())
	cached := token.New("user", "pass", "cached", time.Now().Add(time.Hour), time.Now(), time.Hour.Milliseconds())
	idp.On("RequestToken", mock.Anything).Run(func(args mock.Arguments) { close(entered); <-release }).Return(shared.IdentityProviderResponse(nil), nil).Once()
	parser.On("ParseResponse", shared.IdentityProviderResponse(nil)).Return(fresh, nil).Once()
	m := &entraidTokenManager{
		idp: idp, identityProviderResponseParser: parser, token: cached,
		tokenRWLock: &sync.RWMutex{}, expirationRefreshRatio: 0.75,
		lowerBoundDuration: time.Second, requestTimeout: time.Second,
		startGate: make(chan struct{}, 1), ctx: context.Background(),
	}
	stop, err := m.Start(lifecycleTestListener{})
	if err != nil {
		t.Fatal(err)
	}
	forced := make(chan error, 1)
	go func() { _, err := m.GetTokenWithContext(context.Background(), true); forced <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("old acquisition did not start")
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	newStop := make(chan error, 1)
	go func() {
		stop, err := m.Start(lifecycleTestListener{})
		if err == nil {
			err = stop()
		}
		newStop <- err
	}()
	close(release)
	if err := <-forced; err == nil {
		t.Fatal("retired acquisition result should be rejected")
	}
	select {
	case err := <-newStop:
		if err != nil {
			t.Fatalf("restart failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("restart waited on retired flight indefinitely")
	}
	m.tokenRWLock.RLock()
	got := m.token
	m.tokenRWLock.RUnlock()
	if got.RawToken() != "cached" {
		t.Fatalf("stale lifecycle token was stored: %q", got.RawToken())
	}
	idp.AssertExpectations(t)
	parser.AssertExpectations(t)
}

type panicOnceIDP struct {
	entered chan struct{}
	release chan struct{}
	calls   int
}

func (p *panicOnceIDP) RequestToken(context.Context) (shared.IdentityProviderResponse, error) {
	p.calls++
	if p.calls == 1 && p.entered != nil {
		close(p.entered)
		<-p.release
		panic("identity provider panic")
	}
	return nil, nil
}

type panicOnceParser struct {
	entered chan struct{}
	release chan struct{}
	calls   int
}

func (p *panicOnceParser) ParseResponse(shared.IdentityProviderResponse) (*token.Token, error) {
	p.calls++
	if p.calls == 1 && p.entered != nil {
		close(p.entered)
		<-p.release
		panic("response parser panic")
	}
	return token.New("user", "pass", "retried", time.Now().Add(time.Hour), time.Now(), time.Hour.Milliseconds()), nil
}

func TestAcquisitionWorkerContainsPanics(t *testing.T) {
	if panicAt := os.Getenv("ENTRAID_PANIC_STAGE"); panicAt != "" {
		testAcquisitionWorkerContainsPanic(t, panicAt)
		return
	}
	for _, stage := range []string{"identity", "parser"} {
		t.Run(stage, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestAcquisitionWorkerContainsPanics$")
			cmd.Env = append(os.Environ(), "ENTRAID_PANIC_STAGE="+stage)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("panic acquisition subprocess failed: %v\n%s", err, output)
			}
		})
	}
}

func testAcquisitionWorkerContainsPanic(t *testing.T, stage string) {
	entered, release := make(chan struct{}), make(chan struct{})
	idp := &panicOnceIDP{}
	parser := &panicOnceParser{}
	if stage == "identity" {
		idp.entered, idp.release = entered, release
	} else {
		parser.entered, parser.release = entered, release
	}
	m := &entraidTokenManager{
		idp: idp, identityProviderResponseParser: parser,
		tokenRWLock: &sync.RWMutex{}, expirationRefreshRatio: 0.75,
		lowerBoundDuration: time.Second, requestTimeout: time.Second, ctx: context.Background(),
	}
	first := make(chan error, 1)
	go func() { _, err := m.GetTokenWithContext(context.Background(), false); first <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("acquisition did not reach panic point")
	}
	joinedContext := &flightWaitObservedContext{Context: context.Background(), waiting: make(chan struct{})}
	joined := make(chan error, 1)
	go func() { _, err := m.GetTokenWithContext(joinedContext, false); joined <- err }()
	select {
	case <-joinedContext.waiting:
	case <-time.After(time.Second):
		t.Fatal("second caller did not join acquisition")
	}
	close(release)
	for name, result := range map[string]<-chan error{"first": first, "joined": joined} {
		select {
		case err := <-result:
			if err == nil {
				t.Errorf("%s caller got nil error after panic", name)
			}
		case <-time.After(time.Second):
			t.Errorf("%s caller remained blocked after panic", name)
		}
	}
	tk, err := m.GetTokenWithContext(context.Background(), false)
	if err != nil {
		t.Fatalf("acquisition did not retry after panic: %v", err)
	}
	if tk == nil || tk.RawToken() != "retried" {
		t.Fatalf("retry returned token %v, want retried token", tk)
	}
}
