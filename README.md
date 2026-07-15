# midPoint MCP Server

A [Model Context Protocol](https://modelcontextprotocol.io) server for
[Evolveum midPoint](https://evolveum.com/midpoint/), exposing identity
governance operations as MCP tools so AI assistants can query and (optionally)
manage users, roles, and resources through midPoint's REST API.

> **Status: early development.** The tool surface below is the design target;
> watch releases for the first usable build.

## Planned tools

Read (default):

- `search_users` / `get_user` — find identities by name, email, or OID
- `list_roles` / `get_role` — role catalog and definitions
- `list_resources` / `get_resource` — connected systems and their status
- `get_user_assignments` — what a user actually has, and why

Write (must be explicitly enabled):

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

## License

Apache-2.0
