# Changelog

All notable changes to this project are documented here. Format loosely
follows [Keep a Changelog](https://keepachangelog.com/); milestones map to
`PLAN.md`.

## [Unreleased]

### M1 — read tools

- Seven read-only MCP tools: `search_users` (free-text over name/full
  name/email, or exact OID), `get_user`, `get_user_assignments` (direct
  assignments plus effective membership, each flagged direct or inherited),
  `list_roles`, `get_role`, `list_resources`, `get_resource` (with connection
  status where midPoint reports it). All return structured output plus a
  human-readable line.
- REST client extended with a generic request core (GET/POST, query options),
  `POST /ws/rest/{type}/search` using midPoint's text query language, and
  `GET /ws/rest/{type}/{oid}?options=resolveNames`. Tolerant decoding handles
  midPoint's JSON quirks: PolyStrings as string or object, and single-element
  collections serialized as a bare object instead of an array.
- Search input is escaped into query-language string literals so a
  caller-supplied value can't inject filter syntax; result sizes are capped
  (default 20, max 100).
- Table-driven tests against recorded REST fixtures (`internal/midpoint/testdata`)
  covering search/get/list, the array/single-object envelope, direct-vs-inherited
  membership, and the injection-escaping guard; plus an in-process MCP
  round-trip test that drives every tool through the SDK (verifying input/output
  schema validation, including empty arrays marshaling as `[]` not `null`).
- Integration test (`-tags=integration`) against a live midPoint 4.10 container;
  skips cleanly when `MIDPOINT_*` is unset, so a missing container is never a
  failure. `go test ./...` green.

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
