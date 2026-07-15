# Changelog

All notable changes to this project are documented here. Format loosely
follows [Keep a Changelog](https://keepachangelog.com/); milestones map to
`PLAN.md`.

## [Unreleased]

### M0 — scaffold

- Go module `github.com/mckay22/midpoint-mcp-server` (Go 1.25; the MCP SDK
  requires >= 1.25).
- stdio MCP server exposing one tool, `ping`, which calls midPoint
  `GET /ws/rest/self` and returns the authenticated identity (oid, name, and
  full name / email when set), as both human-readable text and structured
  output.
- `internal/midpoint`: minimal REST client with HTTP Basic auth. Config comes
  from `MIDPOINT_URL`, `MIDPOINT_USERNAME`, `MIDPOINT_PASSWORD`; optional
  `MIDPOINT_INSECURE_TLS=true` skips certificate verification for self-signed
  dev instances only. Credentials are read from the environment at runtime and
  never appear in code, logs, or errors.
- Table-driven tests for the client (recorded `/ws/rest/self` responses via
  `httptest`, covering PolyString-object and bare-string `name` forms, non-2xx
  handling, and a guard that errors never leak the password) and for
  `ConfigFromEnv`. `go test ./...` green.

### Dependencies

- `github.com/modelcontextprotocol/go-sdk` v1.6.1 — the official Model Context
  Protocol SDK for Go; mandated by `PLAN.md` as the server framework. Its
  transitive dependencies (`google/jsonschema-go`, `segmentio/encoding`,
  `segmentio/asm`, `yosida95/uritemplate`, `golang.org/x/oauth2`,
  `golang.org/x/sys`) are pulled in indirectly. No other direct dependencies —
  everything else uses the standard library.
