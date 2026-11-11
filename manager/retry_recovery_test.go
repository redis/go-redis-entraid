package manager

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/redis/go-redis-entraid/shared"
	"github.com/redis/go-redis-entraid/token"
	"github.com/stretchr/testify/require"
)

type renewalTimeoutCredential struct{}

func (renewalTimeoutCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{}, &net.DNSError{Err: "timeout", IsTimeout: true}
}

type renewalRecoveryProvider struct {
	chain     *azidentity.ChainedTokenCredential
	calls     atomic.Int32
	successAt int
	token     azcore.AccessToken
}

func (p *renewalRecoveryProvider) RequestToken(ctx context.Context) (shared.IdentityProviderResponse, error) {
	calls := int(p.calls.Add(1))
	if calls == 1 {
		return shared.NewIDPResponse(shared.ResponseTypeAccessToken, &p.token)
	}
	if calls < p.successAt {
		_, err := p.chain.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{"https://redis.azure.com/.default"}})
		return nil, err
	}
	return shared.NewIDPResponse(shared.ResponseTypeAccessToken, &p.token)
}

type renewalRecoveryListener struct {
	next  chan int32
	err   chan error
	calls *atomic.Int32
}

func (l *renewalRecoveryListener) OnNext(*token.Token) {
	select {
	case l.next <- l.calls.Load():
	default:
	}
}
func (l *renewalRecoveryListener) OnError(err error) { l.err <- err }

func TestAzureChainedTimeoutRetriesAndRecoversDuringRenewal(t *testing.T) {
	chain, err := azidentity.NewChainedTokenCredential([]azcore.TokenCredential{renewalTimeoutCredential{}}, nil)
	require.NoError(t, err)
	provider := &renewalRecoveryProvider{
		chain:     chain,
		successAt: 4,
		token:     azcore.AccessToken{Token: newTestJWTToken(time.Now().Add(time.Hour))},
	}
	listener := &renewalRecoveryListener{next: make(chan int32, 1), err: make(chan error, 1), calls: &provider.calls}
	tm, err := NewTokenManager(provider, TokenManagerOptions{
		ExpirationRefreshRatio: 0.5,
		LowerRefreshBound:      time.Hour,
		RetryOptions: RetryOptions{
			MaxAttempts:       3,
			InitialDelay:      time.Millisecond,
			MaxDelay:          time.Millisecond,
			BackoffMultiplier: 1,
		},
	})
	require.NoError(t, err)
	stop, err := tm.Start(listener)
	require.NoError(t, err)
	defer stop()

	select {
	case calls := <-listener.next:
		require.EqualValues(t, 4, calls, "two transient failures must be retried before success")
	case err := <-listener.err:
		t.Fatalf("renewal stopped instead of recovering: %v", err)
	case <-time.After(time.Second):
		t.Fatal("renewal did not recover within retry budget")
	}
}

func TestAzureChainedTimeoutRenewalReportsRetryBudgetExhaustion(t *testing.T) {
	chain, err := azidentity.NewChainedTokenCredential([]azcore.TokenCredential{renewalTimeoutCredential{}}, nil)
	require.NoError(t, err)
	provider := &renewalRecoveryProvider{
		chain:     chain,
		successAt: 100,
		token:     azcore.AccessToken{Token: newTestJWTToken(time.Now().Add(time.Hour))},
	}
	listener := &renewalRecoveryListener{next: make(chan int32, 1), err: make(chan error, 1), calls: &provider.calls}
	tm, err := NewTokenManager(provider, TokenManagerOptions{
		ExpirationRefreshRatio: 0.5,
		LowerRefreshBound:      time.Hour,
		RetryOptions: RetryOptions{
			MaxAttempts:       3,
			InitialDelay:      time.Millisecond,
			MaxDelay:          time.Second, // no new round before the assertions
			BackoffMultiplier: 1,
		},
	})
	require.NoError(t, err)
	stop, err := tm.Start(listener)
	require.NoError(t, err)
	defer stop()

	select {
	case err := <-listener.err:
		require.ErrorContains(t, err, "max attempts reached")
		require.EqualValues(t, 4, provider.calls.Load(), "initial acquisition plus exactly three renewal attempts")
	case <-listener.next:
		t.Fatal("renewal unexpectedly succeeded")
	case <-time.After(time.Second):
		t.Fatal("retry exhaustion was not reported")
	}
}

func TestDefaultRetryClassificationKeepsTerminalAzureFailuresAndCancellation(t *testing.T) {
	terminal, err := azidentity.NewChainedTokenCredential([]azcore.TokenCredential{renewalTerminalCredential{}}, nil)
	require.NoError(t, err)
	_, err = terminal.GetToken(context.Background(), policy.TokenRequestOptions{Scopes: []string{"https://redis.azure.com/.default"}})
	require.Error(t, err)
	require.False(t, defaultIsRetryable(err), "permission/configuration authentication failure must remain terminal")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (&renewalContextCredential{}).GetToken(ctx, policy.TokenRequestOptions{})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, defaultIsRetryable(err), "context cancellation must remain terminal")

	canceledChain, err := azidentity.NewChainedTokenCredential([]azcore.TokenCredential{renewalContextCredential{}}, nil)
	require.NoError(t, err)
	_, err = canceledChain.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{"https://redis.azure.com/.default"}})
	require.Error(t, err)
	require.False(t, defaultIsRetryable(err), "Azure's wrapped context cancellation must remain terminal")
}

type renewalTerminalCredential struct{}

func (renewalTerminalCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{}, errors.New("permission denied by credential configuration")
}

type renewalContextCredential struct{}

func (renewalContextCredential) GetToken(ctx context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{}, ctx.Err()
}
