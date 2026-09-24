//go:build acceptance

package acceptance

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// identityProvider is a minimal OIDC issuer: discovery, JWKS, and token minting.
type identityProvider struct {
	server    *httptest.Server
	key       *rsa.PrivateKey
	untrusted *rsa.PrivateKey
}

func newIdentityProvider() (*identityProvider, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	untrusted, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	provider := &identityProvider{key: key, untrusted: untrusted}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]string{
			"issuer": provider.issuer(), "jwks_uri": provider.issuer() + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "acceptance", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	provider.server = httptest.NewServer(mux)
	return provider, nil
}

func (p *identityProvider) issuer() string { return p.server.URL }

type tokenOptions struct {
	expired   bool
	untrusted bool
	audience  string
}

func (p *identityProvider) mint(subject, tenantID string, options tokenOptions) (string, error) {
	now := time.Now()
	expires := now.Add(15 * time.Minute)
	if options.expired {
		expires = now.Add(-10 * time.Minute)
	}
	audience := oidcAudience
	if options.audience != "" {
		audience = options.audience
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": p.issuer(), "aud": audience, "sub": subject, "name": subject,
		"iat": now.Unix(), "exp": expires.Unix(), "jti": randomHex(8),
		oidcTenantClaim: tenantID,
	})
	token.Header["kid"] = "acceptance"
	key := p.key
	if options.untrusted {
		key = p.untrusted
	}
	return token.SignedString(key)
}

func (p *identityProvider) close() { p.server.Close() }
