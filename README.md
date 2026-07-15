# midPoint MCP Server

A [Model Context Protocol](https://modelcontextprotocol.io) server for
[Evolveum midPoint](https://evolveum.com/midpoint/), exposing identity
governance operations as MCP tools so AI assistants can query and (optionally)
manage users, roles, and resources through midPoint's REST API.

> **Status: early development.** The tool surface below is the design target.
> Implemented so far: `ping` (M0), the read tools (M1), the write tools with
> their gate (M2), self-service requests & approvals (M3), and the
> streamable-HTTP transport + packaging (M4). Per-user OIDC auth for shared HTTP
> is next (M4.5).

## Configuration

Credentials are read from the environment at runtime (never written to disk):

| Variable | Required | Purpose |
| --- | --- | --- |
| `MIDPOINT_URL` | yes | midPoint deployment root, e.g. `https://localhost:8443/midpoint` |
| `MIDPOINT_USERNAME` | yes | REST user for HTTP Basic auth |
| `MIDPOINT_PASSWORD` | yes | password for that user |
| `MIDPOINT_INSECURE_TLS` | no | `true` skips TLS verification — self-signed dev instances only |
| `MIDPOINT_MCP_ALLOW_WRITES` | no | `true` enables the write tools; otherwise they return a dry-run preview |

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

**HTTP mode is loopback-only for now.** It binds `127.0.0.1` by default and
*refuses to start* on any non-loopback address — it has no per-request
authentication yet, so a network-reachable endpoint would let every caller act
as the single configured identity. Per-user OAuth/OIDC for shared, multi-user
HTTP is the next milestone (M4.5); use stdio until then.

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

- `request_role` — self-service role request (routed through midPoint's approval
  policy when one applies)
- `list_my_requests` — approval cases you initiated
- `list_work_items` — your approval inbox
- `get_case` — a case and its work items
- `approve_work_item` / `reject_work_item` — decide a work item

## Design

- Single static binary (Go, official MCP SDK), no runtime dependencies
- **stdio** transport by default — drops into Claude Desktop, VS Code, or any
  MCP client config; **streamable HTTP** via `--http` (loopback-only until
  per-user auth lands)
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
