# jabali-mcp

A [Model Context Protocol](https://modelcontextprotocol.io) server that exposes
the [Jabali Panel](https://github.com/shukiv/jabali-panel) REST API as MCP tools,
so an LLM agent can drive panel operations — domains, DNS, mail, applications,
databases, backups — through natural language.

> **Status:** working MVP. Read tools + gated write/destructive tools + fleet
> registry are implemented and tested against an in-memory MCP round-trip. The
> tool set is currently hand-written from the panel's OpenAPI spec; generating
> it from the spec is the next step (see `internal/gen`).

## Auth model — inherits the panel's tenant isolation

Tools authenticate with a **per-user Bearer token** (`jat_…`, minted in the
panel under *API Tokens*). The token acts as its owning user and the panel
enforces ownership on every endpoint, so the MCP server can only ever reach the
resources that token's user owns — a tenant token is confined to that tenant.
Use a **non-admin** token to keep the blast radius to one account.

## Safety

This fronts a hosting control plane, so mutation is fenced in three layers:

1. **Read-only by default.** Only `read:*`-style tools are registered unless
   `JABALI_MCP_ALLOW_WRITE=1` is set.
2. **Destructive tools require `confirm: true`.** `delete_domain`,
   `delete_dns_record`, `delete_mailbox`, `set_mailbox_password`, and
   `restore_backup` return a preview and do nothing on the first call; they act
   only when re-called with `confirm: true`. A model cannot destroy state in one
   step (the guard against prompt-injected tool calls).
3. **Tool hints.** Read tools carry `readOnlyHint`; destructive tools carry
   `destructiveHint` so MCP clients can surface the risk.

TLS verification is always on. To trust a self-hosted panel's private CA, point
`JABALI_CA_FILE` at its bundle — that *adds* the CA, it never disables
verification.

## Configuration (environment)

Single panel:

| Variable | Meaning |
|---|---|
| `JABALI_PANEL_URL` | e.g. `https://panel.example:8443/api/v1` |
| `JABALI_API_TOKEN` | `jat_…` bearer token |
| `JABALI_PANEL_NAME` | logical name (optional; default `default`) |
| `JABALI_CA_FILE` | PEM bundle to trust a self-hosted CA (optional) |
| `JABALI_MCP_ALLOW_WRITE` | `1` to enable mutating tools (default: read-only) |

Fleet (multiple panels): set `JABALI_PANELS_FILE` to a JSON array — it overrides
the single-panel vars, and tools accept an optional `panel` argument to target
one:

```json
[
  { "name": "prod-a", "url": "https://a:8443/api/v1", "token": "jat_…" },
  { "name": "prod-b", "url": "https://b:8443/api/v1", "token": "jat_…" }
]
```

## Tools

**Read (always on):** `list_domains`, `get_domain`, `list_dns_records`,
`list_mailboxes`, `list_forwarders`, `list_applications`, `list_databases`,
`list_backups`, `list_api_tokens`.

**Write (needs `JABALI_MCP_ALLOW_WRITE=1`):** `create_domain`,
`create_dns_record`, `update_dns_record`, `create_mailbox`, `create_forwarder`,
`create_backup` — plus the confirm-gated destructive set: `delete_domain`,
`delete_dns_record`, `delete_mailbox`, `set_mailbox_password`, `restore_backup`.

## Run

```sh
go build -o jabali-mcp ./cmd/jabali-mcp
JABALI_PANEL_URL=https://panel.example:8443/api/v1 \
JABALI_API_TOKEN=jat_… \
./jabali-mcp        # speaks MCP over stdio
```

Register it with an MCP client (e.g. Claude) as a stdio server running the
`jabali-mcp` binary with those env vars set.

## Layout

```
cmd/jabali-mcp/     entry point (stdio MCP server)
internal/client/    Bearer-token HTTP client + fleet registry
internal/tools/     read.go + write.go — one tool per REST operation
internal/gen/       (planned) generate tool defs from openapi.yaml
docs/DESIGN.md      architecture, security model, roadmap
```

## Roadmap

- **M1 — read-only MVP** ✅ client, MCP stdio server, read tools, tests.
- **M2 — gated mutations** ✅ write tools behind an opt-in flag; destructive ops
  confirm-gated.
- **M3 — fleet** ✅ multi-panel registry + `panel` tool argument.
- **Next:** generate the tool set from `docs/api/openapi.yaml`; add per-tool
  input-schema validation and a dry-run mode.

## License

MIT — see [LICENSE](LICENSE).
