package internal

import "testing"

func TestIsClosedWithNilChannel(t *testing.T) {
	t.Parallel()
	var ch chan struct{}
	if IsClosed(ch) {
		t.Error("expected nil channel to be open")
	}
}

func TestIsClosedWithEmptyChannel(t *testing.T) {
	t.Parallel()
	ch := make(chan struct{})
	if IsClosed(ch) {
		t.Error("expected empty channel to be open")
	}

	close(ch)
	if !IsClosed(ch) {
		t.Error("expected empty channel to be closed")
	}
}

func TestIsClosedWithClosedChannel(t *testing.T) {
	t.Parallel()
	ch := make(chan struct{})
	close(ch)
	if !IsClosed(ch) {
		t.Error("expected closed channel to be closed")
	}
}

func BenchmarkIsClosedWithNilChannel(b *testing.B) {
	var ch chan struct{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsClosed(ch)
	}
}

func BenchmarkIsClosedWithEmptyChannel(b *testing.B) {
	ch := make(chan struct{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsClosed(ch)
	}
}

func BenchmarkIsClosedWithClosedChannel(b *testing.B) {
	ch := make(chan struct{})
	close(ch)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsClosed(ch)
	}
}
