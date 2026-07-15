# midPoint MCP server — PLAN

Go MCP server for Evolveum midPoint. Public repo — keep README/docs
product-neutral (midPoint + MCP only; no downstream deployment stories).

## Stack

- Go, `github.com/modelcontextprotocol/go-sdk` (official SDK)
- midPoint REST API (`/ws/rest/...`), midPoint 4.8+ / tested against 4.10
- Auth: HTTP Basic (midPoint's native REST auth) via env
  `MIDPOINT_URL`, `MIDPOINT_USERNAME`, `MIDPOINT_PASSWORD`
- Transports: stdio (default), streamable HTTP via `--http :3001` (endpoint `/mcp`)
- Writes gated by `MIDPOINT_MCP_ALLOW_WRITES=true`; every write tool returns a
  dry-run preview unless the gate is on

## Milestones (one per session)

- **M0 — scaffold**: Go module, main with SDK stdio server + one `ping` tool
  hitting `/ws/rest/self`; README stays honest about status. AC: connects from
  an MCP client, `ping` returns the authenticated identity.
- **M1 — read tools**: search_users (name/email/oid), get_user, list_roles,
  get_role, list_resources, get_resource, get_user_assignments. Table-driven
  tests against recorded REST fixtures; integration test against a midPoint
  4.10 docker container (skip when docker absent). AC: an assistant can answer
  "who is X and what do they have?" end to end.
- **M2 — write tools + gate**: create_user, enable/disable_user,
  assign/unassign_role, recompute_user; `MIDPOINT_MCP_ALLOW_WRITES` gate with
  dry-run previews when off. AC: disable→enable round-trip visible in midPoint
  GUI; writes refused (with preview) when the gate is off.
- **M3 — requests & approvals (self-service)**: request_role (assignment-add
  delta → midPoint approval policy turns it into a Case instead of executing),
  list_my_requests, list_work_items (the caller's approval inbox),
  approve_work_item / reject_work_item (behind the write gate), get_case.
  Exact case/work-item REST endpoints verified against midPoint 4.10 during
  implementation. AC against a live midPoint: request → case opens attributed
  to the correct requester → approve via work item → assignment appears.
- **M4 — HTTP transport + packaging**: `--http` streamable HTTP mode,
  Dockerfile (scratch, static), GitHub release with binaries, MCP client
  config snippets in README (Claude Desktop, VS Code).
- **M5 — audit & reporting (read-only, query-driven)**: deliberately skip
  midPoint's native report engine — its CSV/HTML output lands on the server
  filesystem (a `reportData` `filePath`, not a downloadable stream), so it's
  unreachable in shared HTTP mode. Instead build our own query/aggregation
  layer over the REST search API:
  - `search_audit` — audit-trail queries (time range, initiator, target, event
    type, outcome, channel). Audit records are container values with no parent
    object, so the exact REST search shape is verified against midPoint 4.10
    during implementation (same discipline as M3's case endpoints).
  - `search_objects` — filtered searches across users / roles / orgs /
    assignments / shadows (midPoint query language) so the assistant can
    compose ad-hoc reports: orphaned accounts, unused roles, assignments
    expiring soon, disabled users still holding access, SoD conflicts.
  - Optional local read-model: cache/index REST results to power heavier
    aggregation and point-in-time snapshots. **Open design decision for M5:**
    in-memory/ephemeral vs a persistent store. Persisting identity and audit
    data at rest is a real security surface (public repo, IGA data) and adds a
    sync/staleness burden — default to ephemeral, and only introduce
    persistence if a concrete report genuinely requires it, documented in
    CHANGELOG when it does.
  - All read-only, so it stays outside the `MIDPOINT_MCP_ALLOW_WRITES` gate.
  AC against a live midPoint: assistant answers "every change to role X in the
  last 30 days" and "orphaned accounts on resource Y" end to end.

## Identity model (who is the caller?)

Requests and approvals are only meaningful if midPoint sees the real human.
Two supported modes, decided by transport:

- **Personal mode (stdio, default)**: the server runs locally with the USER's
  own midPoint credentials — midPoint natively sees them, approval cases are
  attributed correctly, no delegation machinery exists to abuse.
- **Resource-server mode (HTTP, shared)**: per the MCP Authorization spec the
  client presents an OAuth bearer token; the server validates it against the
  configured OIDC issuer's JWKS (`MIDPOINT_MCP_OIDC_ISSUER`,
  `MIDPOINT_MCP_OIDC_AUDIENCE`), extracts `sub`/`preferred_username`, maps it
  to the midPoint user (correlate `sub` == `externalId`, fall back
  `preferred_username` == `name`), and calls midPoint as the service account
  with the **`Switch-To-Principal: <oid>`** header. The service account holds
  the REST **`#proxy`** authorization, filtered (e.g. to the `Person`
  archetype) so it can never impersonate administrators
  (docs.evolveum.com → REST → authentication → impersonation).
- **Never**: an `on_behalf_of` tool parameter. Identity comes from the
  transport's authentication or the local credential — never from tool
  arguments a caller can fabricate.

## Rules

- Definition of done: AC + tests green (`go test ./...`) + CHANGELOG.md line.
- No AI attribution trailers in commits.
- Credentials never in code, logs, tool output, or fixtures.
- Prefer stdlib beyond the MCP SDK; justify every dependency in CHANGELOG.
