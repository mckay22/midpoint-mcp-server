# midPoint MCP Server

A [Model Context Protocol](https://modelcontextprotocol.io) server for
[Evolveum midPoint](https://evolveum.com/midpoint/), exposing identity
governance operations as MCP tools so AI assistants can query and (optionally)
manage users, roles, and resources through midPoint's REST API.

> **Status: early development.** The tool surface below is the design target.
> Implemented so far: `ping` (M0), the read tools (M1), the write tools with
> their gate (M2), self-service requests & approvals (M3), the streamable-HTTP
> transport + packaging (M4), OIDC resource-server identity for shared HTTP
> (M4.5), and query-driven reporting (M5).

## Two modes: personal vs shared

The server runs in one of two identity modes. **Most users want the first, and it
needs no identity provider.**

**Personal mode — stdio (the default).** You run the binary locally (Claude
Desktop, VS Code, a script) with *your own* midPoint credentials. It acts to
midPoint as you; midPoint sees you as you. No OAuth, no OIDC, no Keycloak — just
`MIDPOINT_URL` + `MIDPOINT_USERNAME` + `MIDPOINT_PASSWORD`. This is the common
case.

**Resource-server mode — HTTP + OIDC (opt-in).** One shared server serves many
people over the network, each authenticated by their own OAuth bearer token, and
each request runs as the *real* human so approvals and audit attribute correctly.
This is the only mode that needs an identity provider — and it is *any* OIDC
provider (Keycloak, Okta, Entra / Azure AD, Auth0, …), not a specific one. You opt
in by setting `MIDPOINT_MCP_OIDC_ISSUER` / `MIDPOINT_MCP_OIDC_AUDIENCE`; leave them
unset and the OIDC code never runs.

| You are… | You set | Transport | Identity provider |
| --- | --- | --- | --- |
| one person, your own machine | `MIDPOINT_URL` + your username/password | stdio (default) | **none** |
| a team sharing one server | the above **+** `MIDPOINT_MCP_OIDC_ISSUER` / `_AUDIENCE` | `--http` | **any OIDC** |

Why the split? A single shared server must know *which* human is behind each
request, and it can't trust a caller to self-declare — there is deliberately no
on-behalf-of tool argument — so it requires a signed token from an IdP the
organization already runs. A local personal process has no such problem: it simply
*is* one person with their own credentials. See [Running](#running) for both.

## Configuration

Credentials are read from the environment at runtime (never written to disk):

| Variable | Required | Purpose |
| --- | --- | --- |
| `MIDPOINT_URL` | yes | midPoint deployment root, e.g. `https://localhost:8443/midpoint` |
| `MIDPOINT_USERNAME` | yes | REST user for HTTP Basic auth |
| `MIDPOINT_PASSWORD` | yes | password for that user |
| `MIDPOINT_INSECURE_TLS` | no | `true` skips TLS verification — self-signed dev instances only |
| `MIDPOINT_MCP_ALLOW_WRITES` | no | `true` enables the write tools; otherwise they return a dry-run preview |
| `MIDPOINT_MCP_OIDC_ISSUER` | no | OIDC issuer URL; enables resource-server mode for HTTP (must be set with the audience) |
| `MIDPOINT_MCP_OIDC_AUDIENCE` | no | expected token audience for resource-server mode |

In resource-server mode, `MIDPOINT_USERNAME`/`MIDPOINT_PASSWORD` are the **service
account** — it authenticates the server to midPoint and must hold the
archetype-filtered `#proxy` authorization so it can act as the mapped end users.

## Running

The server speaks **stdio** by default (personal mode — it acts with the
identity of the configured `MIDPOINT_*` credentials). Point your MCP client at
the binary and pass the environment.

### Claude Desktop

In `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "midpoint": {
      "command": "/path/to/midpoint-mcp-server",
      "env": {
        "MIDPOINT_URL": "https://localhost:8443/midpoint",
        "MIDPOINT_USERNAME": "administrator",
        "MIDPOINT_PASSWORD": "your-password"
      }
    }
  }
}
```

### VS Code

In `.vscode/mcp.json` (or your user-level `mcp.json`):

```json
{
  "servers": {
    "midpoint": {
      "command": "/path/to/midpoint-mcp-server",
      "env": {
        "MIDPOINT_URL": "https://localhost:8443/midpoint",
        "MIDPOINT_USERNAME": "administrator",
        "MIDPOINT_PASSWORD": "your-password"
      }
    }
  }
}
```

### Docker

```sh
docker build -t midpoint-mcp-server .
docker run --rm -i \
  -e MIDPOINT_URL=https://host:8443/midpoint \
  -e MIDPOINT_USERNAME=administrator \
  -e MIDPOINT_PASSWORD=your-password \
  midpoint-mcp-server
```

The image is `scratch` plus the static binary and CA certificates, and runs as a
non-root user. `-i` keeps stdin open for the stdio transport.

### HTTP transport

```sh
midpoint-mcp-server --http :3001   # streamable transport at http://127.0.0.1:3001/mcp
```

**Personal mode (no OIDC):** HTTP binds `127.0.0.1` by default and *refuses to
start* on any non-loopback address. Without per-request auth, a network-reachable
endpoint would let every caller act as the single configured identity, so this is
loopback-only by design. Use it for a local client over HTTP; use stdio for
everything else.

**Resource-server mode (OIDC):** set `MIDPOINT_MCP_OIDC_ISSUER` and
`MIDPOINT_MCP_OIDC_AUDIENCE`. Now every request must carry an
`Authorization: Bearer` token:

- the token is validated against the issuer's JWKS (signature, issuer, audience,
  expiry) — invalid tokens get `401`;
- the caller is mapped to a midPoint user (`sub` → `externalId`, else
  `preferred_username` → `name`);
- the request executes **as that user** via midPoint's `Switch-To-Principal`
  header, while the server authenticates as the `#proxy` service account — so
  approvals and audit attribute to the real human.

Because requests are authenticated per user, binding a non-loopback address is
allowed in this mode:

```sh
MIDPOINT_MCP_OIDC_ISSUER=https://keycloak.example.com/realms/corp \
MIDPOINT_MCP_OIDC_AUDIENCE=midpoint-mcp \
midpoint-mcp-server --http 0.0.0.0:3001
```

Identity always comes from the validated token — there is no on-behalf-of tool
argument a caller could use to act as someone else.

**Setting up an identity provider** (Entra ID, Keycloak, Okta, Auth0, …) — how to
configure the audience, correlate tokens to midPoint users, and grant the service
account its `#proxy` authorization — is covered in
[docs/identity-providers.md](docs/identity-providers.md). The short version: the
server is a *resource server*, so it needs **no client secret** — only the issuer
and audience.

## Tools

Read (default, **implemented**):

- `search_users` / `get_user` — find identities by name, email, or OID
- `list_roles` / `get_role` — role catalog and definitions
- `list_resources` / `get_resource` — connected systems and their status
- `get_user_assignments` — what a user actually has, and why

Write (**implemented**; off unless `MIDPOINT_MCP_ALLOW_WRITES=true`, otherwise
each returns a dry-run preview of the exact request it would send):

- `create_user`, `enable_user`, `disable_user`
- `assign_role`, `unassign_role`
- `recompute_user` — trigger midPoint's recompute after changes

Requests & approvals (**implemented**; reads are always available, `request_role`
and the approval actions respect the write gate):

- `list_requestable_roles` — the roles you can request: those flagged
  `requestable` in the catalog, filtered to what you're authorized to see (runs
  as you, so it works per-user in resource-server mode)
- `request_role` — self-service role request (routed through midPoint's approval
  policy when one applies)
- `list_my_requests` — approval cases you initiated
- `list_work_items` — your approval inbox
- `get_case` — a case and its work items
- `approve_work_item` / `reject_work_item` — decide a work item

Reporting (**implemented**, read-only):

- `search_objects` — filtered search across users/roles/orgs/services/shadows/
  resources with a midPoint query-language filter (ad-hoc reports: orphaned
  accounts, unused roles, ...)
- `search_audit` — audit-trail queries (time range + initiator / target / event
  type / outcome / channel). midPoint 4.10 has no REST audit endpoint, so this
  runs a server-side script that reaches the audit service and returns the
  records. It therefore needs script-execution authorization and does **not** work
  under OIDC impersonation (the mapped end user lacks that privilege) — use it in
  personal/service-account mode

## Design

- Single static binary (Go, official MCP SDK), no runtime dependencies
- **stdio** transport by default (personal mode) — drops into Claude Desktop,
  VS Code, or any MCP client; **streamable HTTP** via `--http` (loopback-only
  unless OIDC resource-server mode is configured, then per-user via bearer tokens)
- Talks to midPoint's REST API (4.8+); credentials via environment variables,
  never written to disk
- Write operations are off unless `MIDPOINT_MCP_ALLOW_WRITES=true` — an AI
  assistant reading your IGA is useful, one mutating it is a decision

## Development

- `go test ./...` — unit tests against recorded REST fixtures, no external
  dependencies.
- `go test -tags=integration ./...` — additionally runs live tests against a
  real midPoint (e.g. a 4.10 docker container). Point it at an instance with the
  `MIDPOINT_*` variables above; it skips when they are unset.
- CI (`.github/workflows/ci.yml`) runs `gofmt`, `go vet`, and the tests on every
  push and PR. Pushing a `vX.Y.Z` tag triggers `release.yml`, which cross-builds
  static binaries (linux/darwin/windows, amd64/arm64) and attaches them, with
  checksums, to a GitHub release.

## License

Apache-2.0
