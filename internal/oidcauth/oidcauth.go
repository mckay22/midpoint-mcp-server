// Package oidcauth verifies OAuth bearer tokens against an OIDC issuer's JWKS.
// It is intentionally thin: token signature/issuer/audience/expiry validation is
// delegated to github.com/coreos/go-oidc rather than hand-rolled, and it exposes
// only the claims needed to map a caller to a midPoint user.
package oidcauth

import (
	"context"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Claims are the verified token fields used for correlation.
type Claims struct {
	Subject           string
	PreferredUsername string
	Expiry            time.Time
}

// Authenticator verifies bearer tokens for a single issuer + audience.
type Authenticator struct {
	verifier *oidc.IDTokenVerifier
}

// New discovers the issuer's OIDC metadata (and thus its JWKS) and returns an
// Authenticator that requires the given audience. It performs network I/O.
func New(ctx context.Context, issuer, audience string) (*Authenticator, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return &Authenticator{verifier: provider.Verifier(&oidc.Config{ClientID: audience})}, nil
}

// Verify validates the token (signature against the JWKS, issuer, audience, and
// expiry) and returns its correlation claims. A non-nil error means the token is
// not to be trusted.
func (a *Authenticator) Verify(ctx context.Context, rawToken string) (Claims, error) {
	tok, err := a.verifier.Verify(ctx, rawToken)
	if err != nil {
		return Claims{}, err
	}
	var extra struct {
		PreferredUsername string `json:"preferred_username"`
	}
	_ = tok.Claims(&extra) // best-effort; Subject/Expiry are already on tok
	return Claims{
		Subject:           tok.Subject,
		PreferredUsername: extra.PreferredUsername,
		Expiry:            tok.Expiry,
	}, nil
}
