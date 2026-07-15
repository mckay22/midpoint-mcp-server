package oidcauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

const (
	testIssuer   = "https://issuer.example.com"
	testAudience = "midpoint-mcp"
)

// signer mints signed JWTs for tests.
type signer struct {
	key *rsa.PrivateKey
	jws jose.Signer
}

func newSigner(t *testing.T) *signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jws, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	return &signer{key: key, jws: jws}
}

func (s *signer) mint(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := s.jws.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := obj.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// authenticatorFor builds an Authenticator that trusts s's public key (bypassing
// network discovery via a static key set).
func authenticatorFor(s *signer) *Authenticator {
	keySet := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{s.key.Public()}}
	v := oidc.NewVerifier(testIssuer, keySet, &oidc.Config{ClientID: testAudience})
	return &Authenticator{verifier: v}
}

func baseClaims() map[string]any {
	return map[string]any{
		"iss":                testIssuer,
		"aud":                testAudience,
		"sub":                "user-sub-123",
		"preferred_username": "jdoe",
		"exp":                time.Now().Add(time.Hour).Unix(),
		"iat":                time.Now().Add(-time.Minute).Unix(),
	}
}

func TestVerifyValidToken(t *testing.T) {
	s := newSigner(t)
	a := authenticatorFor(s)

	claims, err := a.Verify(context.Background(), s.mint(t, baseClaims()))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "user-sub-123" || claims.PreferredUsername != "jdoe" {
		t.Errorf("claims = %+v", claims)
	}
	if claims.Expiry.Before(time.Now()) {
		t.Errorf("expiry = %v, want future", claims.Expiry)
	}
}

func TestVerifyRejectsBadTokens(t *testing.T) {
	s := newSigner(t)
	a := authenticatorFor(s)

	t.Run("expired", func(t *testing.T) {
		c := baseClaims()
		c["exp"] = time.Now().Add(-time.Hour).Unix()
		if _, err := a.Verify(context.Background(), s.mint(t, c)); err == nil {
			t.Fatal("expected error for expired token")
		}
	})

	t.Run("wrong audience", func(t *testing.T) {
		c := baseClaims()
		c["aud"] = "some-other-service"
		if _, err := a.Verify(context.Background(), s.mint(t, c)); err == nil {
			t.Fatal("expected error for wrong audience")
		}
	})

	t.Run("wrong issuer", func(t *testing.T) {
		c := baseClaims()
		c["iss"] = "https://evil.example.com"
		if _, err := a.Verify(context.Background(), s.mint(t, c)); err == nil {
			t.Fatal("expected error for wrong issuer")
		}
	})

	t.Run("signature from an untrusted key", func(t *testing.T) {
		// Sign with a different key than the authenticator trusts.
		other := newSigner(t)
		if _, err := a.Verify(context.Background(), other.mint(t, baseClaims())); err == nil {
			t.Fatal("expected error for untrusted signature")
		}
	})

	t.Run("not a jwt", func(t *testing.T) {
		if _, err := a.Verify(context.Background(), "not-a-token"); err == nil {
			t.Fatal("expected error for malformed token")
		}
	})
}
