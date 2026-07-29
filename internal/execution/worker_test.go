package execution

import (
	"net/netip"
	"testing"
	"time"
)

func TestBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: 2 * time.Second},
		{attempt: 2, want: 4 * time.Second},
		{attempt: 5, want: 30 * time.Second},
	}
	for _, test := range tests {
		if got := backoff(test.attempt, 2*time.Second, 30*time.Second); got != test.want {
			t.Fatalf("backoff(%d) = %s, want %s", test.attempt, got, test.want)
		}
	}
}

func TestForbiddenIP(t *testing.T) {
	tests := []struct {
		address string
		want    bool
	}{
		{address: "127.0.0.1", want: true},
		{address: "10.0.0.1", want: true},
		{address: "169.254.169.254", want: true},
		{address: "::1", want: true},
		{address: "1.1.1.1", want: false},
	}
	for _, test := range tests {
		if got := forbiddenIP(netip.MustParseAddr(test.address)); got != test.want {
			t.Fatalf("forbiddenIP(%s) = %v, want %v", test.address, got, test.want)
		}
	}
}
