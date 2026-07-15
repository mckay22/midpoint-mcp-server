package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/mckay22/midpoint-mcp-server/internal/midpoint"
	"github.com/mckay22/midpoint-mcp-server/internal/oidcauth"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serveStdio runs the server over stdio (personal mode).
func serveStdio(client *midpoint.Client, cfg midpoint.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	log.Printf("%s %s serving on stdio (midPoint: %s; writes: %s)",
		serverName, version, cfg.BaseURL, writeState(cfg))
	return newMCPServer(client, cfg).Run(ctx, &mcp.StdioTransport{})
}

// serveHTTP runs the streamable HTTP transport at /mcp.
//
// Without OIDC configured it is personal mode and refuses to bind any
// non-loopback address (HTTP has no per-request auth). With OIDC configured it
// is resource-server mode: every request must carry a valid bearer token, is
// mapped to a midPoint user, and executes as that user — so binding a
// network-reachable address is allowed.
func serveHTTP(addr string, client *midpoint.Client, cfg midpoint.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var authn *oidcauth.Authenticator
	if cfg.ResourceServerMode() {
		a, err := oidcauth.New(ctx, cfg.OIDCIssuer, cfg.OIDCAudience)
		if err != nil {
			return fmt.Errorf("configuring OIDC issuer %q: %w", cfg.OIDCIssuer, err)
		}
		authn = a
	}

	bind, err := resolveBindAddr(addr, cfg.ResourceServerMode())
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              bind,
		Handler:           mcpHTTPHandler(client, cfg, authn),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Shut down gracefully when signalled.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	mode := "personal mode, loopback only"
	if authn != nil {
		mode = "resource-server mode (OIDC bearer)"
	}
	log.Printf("%s %s serving on http://%s/mcp (midPoint: %s; writes: %s; %s)",
		serverName, version, bind, cfg.BaseURL, writeState(cfg), mode)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// mcpHTTPHandler mounts the streamable MCP handler at /mcp. When authn is set,
// requests must pass bearer-token verification and each is executed as the
// mapped midPoint user (via principalMiddleware + Switch-To-Principal).
func mcpHTTPHandler(client *midpoint.Client, cfg midpoint.Config, authn *oidcauth.Authenticator) http.Handler {
	server := newMCPServer(client, cfg)
	if authn != nil {
		server.AddReceivingMiddleware(principalMiddleware)
	}

	streamable := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			SessionTimeout: 5 * time.Minute,
			Logger:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		},
	)

	var handler http.Handler = streamable
	if authn != nil {
		handler = sdkauth.RequireBearerToken(bearerVerifier(authn, client), nil)(streamable)
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.Handle("/mcp/", handler)
	return mux
}

// resolveBindAddr normalizes a --http bind address. A missing host defaults to
// 127.0.0.1. A non-loopback host is rejected unless allowNonLoopback is set
// (which happens only in resource-server mode, where requests are authenticated).
func resolveBindAddr(addr string, allowNonLoopback bool) (string, error) {
	a := strings.TrimSpace(addr)
	if a == "" {
		return "", fmt.Errorf("empty --http address")
	}
	// Allow a bare port (e.g. "3001").
	if !strings.Contains(a, ":") {
		a = ":" + a
	}

	host, port, err := net.SplitHostPort(a)
	if err != nil {
		return "", fmt.Errorf("invalid --http address %q: %w", addr, err)
	}
	if port == "" {
		return "", fmt.Errorf("invalid --http address %q: missing port", addr)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if !isLoopbackHost(host) && !allowNonLoopback {
		return "", fmt.Errorf(
			"refusing to bind --http to non-loopback host %q: HTTP has no per-request authentication unless "+
				"OIDC resource-server mode is configured (%s + %s); otherwise use 127.0.0.1, ::1, or localhost",
			host, midpoint.EnvOIDCIssuer, midpoint.EnvOIDCAudience)
	}
	return net.JoinHostPort(host, port), nil
}

// isLoopbackHost reports whether host is a loopback address or "localhost".
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
