package httpapi

import (
	"crypto/sha256"
	"testing"
	"time"
)

func TestRoleAllows(t *testing.T) {
	tests := []struct {
		role       string
		permission Permission
		want       bool
	}{
		{role: "owner", permission: "identity:write", want: true},
		{role: "operator", permission: "pipeline:write", want: true},
		{role: "operator", permission: "incident:classify", want: true},
		{role: "operator", permission: "approval:decide", want: false},
		{role: "approver", permission: "approval:decide", want: true},
		{role: "approver", permission: "incident:classify", want: false},
		{role: "viewer", permission: "incident:classify", want: false},
		{role: "viewer", permission: "workflow:write", want: false},
	}
	for _, test := range tests {
		if got := roleAllows(test.role, test.permission); got != test.want {
			t.Fatalf("roleAllows(%q, %q) = %v, want %v", test.role, test.permission, got, test.want)
		}
	}
}

func TestWeekBoundsUTC(t *testing.T) {
	now := time.Date(2026, time.July, 29, 23, 30, 0, 0, time.FixedZone("test", -7*60*60))
	start, end := weekBoundsUTC(now, 1)
	wantStart := time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("week bounds = [%s, %s), want [%s, %s)", start, end, wantStart, wantEnd)
	}
}

func TestBearerToken(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
		ok    bool
	}{
		{name: "standard", value: "Bearer token", want: "token", ok: true},
		{name: "case insensitive scheme", value: "bearer token", want: "token", ok: true},
		{name: "surrounding whitespace", value: "  Bearer   token  ", want: "token", ok: true},
		{name: "raw token", value: "token"},
		{name: "missing token", value: "Bearer"},
		{name: "extra field", value: "Bearer token extra"},
		{name: "wrong scheme", value: "Basic token"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := bearerToken(test.value)
			if got != test.want || ok != test.ok {
				t.Fatalf("bearerToken(%q) = (%q, %v), want (%q, %v)", test.value, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestSessionCacheIsBoundedAndExpires(t *testing.T) {
	cache := newSessionCache(3)
	now := time.Now()
	for i := range 10 {
		cache.put(sha256.Sum256([]byte{byte(i)}), sessionEntry{expiresAt: now.Add(time.Minute)})
	}
	if cache.len() > 3 {
		t.Fatalf("cache grew to %d entries, limit 3", cache.len())
	}
	key := sha256.Sum256([]byte("expired"))
	cache.put(key, sessionEntry{expiresAt: now.Add(-time.Second)})
	if _, ok := cache.get(key, now); ok {
		t.Fatal("expired session was served from cache")
	}
}

func TestLooksLikeJWT(t *testing.T) {
	if !looksLikeJWT("eyJhbGciOi.eyJzdWIi.c2ln") || looksLikeJWT("nmb_abcdef") || looksLikeJWT("eyJonlyone.part") {
		t.Fatal("JWT shape detection is wrong")
	}
}
