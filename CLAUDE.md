# midpoint-mcp-server — session rules

`PLAN.md` is authoritative; read it before coding. One milestone per session.

- **This repo is public.** Content stays product-neutral: midPoint + MCP only —
  no references to private projects or downstream deployment stories.
- Go + the official MCP go-sdk; prefer stdlib otherwise; every new dependency
  justified in CHANGELOG.md.
- Definition of done: milestone AC + `go test ./...` green + CHANGELOG.md line.
- No AI attribution trailers (Co-Authored-By etc.) in commits — owner rule.
- Credentials only via env at runtime; never in code, fixtures, or logs.
- Commit and push every green chunk.
