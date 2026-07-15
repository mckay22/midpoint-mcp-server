# Changelog

All notable changes to this project are documented here. Format loosely
follows [Keep a Changelog](https://keepachangelog.com/); milestones map to
`PLAN.md`.

## [Unreleased]

## [0.1.0] - 2026-07-15

First tagged release. Covers M0–M4.5: stdio + streamable-HTTP transports, the
read/write/requests-&-approvals tool set with the write gate, and OIDC
resource-server identity for shared HTTP.

### M4.5 — OIDC resource-server identity

- Resource-server mode for HTTP: set `MIDPOINT_MCP_OIDC_ISSUER` +
  `MIDPOINT_MCP_OIDC_AUDIENCE` and every request must present an OAuth
  `Authorization: Bearer` token. Tokens are validated against the issuer's JWKS
  (signature, issuer, audience, expiry) via `go-oidc`; failures return 401.
- Each caller is mapped to a midPoint user — `sub` → `externalId`, falling back
  to `preferred_username` → `name` — and the request executes as that user via
  the `Switch-To-Principal: <oid>` header while authenticating as the service
  account (which holds the archetype-filtered `#proxy` authorization). The
  correlation search itself runs as the service account, not impersonated.
- Identity flows the guaranteed way: an SDK receiving-middleware reads the
  per-request `TokenInfo` and puts the correlated OID into the request context;
  the REST client sets `Switch-To-Principal` from the context. No on-behalf-of
  tool argument exists — identity comes only from the validated token.
- Non-loopback `--http` binding is unlocked **only** when OIDC is configured;
  otherwise the loopback-only rail from M4 stands. `MIDPOINT_MCP_OIDC_*` is
  all-or-nothing (a half-configured resource server is rejected at startup).
- Test-first (security-critical): token validation with a static JWKS and
  self-signed tokens (valid / expired / wrong-audience / wrong-issuer /
  untrusted-signature / malformed); correlation precedence and ambiguity;
  `Switch-To-Principal` header injection; config validation; and a full
  end-to-end test — mock OIDC (discovery + JWKS) + MCP client with a bearer
  token → `ping` → the mapped `Switch-To-Principal` reaches midPoint, and
  missing/expired/wrong-audience tokens are refused. `go test ./...` green.

### Dependencies

- `github.com/coreos/go-oidc/v3` — OIDC discovery, JWKS handling, and bearer
  token verification. Hand-rolling JWT signature/issuer/audience/expiry checks
  for a security boundary would be a liability; this is the canonical Go OIDC
  library. Pulls in `github.com/go-jose/go-jose/v4` (JWT/JWK crypto).
- `github.com/go-jose/go-jose/v4` — also imported directly in tests to mint
  signed tokens and serve a JWKS; already required transitively by `go-oidc`.

### M4 — HTTP transport + packaging

- `--http <addr>` runs the SDK's streamable HTTP transport at `/mcp` (stdio stays
  the default). `--version` prints the build version.
- **Safety rail (part of the milestone):** HTTP binds `127.0.0.1` by default and
  *refuses to start* on any non-loopback address — there is no bypass flag.
  HTTP mode has no per-request authentication yet (that is M4.5), so it must not
  expose an unauthenticated network surface. In M4, HTTP is still personal mode
  (the configured credentials' identity) over a different transport. The SDK's
  built-in DNS-rebinding protection is left enabled.
- `Dockerfile`: multi-stage build producing a static (`CGO_ENABLED=0`) binary on
  `scratch`, with CA certificates copied in (for HTTPS to midPoint) and a
  non-root user. `.dockerignore` trims the build context.
- Release automation: `.github/workflows/release.yml` cross-builds static
  binaries (linux/darwin/windows × amd64/arm64) on a `vX.Y.Z` tag and publishes
  them with checksums to a GitHub release; `main.version` is injected via
  `-ldflags`. `.github/workflows/ci.yml` runs gofmt/vet/tests on push and PR.
- README: MCP client config snippets (Claude Desktop, VS Code), Docker usage, and
  the HTTP transport with its loopback-only caveat.
- Tests: address resolution / non-loopback refusal (table-driven) and an in-
  process `/mcp` initialize smoke test. `go test ./...` green.

### M3 — requests & approvals (self-service)

- Six tools: `request_role`, `list_my_requests`, `list_work_items`, `get_case`,
  `approve_work_item`, `reject_work_item`.
- `request_role` submits an assignment-add delta on the target user (defaults to
  the authenticated user); midPoint policy decides whether it executes directly
  or opens an approval **case**. The requester identity is always the
  authenticated principal — midPoint sets `requestorRef`; the tool never accepts
  an on-behalf-of requester. After applying, it best-effort surfaces the created
  case oid.
- `list_my_requests` scopes cases to `requestorRef` = self; `list_work_items` is
  the caller's inbox — open cases where `workItem/assigneeRef` = self, returning
  only the caller's still-open work items. Both resolve the caller via
  `/ws/rest/self`, so identity comes from the credentials, not from arguments.
- `approve_work_item` / `reject_work_item` complete a work item via
  `POST /ws/rest/cases/{oid}/workItems/{id}/complete` with the approval-outcome
  URI; both respect the write gate (dry-run preview when off), as does
  `request_role`.
- REST verified against the 4.10 docs: cases `GET`/`POST …/search`,
  work-item `…/complete` (204). Note: an older support-4.10 example page still
  flags work-item completion as unimplemented (MID-6067), but the current cases
  endpoint reference documents it; the integration test is the live check.
- Tests: fixture-based decoding of cases/work-items (incl. inbox filtering to
  self + open items), request/complete delta correctness, MCP round-trip and
  gate on/off proofs. Integration test extended with the full request → case →
  approve → assignment-appears flow (opt-in via `MIDPOINT_IT_APPROVAL_ROLE_OID`).
  `go test ./...` green.

### M2 — write tools + gate

- Six write tools: `create_user`, `enable_user`, `disable_user`, `assign_role`,
  `unassign_role`, `recompute_user`.
- `MIDPOINT_MCP_ALLOW_WRITES` gate (default off). When off, every write tool
  returns a **dry-run preview** — the exact method, endpoint, and request body it
  *would* send — and makes no mutating call. When on, it applies the change and
  reports the result (e.g. the new oid from the create `Location` header).
- REST mapping verified against the 4.10 docs: create via `POST /ws/rest/users`
  (`{"user":{…}}` → 201 + `Location`); enable/disable/assign/unassign via
  `PATCH /ws/rest/users/{oid}` with an `objectModification`/`itemDelta`
  (`replace activation/administrativeStatus`; `add assignment`; `delete
  assignment[<id>]`); recompute via `PATCH …?options=reconcile` with an empty
  modification. `unassign_role` reads the user first to resolve the exact
  assignment container id(s).
- Write path is structured as build-a-Plan then apply, so the preview and the
  applied request are guaranteed identical.
- Tests: delta-JSON correctness for every plan, `Location`→oid parsing, and MCP
  round-trip tests proving the gate — **gate off makes no write call and returns
  a preview; gate on issues the expected PATCH/POST**. Integration test extended
  with a live disable→enable round-trip (runs only when the gate is on).
  `go test ./...` green.

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
