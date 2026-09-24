package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"
)

const (
	oidcDiscoveryTimeout   = 10 * time.Second
	maxDiscoveryBytes      = 256 << 10
	oidcClockLeeway        = 30 * time.Second
	maxOIDCSessionLifetime = time.Hour
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// OIDCConfig describes a single trusted identity provider.
type OIDCConfig struct {
	Issuer        string
	Audience      string
	Algorithms    []string
	TenantClaim   string
	DefaultTenant string
	// HTTPClient is used for discovery and JWKS retrieval. Nil uses a bounded default client.
	HTTPClient *http.Client
}

// OIDCIdentity is the verified, minimal subset of claims Nimbus relies on.
type OIDCIdentity struct {
	Issuer      string
	Subject     string
	DisplayName string
	TenantID    string
	ExpiresAt   time.Time
}

// OIDCVerifier validates bearer JWTs issued by one OIDC provider.
type OIDCVerifier struct {
	config OIDCConfig
	jwks   keyfunc.Keyfunc
}

type discoveryDocument struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

// NewOIDCVerifier resolves the provider discovery document and starts a
// background JWKS refresher bound to ctx.
func NewOIDCVerifier(ctx context.Context, config OIDCConfig) (*OIDCVerifier, error) {
	config.Issuer = strings.TrimSpace(config.Issuer)
	if config.Issuer == "" || strings.TrimSpace(config.Audience) == "" {
		return nil, errors.New("OIDC issuer and audience are required")
	}
	if len(config.Algorithms) == 0 {
		config.Algorithms = []string{"RS256", "ES256"}
	}
	if config.TenantClaim == "" && !uuidPattern.MatchString(config.DefaultTenant) {
		return nil, errors.New("OIDC requires a tenant claim or a default tenant UUID")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: oidcDiscoveryTimeout}
	}
	document, err := discover(ctx, client, config.Issuer)
	if err != nil {
		return nil, err
	}
	jwks, err := keyfunc.NewDefaultOverrideCtx(ctx, []string{document.JWKSURI}, keyfunc.Override{
		Client:            client,
		HTTPTimeout:       oidcDiscoveryTimeout,
		RefreshInterval:   time.Hour,
		RefreshUnknownKID: rate.NewLimiter(rate.Every(5*time.Minute), 1),
	})
	if err != nil {
		return nil, fmt.Errorf("create OIDC JWKS client: %w", err)
	}
	return &OIDCVerifier{config: config, jwks: jwks}, nil
}

func discover(ctx context.Context, client *http.Client, issuer string) (discoveryDocument, error) {
	requestCtx, cancel := context.WithTimeout(ctx, oidcDiscoveryTimeout)
	defer cancel()
	endpoint := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return discoveryDocument{}, fmt.Errorf("build OIDC discovery request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return discoveryDocument{}, fmt.Errorf("fetch OIDC discovery document: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return discoveryDocument{}, fmt.Errorf("OIDC discovery returned status %d", response.StatusCode)
	}
	var document discoveryDocument
	if err := json.NewDecoder(io.LimitReader(response.Body, maxDiscoveryBytes)).Decode(&document); err != nil {
		return discoveryDocument{}, fmt.Errorf("decode OIDC discovery document: %w", err)
	}
	if document.Issuer != issuer {
		return discoveryDocument{}, errors.New("OIDC discovery issuer does not match the configured issuer")
	}
	parsed, err := url.Parse(document.JWKSURI)
	if err != nil || (parsed.Scheme != "https" && !isLoopbackHost(parsed.Hostname())) {
		return discoveryDocument{}, errors.New("OIDC jwks_uri must use https")
	}
	return document, nil
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// Verify validates signature, algorithm, issuer, audience, and lifetime, then
// derives the tenant from a trusted claim or the configured default.
func (v *OIDCVerifier) Verify(ctx context.Context, raw string) (OIDCIdentity, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, v.jwks.KeyfuncCtx(ctx),
		jwt.WithValidMethods(v.config.Algorithms),
		jwt.WithIssuer(v.config.Issuer),
		jwt.WithAudience(v.config.Audience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(oidcClockLeeway),
	)
	if err != nil || !token.Valid {
		return OIDCIdentity{}, fmt.Errorf("invalid OIDC token: %w", err)
	}
	subject, err := claims.GetSubject()
	if err != nil || subject == "" || len(subject) > 255 {
		return OIDCIdentity{}, errors.New("OIDC token has no usable subject")
	}
	expires, err := claims.GetExpirationTime()
	if err != nil || expires == nil {
		return OIDCIdentity{}, errors.New("OIDC token has no expiry")
	}
	tenantID := v.config.DefaultTenant
	if v.config.TenantClaim != "" {
		value, _ := claims[v.config.TenantClaim].(string)
		tenantID = value
	}
	if !uuidPattern.MatchString(tenantID) {
		return OIDCIdentity{}, errors.New("OIDC token does not resolve to a tenant")
	}
	sessionExpiry := expires.Time
	if limit := time.Now().Add(maxOIDCSessionLifetime); sessionExpiry.After(limit) {
		sessionExpiry = limit
	}
	return OIDCIdentity{
		Issuer:      v.config.Issuer,
		Subject:     subject,
		DisplayName: displayName(claims, subject),
		TenantID:    strings.ToLower(tenantID),
		ExpiresAt:   sessionExpiry,
	}, nil
}

func displayName(claims jwt.MapClaims, fallback string) string {
	for _, key := range []string{"name", "preferred_username", "email"} {
		if value, ok := claims[key].(string); ok {
			value = strings.TrimSpace(value)
			if value != "" && len(value) <= 120 {
				return value
			}
		}
	}
	if len(fallback) > 120 {
		return fallback[:120]
	}
	return fallback
}
