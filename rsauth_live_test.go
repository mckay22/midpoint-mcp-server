//go:build integration

// Live verification of resource-server mode (M4.5) against a real OIDC issuer and
// a real midPoint. It is compiled only under -tags=integration and skips unless
// OIDC is configured, so it never runs in the default suite.
//
// It drives the whole path end to end: fetch a real bearer token (OAuth2 password
// grant), present it to the streamable HTTP server, and confirm the request is
// verified, correlated to a midPoint user, and executed as that user via
// Switch-To-Principal — with unmapped and missing tokens refused.
//
// Environment:
//
//	# server (service account + issuer/audience) — reuses the standard config
//	MIDPOINT_URL, MIDPOINT_USERNAME, MIDPOINT_PASSWORD   # the #proxy service account
//	MIDPOINT_MCP_OIDC_ISSUER, MIDPOINT_MCP_OIDC_AUDIENCE
//
//	# how to obtain tokens (OAuth2 password grant)
//	OIDC_IT_TOKEN_URL      # the issuer's token endpoint
//	OIDC_IT_CLIENT_ID      # OAuth client that allows the password grant
//	OIDC_IT_CLIENT_SECRET  # optional, for a confidential client
//
//	# a user present in BOTH the IdP and midPoint (positive impersonation test)
//	OIDC_IT_POS_USERNAME, OIDC_IT_POS_PASSWORD
//	OIDC_IT_POS_EXPECT     # optional: expected midPoint name (defaults to the username)
//
//	# a user present in the IdP but NOT midPoint (negative correlation test)
//	OIDC_IT_NEG_USERNAME, OIDC_IT_NEG_PASSWORD
//
// Example:
//
//	MIDPOINT_URL=https://midpoint.example.com/midpoint \
//	MIDPOINT_USERNAME=svc-mcp MIDPOINT_PASSWORD=… \
//	MIDPOINT_MCP_OIDC_ISSUER=https://idp.example.com/realms/corp \
//	MIDPOINT_MCP_OIDC_AUDIENCE=midpoint-mcp \
//	OIDC_IT_TOKEN_URL=https://idp.example.com/realms/corp/protocol/openid-connect/token \
//	OIDC_IT_CLIENT_ID=midpoint-mcp \
//	OIDC_IT_POS_USERNAME=bob OIDC_IT_POS_PASSWORD=… \
//	OIDC_IT_NEG_USERNAME=alice OIDC_IT_NEG_PASSWORD=… \
//	go test -tags=integration ./... -run LiveOIDCResourceServer -v
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mckay22/midpoint-mcp-server/internal/midpoint"
	"github.com/mckay22/midpoint-mcp-server/internal/oidcauth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLiveOIDCResourceServer(t *testing.T) {
	cfg, err := midpoint.ConfigFromEnv()
	if err != nil {
		t.Skipf("skipping live OIDC test: %v", err)
	}
	if !cfg.ResourceServerMode() {
		t.Skipf("skipping live OIDC test: set %s and %s", midpoint.EnvOIDCIssuer, midpoint.EnvOIDCAudience)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	authn, err := oidcauth.New(ctx, cfg.OIDCIssuer, cfg.OIDCAudience)
	if err != nil {
		t.Fatalf("OIDC discovery against %q failed: %v", cfg.OIDCIssuer, err)
	}
	srv := httptest.NewServer(mcpHTTPHandler(midpoint.NewClient(cfg), cfg, authn))
	defer srv.Close()

	// No token → refused (this always runs when OIDC is configured).
	t.Run("no token is refused", func(t *testing.T) {
		if _, _, err := livePing(ctx, srv.URL, ""); err == nil {
			t.Fatal("a request without a bearer token must be refused")
		}
	})

	// A valid token for a user NOT in midPoint → correlation fails → refused.
	if u, p := os.Getenv("OIDC_IT_NEG_USERNAME"), os.Getenv("OIDC_IT_NEG_PASSWORD"); u != "" && p != "" {
		t.Run("uncorrelated user is refused", func(t *testing.T) {
			tok := liveToken(ctx, t, u, p)
			if _, _, err := livePing(ctx, srv.URL, tok); err == nil {
				t.Fatalf("%q has no midPoint user; the request must be refused", u)
			}
		})
	} else {
		t.Log("negative case skipped (set OIDC_IT_NEG_USERNAME / OIDC_IT_NEG_PASSWORD)")
	}

	// A valid token for a user in both → executed as that user (Switch-To-Principal).
	if u, p := os.Getenv("OIDC_IT_POS_USERNAME"), os.Getenv("OIDC_IT_POS_PASSWORD"); u != "" && p != "" {
		t.Run("mapped user is impersonated", func(t *testing.T) {
			expect := os.Getenv("OIDC_IT_POS_EXPECT")
			if expect == "" {
				expect = u
			}
			tok := liveToken(ctx, t, u, p)
			name, oid, err := livePing(ctx, srv.URL, tok)
			if err != nil {
				t.Fatalf("ping as %q: %v", u, err)
			}
			if name == cfg.Username {
				t.Fatalf("ping returned the service account %q — impersonation did not happen", cfg.Username)
			}
			if name != expect {
				t.Errorf("ping identity = %q, want %q (Switch-To-Principal not applied?)", name, expect)
			}
			t.Logf("impersonation OK: IdP user %q → midPoint identity %q (oid %s)", u, name, oid)
		})
	} else {
		t.Log("positive case skipped (set OIDC_IT_POS_USERNAME / OIDC_IT_POS_PASSWORD)")
	}
}

// liveToken obtains an access token via the OAuth2 password grant. The token is
// never logged.
func liveToken(ctx context.Context, t *testing.T, username, password string) string {
	t.Helper()
	tokenURL, clientID := os.Getenv("OIDC_IT_TOKEN_URL"), os.Getenv("OIDC_IT_CLIENT_ID")
	if tokenURL == "" || clientID == "" {
		t.Fatal("OIDC_IT_TOKEN_URL and OIDC_IT_CLIENT_ID are required to fetch tokens")
	}
	form := url.Values{
		"grant_type": {"password"},
		"client_id":  {clientID},
		"username":   {username},
		"password":   {password},
	}
	if secret := os.Getenv("OIDC_IT_CLIENT_SECRET"); secret != "" {
		form.Set("client_secret", secret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("building token request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token endpoint returned %d: %s", resp.StatusCode, body)
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		t.Fatalf("decoding token response: %v", err)
	}
	if tr.AccessToken == "" {
		t.Fatalf("no access_token for %q: %s (%s)", username, tr.Error, tr.ErrorDesc)
	}
	return tr.AccessToken
}

// livePing connects an MCP client over streamable HTTP (optionally with a bearer
// token) and calls ping, returning the identity midPoint reports for the caller.
func livePing(ctx context.Context, base, token string) (name, oid string, err error) {
	transport := &mcp.StreamableClientTransport{
		Endpoint:             base + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerRoundTripper{token: token, base: http.DefaultTransport}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1, // fail fast on auth errors instead of reconnecting
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "live-it", Version: "t"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		return "", "", err
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "ping", Arguments: map[string]any{}})
	if err != nil {
		return "", "", err
	}
	if res.IsError {
		return "", "", fmt.Errorf("ping tool error: %v", res.Content)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return "", "", err
	}
	var id struct {
		Name string `json:"name"`
		OID  string `json:"oid"`
	}
	if err := json.Unmarshal(b, &id); err != nil {
		return "", "", err
	}
	return id.Name, id.OID, nil
}
