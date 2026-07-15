package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mckay22/midpoint-mcp-server/internal/midpoint"
)

func TestResolveBindAddrLoopbackOnly(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"port only defaults to loopback", ":3001", "127.0.0.1:3001", false},
		{"bare port", "3001", "127.0.0.1:3001", false},
		{"explicit loopback ipv4", "127.0.0.1:3001", "127.0.0.1:3001", false},
		{"localhost", "localhost:3001", "localhost:3001", false},
		{"loopback ipv6", "[::1]:3001", "[::1]:3001", false},
		{"all interfaces ipv4 refused", "0.0.0.0:3001", "", true},
		{"lan address refused", "192.168.1.10:3001", "", true},
		{"external host refused", "example.com:3001", "", true},
		{"all interfaces ipv6 refused", "[::]:3001", "", true},
		{"empty refused", "", "", true},
		{"missing port refused", "127.0.0.1:", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveBindAddr(tt.in, false) // loopback-only (personal mode)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveBindAddr(%q, false) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveBindAddr(%q, false): unexpected error %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("resolveBindAddr(%q, false) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// In resource-server mode (allowNonLoopback=true) a public bind is permitted,
// but an empty host still defaults to loopback (operator must opt in explicitly).
func TestResolveBindAddrResourceServer(t *testing.T) {
	cases := map[string]string{
		"0.0.0.0:3001":     "0.0.0.0:3001",
		"192.168.1.10:443": "192.168.1.10:443",
		":3001":            "127.0.0.1:3001",
	}
	for in, want := range cases {
		got, err := resolveBindAddr(in, true)
		if err != nil {
			t.Fatalf("resolveBindAddr(%q, true): %v", in, err)
		}
		if got != want {
			t.Errorf("resolveBindAddr(%q, true) = %q, want %q", in, got, want)
		}
	}
}

// TestHTTPHandlerInitialize confirms the streamable handler answers an MCP
// initialize at /mcp and advertises this server. (initialize does not touch
// midPoint, so the client target is irrelevant.)
func TestHTTPHandlerInitialize(t *testing.T) {
	client := midpoint.NewClient(midpoint.Config{BaseURL: "http://127.0.0.1:1", Username: "u", Password: "p"})
	srv := httptest.NewServer(mcpHTTPHandler(client, midpoint.Config{}, nil))
	defer srv.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), serverName) {
		t.Errorf("initialize response does not mention %q: %s", serverName, b)
	}
}

// TestHTTPUnknownPath404 confirms only /mcp is served.
func TestHTTPUnknownPath404(t *testing.T) {
	srv := httptest.NewServer(mcpHTTPHandler(midpoint.NewClient(midpoint.Config{}), midpoint.Config{}, nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /nope status = %d, want 404", resp.StatusCode)
	}
}
