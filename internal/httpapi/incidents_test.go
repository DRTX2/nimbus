package httpapi

import (
	"testing"
	"time"
)

func TestValidateObservationWindow(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 7, 0, 0, time.UTC)
	if err := validateObservationWindow(now.Add(-7*time.Minute), now.Add(-2*time.Minute), now); err != nil {
		t.Fatalf("valid observation window rejected: %v", err)
	}

	tests := []struct {
		name  string
		start time.Time
		end   time.Time
	}{
		{name: "wrong duration", start: now.Add(-10 * time.Minute), end: now.Add(-4 * time.Minute)},
		{name: "unaligned", start: now.Add(-6 * time.Minute), end: now.Add(-time.Minute)},
		{name: "future", start: now, end: now.Add(5 * time.Minute)},
		{name: "stale", start: now.Add(-25 * time.Hour), end: now.Add(-25*time.Hour + 5*time.Minute)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateObservationWindow(test.start, test.end, now); err == nil {
				t.Fatal("invalid observation window was accepted")
			}
		})
	}
}
