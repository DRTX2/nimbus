package store

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultPageSize = 50
	MaxPageSize     = 100
)

// Page requests a keyset page. Cursors are opaque to clients so the ordering
// key can evolve without breaking the API contract.
type Page struct {
	Limit  int
	Cursor string
}

type pageKey struct {
	At time.Time
	ID string
}

func (p Page) normalizedLimit() int {
	if p.Limit < 1 {
		return DefaultPageSize
	}
	if p.Limit > MaxPageSize {
		return MaxPageSize
	}
	return p.Limit
}

func (p Page) key() (*pageKey, error) {
	if p.Cursor == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(p.Cursor)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed cursor", ErrInvalidInput)
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok || len(id) != 36 {
		return nil, fmt.Errorf("%w: malformed cursor", ErrInvalidInput)
	}
	parsed, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed cursor", ErrInvalidInput)
	}
	return &pageKey{At: parsed, ID: id}, nil
}

func encodeCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id))
}
