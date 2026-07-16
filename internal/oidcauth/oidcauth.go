// Package oidcauth verifies OAuth bearer tokens against an OIDC issuer's JWKS.
// It is intentionally thin: token signature/issuer/audience/expiry validation is
// delegated to github.com/coreos/go-oidc rather than hand-rolled, and it exposes
// only the claims needed to map a caller to a midPoint user.
package oidcauth

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// DefaultCorrelationClaim is the token claim read for correlation when a
// deployment does not configure one — the standard OIDC username claim.
const DefaultCorrelationClaim = "preferred_username"

// Claims are the verified token fields used for correlation.
type Claims struct {
	// Subject is the token `sub`, matched against a midPoint user's externalId.
	Subject string
	// CorrelationValue is the value of the configured correlation claim
	// (preferred_username by default), matched against a midPoint attribute.
	CorrelationValue string
	Expiry           time.Time
}

// Authenticator verifies bearer tokens for a single issuer + audience.
type Authenticator struct {
	verifier *oidc.IDTokenVerifier
	// correlationClaim is the token claim whose value CorrelationValue carries.
	// Empty means DefaultCorrelationClaim.
	correlationClaim string
}

// New discovers the issuer's OIDC metadata (and thus its JWKS) and returns an
// Authenticator that requires the given audience. correlationClaim names the token
// claim to expose for correlation (empty = DefaultCorrelationClaim). It performs
// network I/O.
func New(ctx context.Context, issuer, audience, correlationClaim string) (*Authenticator, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return &Authenticator{
		verifier:         provider.Verifier(&oidc.Config{ClientID: audience}),
		correlationClaim: correlationClaim,
	}, nil
}

// Verify validates the token (signature against the JWKS, issuer, audience, and
// expiry) and returns its correlation claims. A non-nil error means the token is
// not to be trusted.
func (a *Authenticator) Verify(ctx context.Context, rawToken string) (Claims, error) {
	tok, err := a.verifier.Verify(ctx, rawToken)
	if err != nil {
		return Claims{}, err
	}
	claim := a.correlationClaim
	if claim == "" {
		claim = DefaultCorrelationClaim
	}
	var all map[string]any
	_ = tok.Claims(&all) // best-effort; Subject/Expiry are already on tok
	return Claims{
		Subject:          tok.Subject,
		CorrelationValue: claimString(all[claim]),
		Expiry:           tok.Expiry,
	}, nil
}

// claimString coerces a decoded JSON claim to a string for correlation. It
// returns "" for an absent or non-scalar claim. A whole number renders without a
// fractional part so a numeric claim (e.g. an employee number) matches midPoint's
// string value.
func claimString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == math.Trunc(t) && !math.IsInf(t, 0) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}
