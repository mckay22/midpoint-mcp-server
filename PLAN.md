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
- **M3 — HTTP transport + packaging**: `--http` streamable HTTP mode,
  Dockerfile (scratch, static), GitHub release with binaries, MCP client
  config snippets in README (Claude Desktop, VS Code).

## Rules

- Definition of done: AC + tests green (`go test ./...`) + CHANGELOG.md line.
- No AI attribution trailers in commits.
- Credentials never in code, logs, tool output, or fixtures.
- Prefer stdlib beyond the MCP SDK; justify every dependency in CHANGELOG.
