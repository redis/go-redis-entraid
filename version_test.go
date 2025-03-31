package redis

import (
	"testing"
)

func TestVersion(t *testing.T) {
	if Version() != version {
		t.Errorf("Version() = %s; want %s", Version(), version)
	}
}
