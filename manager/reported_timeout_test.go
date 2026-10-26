package manager

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

type reportedTimeoutCredential struct{}

func (reportedTimeoutCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{}, &net.DNSError{Err: "timeout", IsTimeout: true, Name: "redis.example"}
}

type diagnosticCredential struct{ err error }

func (c diagnosticCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{}, c.err
}

type inflightCanceledCredential struct{ entered chan struct{} }

func (c inflightCanceledCredential) GetToken(ctx context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	close(c.entered)
	<-ctx.Done()
	return azcore.AccessToken{}, ctx.Err()
}

func TestAzureCancellationDuringAcquisitionRemainsTerminal(t *testing.T) {
	entered := make(chan struct{})
	credential, err := azidentity.NewChainedTokenCredential([]azcore.TokenCredential{inflightCanceledCredential{entered}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := credential.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{"https://redis.azure.com/.default"}})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("credential was not entered")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
		if defaultIsRetryable(err) {
			t.Fatalf("in-flight cancellation became retryable: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("credential did not return after cancellation")
	}
}

func TestReportedAzureTimeoutRemainsRetryable(t *testing.T) {
	credential, err := azidentity.NewChainedTokenCredential([]azcore.TokenCredential{reportedTimeoutCredential{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = credential.GetToken(context.Background(), policy.TokenRequestOptions{Scopes: []string{"https://redis.azure.com/.default"}})
	if err == nil {
		t.Fatal("expected credential timeout")
	}
	if !defaultIsRetryable(err) {
		t.Fatalf("reported timeout still became nonretryable: %T: %v", err, err)
	}
}

func TestAzureChainedNonDNSLookupTimeoutRemainsTerminal(t *testing.T) {
	for _, err := range []error{
		errors.New("permission denied: lookup redis.example: timeout"),
		&net.DNSError{Err: "no such host", Name: "redis.example"},
	} {
		credential, chainErr := azidentity.NewChainedTokenCredential([]azcore.TokenCredential{diagnosticCredential{err: err}}, nil)
		if chainErr != nil {
			t.Fatal(chainErr)
		}
		_, chainErr = credential.GetToken(context.Background(), policy.TokenRequestOptions{Scopes: []string{"https://redis.azure.com/.default"}})
		if chainErr == nil {
			t.Fatal("expected credential failure")
		}
		if defaultIsRetryable(chainErr) {
			t.Fatalf("non-DNS-timeout diagnostic became retryable: %v", chainErr)
		}
	}
}

func TestAzureChainedWrappedDNSLookupTimeoutIsRetryable(t *testing.T) {
	dnsErr := &net.DNSError{Err: "timeout", Name: "redis.example"}
	opErr := &net.OpError{Op: "dial", Net: "tcp", Err: dnsErr}
	tests := []struct {
		name string
		err  error
	}{
		{name: "bare DNS error", err: dnsErr},
		{name: "dial operation error", err: opErr},
		{name: "URL error", err: &url.Error{Op: "Get", URL: "https://redis.example", Err: opErr}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			credential, err := azidentity.NewChainedTokenCredential([]azcore.TokenCredential{diagnosticCredential{err: tt.err}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = credential.GetToken(context.Background(), policy.TokenRequestOptions{Scopes: []string{"https://redis.azure.com/.default"}})
			if err == nil {
				t.Fatal("expected credential failure")
			}
			if !defaultIsRetryable(err) {
				t.Fatalf("wrapped DNS lookup timeout became nonretryable: %T: %v", err, err)
			}
		})
	}
}
