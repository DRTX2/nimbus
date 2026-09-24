package httpapi

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/david/nimbus/internal/store"
)

const (
	maxCachedSessions = 10_000
	// sessionCacheTTL bounds how long a role or membership change can take to
	// reach a process that already verified the caller's token.
	sessionCacheTTL = time.Minute
)

type principalContextKey struct{}

// authenticator resolves bearer credentials into a tenant-scoped principal.
// API keys are opaque tokens; JWTs are verified against the configured OIDC
// provider and exchanged for a short-lived database session capability.
type authenticator struct {
	store    *store.Store
	oidc     *OIDCVerifier
	logger   *slog.Logger
	sessions *sessionCache
}

func newAuthenticator(repository *store.Store, oidc *OIDCVerifier, logger *slog.Logger) *authenticator {
	return &authenticator{store: repository, oidc: oidc, logger: logger, sessions: newSessionCache(maxCachedSessions)}
}

func (a *authenticator) authenticate(ctx context.Context, token string) (store.Principal, store.TenantCapability, error) {
	if a.oidc != nil && looksLikeJWT(token) {
		key := sha256.Sum256([]byte(token))
		if entry, ok := a.sessions.get(key, time.Now()); ok {
			return entry.principal, entry.capability, nil
		}
		identity, err := a.oidc.Verify(ctx, token)
		if err != nil {
			return store.Principal{}, store.TenantCapability{}, errors.Join(store.ErrForbidden, err)
		}
		principal, capability, err := a.store.AuthenticateOIDC(ctx, store.FederatedIdentity{
			Issuer: identity.Issuer, Subject: identity.Subject, DisplayName: identity.DisplayName,
			TenantID: identity.TenantID, ExpiresAt: identity.ExpiresAt,
		})
		if err != nil {
			return store.Principal{}, store.TenantCapability{}, err
		}
		cacheUntil := time.Now().Add(sessionCacheTTL)
		if identity.ExpiresAt.Before(cacheUntil) {
			cacheUntil = identity.ExpiresAt
		}
		a.sessions.put(key, sessionEntry{principal: principal, capability: capability, expiresAt: cacheUntil})
		return principal, capability, nil
	}
	return a.store.AuthenticateAPIKey(ctx, token)
}

func (a *authenticator) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.URL.Path, "/v1/") {
			next.ServeHTTP(response, request)
			return
		}
		token, ok := bearerToken(request.Header.Get("Authorization"))
		if !ok {
			writeUnauthorized(response)
			return
		}
		principal, capability, err := a.authenticate(request.Context(), token)
		if err != nil {
			if !errors.Is(err, store.ErrForbidden) {
				a.logger.Error("authentication failed", "error", err)
			} else {
				a.logger.Debug("credential rejected", "path", request.URL.Path)
			}
			writeUnauthorized(response)
			return
		}
		ctx := context.WithValue(request.Context(), principalContextKey{}, principal)
		ctx = store.WithTenantCapability(ctx, capability)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func writeUnauthorized(response http.ResponseWriter) {
	response.Header().Set("Content-Type", "application/problem+json")
	response.Header().Set("WWW-Authenticate", `Bearer realm="nimbus"`)
	response.WriteHeader(http.StatusUnauthorized)
	_, _ = response.Write([]byte(`{"title":"Unauthorized","status":401,"detail":"a valid bearer token is required"}`))
}

// looksLikeJWT distinguishes compact JWS tokens from opaque API keys without
// decoding untrusted input.
func looksLikeJWT(token string) bool {
	return strings.HasPrefix(token, "ey") && strings.Count(token, ".") == 2
}

func bearerToken(value string) (string, bool) {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func principalFrom(ctx context.Context) store.Principal {
	principal, _ := ctx.Value(principalContextKey{}).(store.Principal)
	return principal
}

type sessionEntry struct {
	principal  store.Principal
	capability store.TenantCapability
	expiresAt  time.Time
}

// sessionCache bounds memory for verified JWT sessions. Entries never outlive
// the database session they reference.
type sessionCache struct {
	mu      sync.Mutex
	limit   int
	entries map[[sha256.Size]byte]sessionEntry
}

func newSessionCache(limit int) *sessionCache {
	return &sessionCache{limit: limit, entries: make(map[[sha256.Size]byte]sessionEntry)}
}

func (c *sessionCache) get(key [sha256.Size]byte, now time.Time) (sessionEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return sessionEntry{}, false
	}
	if !now.Before(entry.expiresAt) {
		delete(c.entries, key)
		return sessionEntry{}, false
	}
	return entry, true
}

func (c *sessionCache) put(key [sha256.Size]byte, entry sessionEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.limit {
		now := time.Now()
		for candidate, existing := range c.entries {
			if !now.Before(existing.expiresAt) {
				delete(c.entries, candidate)
			}
		}
		// Still full: evict arbitrary entries. Callers simply re-verify.
		for candidate := range c.entries {
			if len(c.entries) < c.limit {
				break
			}
			delete(c.entries, candidate)
		}
	}
	c.entries[key] = entry
}

func (c *sessionCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
