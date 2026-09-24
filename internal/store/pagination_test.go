package store

import (
	"errors"
	"testing"
	"time"
)

// Cursor decoding guards a public input boundary, so malformed values must be
// classified as invalid input rather than reaching SQL.
func TestPageCursorRoundTripAndRejection(t *testing.T) {
	at := time.Date(2026, 9, 24, 10, 0, 0, 123456000, time.UTC)
	id := "3f1d2a7e-9a55-4f0e-8a51-0e7c2a0b9c11"
	key, err := Page{Cursor: encodeCursor(at, id)}.key()
	if err != nil || key == nil || !key.At.Equal(at) || key.ID != id {
		t.Fatalf("round trip = %+v, %v", key, err)
	}
	for _, cursor := range []string{"%%%", "bm90LWEtY3Vyc29y", encodeCursor(at, "short")} {
		if _, err := (Page{Cursor: cursor}).key(); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("cursor %q error = %v, want ErrInvalidInput", cursor, err)
		}
	}
	if got := (Page{Limit: 1000}).normalizedLimit(); got != MaxPageSize {
		t.Fatalf("limit clamp = %d", got)
	}
}
