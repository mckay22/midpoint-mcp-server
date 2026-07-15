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

// serveHTTP runs the streamable HTTP transport at /mcp. It refuses to bind any
// non-loopback address: HTTP mode has no per-request authentication yet
// (PLAN.md M4.5), so a network-reachable surface would be unauthenticated.
// There is deliberately no flag to override this.
func serveHTTP(addr string, client *midpoint.Client, cfg midpoint.Config) error {
	bind, err := resolveLoopbackAddr(addr)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	httpServer := &http.Server{
		Addr:              bind,
		Handler:           mcpHTTPHandler(client, cfg),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Shut down gracefully when signalled.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("%s %s serving on http://%s/mcp (midPoint: %s; writes: %s; personal mode, loopback only)",
		serverName, version, bind, cfg.BaseURL, writeState(cfg))
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// mcpHTTPHandler mounts the streamable MCP handler at /mcp. A single shared
// server backs every session (personal mode).
func mcpHTTPHandler(client *midpoint.Client, cfg midpoint.Config) http.Handler {
	server := newMCPServer(client, cfg)
	streamable := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			SessionTimeout: 5 * time.Minute,
			Logger:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		},
	)
	mux := http.NewServeMux()
	mux.Handle("/mcp", streamable)
	mux.Handle("/mcp/", streamable)
	return mux
}

// resolveLoopbackAddr normalizes a --http bind address and enforces the
// loopback-only rule. A missing host defaults to 127.0.0.1; any non-loopback
// host is rejected.
func resolveLoopbackAddr(addr string) (string, error) {
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
	if !isLoopbackHost(host) {
		return "", fmt.Errorf(
			"refusing to bind --http to non-loopback host %q: HTTP mode has no per-request authentication yet "+
				"(see PLAN.md M4.5); use 127.0.0.1, ::1, or localhost", host)
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
