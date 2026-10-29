package main

import (
	"context"
	"testing"
)

// TestProviderLifecycle runs the example against the configured endpoint.
func TestProviderLifecycle(t *testing.T) {
	if err := run(context.Background()); err != nil {
		t.Fatal(err)
	}
}
