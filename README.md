# midPoint MCP Server

A [Model Context Protocol](https://modelcontextprotocol.io) server for
[Evolveum midPoint](https://evolveum.com/midpoint/), exposing identity
governance operations as MCP tools so AI assistants can query and (optionally)
manage users, roles, and resources through midPoint's REST API.

> **Status: early development.** The tool surface below is the design target.
> Implemented so far: a stdio server with `ping` (M0) and the full read tool set
> (M1) — `search_users`, `get_user`, `get_user_assignments`, `list_roles`,
> `get_role`, `list_resources`, `get_resource`. The write tools are not built
> yet — watch releases.

## Configuration

Credentials are read from the environment at runtime (never written to disk):

| Variable | Required | Purpose |
| --- | --- | --- |
| `MIDPOINT_URL` | yes | midPoint deployment root, e.g. `https://localhost:8443/midpoint` |
| `MIDPOINT_USERNAME` | yes | REST user for HTTP Basic auth |
| `MIDPOINT_PASSWORD` | yes | password for that user |
| `MIDPOINT_INSECURE_TLS` | no | `true` skips TLS verification — self-signed dev instances only |

## Tools

Read (default, **implemented**):

- `search_users` / `get_user` — find identities by name, email, or OID
- `list_roles` / `get_role` — role catalog and definitions
- `list_resources` / `get_resource` — connected systems and their status
- `get_user_assignments` — what a user actually has, and why

Write (planned; must be explicitly enabled):

- `create_user`, `enable_user`, `disable_user`
- `assign_role`, `unassign_role`
- `recompute_user` — trigger midPoint's recompute after changes

## Design

- Single static binary (Go, official MCP SDK), no runtime dependencies
- **stdio** transport by default — drops into Claude Desktop, VS Code, or any
  MCP client config; **streamable HTTP** behind a flag for shared deployments
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

## License

Apache-2.0
