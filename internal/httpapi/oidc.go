package httpapi

import (
	"errors"
	"fmt"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

type OIDCValidator struct {
	issuer   string
	audience string
	jwks     keyfunc.Keyfunc
}

func NewOIDCValidator(issuer, audience string) (*OIDCValidator, error) {
	if issuer == "" || audience == "" {
		return nil, errors.New("OIDC issuer and audience are required")
	}
	jwksURL := strings.TrimRight(issuer, "/") + "/.well-known/jwks.json"
	k, err := keyfunc.NewDefault([]string{jwksURL})
	if err != nil {
		return nil, fmt.Errorf("failed to create JWKS from %q: %w", jwksURL, err)
	}
	return &OIDCValidator{
		issuer:   issuer,
		audience: audience,
		jwks:     k,
	}, nil
}

func (v *OIDCValidator) ValidateToken(tokenString string) (*jwt.Token, error) {
	token, err := jwt.Parse(tokenString, v.jwks.Keyfunc, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(v.issuer), jwt.WithAudience(v.audience))
	if err != nil {
		return nil, fmt.Errorf("invalid OIDC token: %w", err)
	}
	if !token.Valid {
		return nil, errors.New("OIDC token is not valid")
	}
	return token, nil
}
