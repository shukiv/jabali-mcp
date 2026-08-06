# jabali-mcp

A [Model Context Protocol](https://modelcontextprotocol.io) server that exposes
the [Jabali Panel](https://github.com/shukiv/jabali-panel) automation API as MCP
tools, so an LLM agent can drive panel operations (domains, DNS, SSL, mail,
backups, …) through natural language.

> **Status:** skeleton. The structure and design are set; the MCP SDK wiring,
> the HMAC client, and the openapi→tools generator are the first milestone.

## Why this is a thin wrapper, not a new API

Jabali Panel already ships the hard parts:

- **A scoped automation API** — HMAC-SHA256 auth, per-route `RequireScope`,
  fine-grained scopes (`read:dns`, `write:domains`, `read:backups`, `write:ssl`,
  …). See jabali-panel ADR-0093.
- **An OpenAPI spec** (`docs/api/openapi.yaml`) kept in lockstep with the routes
  by a coverage golden test.
- **Credential + idempotency models** (`automation_token`,
  `automation_operation`), so tool calls get replay-safety for free.

This server re-presents those operations as MCP tools. It does not reimplement
auth, validation, or scoping — it rides them.

## Security model (read `docs/DESIGN.md` before adding a tool)

This fronts a **root-capable hosting control plane**. The rules are
non-negotiable:

1. **One tool = one automation scope.** Tools ride `RequireScope`; they never
   bypass it. The server's token is scoped minimally and **never holds
   `write:everything`**.
2. **Read-only by default.** The default surface is `read:*` tools only. Write
   tools are opt-in.
3. **Destructive ops are human-in-the-loop.** Delete user, restore (overwrites
   live files + DB), DNS/SSL writes → confirmation-gated, never
   model-autonomous. Prompt injection is the threat: the model reads
   tenant-controlled data and could be steered to call a destructive tool.
4. **Secrets never leave the panel.** Token/secret hashes are `json:"-"`
   server-side; audit that no tool output leaks them.

## Layout

```
cmd/jabali-mcp/     entry point (stdio MCP server)
internal/tools/     one tool per automation operation; declares its scope
internal/client/    HMAC-SHA256 automation-API client (ADR-0093)
internal/gen/       openapi.yaml -> tool definitions
docs/DESIGN.md      architecture + security model + roadmap
```

## Roadmap

1. **Read-only MVP (operator-local).** Generate `read:*` tools from
   `openapi.yaml`; wire the HMAC client and the MCP stdio server. One panel.
2. **Gated mutations.** Add write tools behind an opt-in scope set +
   confirmation gating.
3. **Fleet.** One server fronting many panels (multi-credential custody) — for
   bulk migration/DNS/SSL ops. Larger blast radius; phase 3.

## Configuration (planned)

`JABALI_PANEL_URL`, `JABALI_AUTOMATION_TOKEN_ID`, and the token secret from a
`0600` file or the environment — never committed, never logged.

## License

MIT — see [LICENSE](LICENSE).
