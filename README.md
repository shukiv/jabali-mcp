# jabali-mcp

**A [Model Context Protocol](https://modelcontextprotocol.io) server for [Jabali Panel](https://github.com/shukiv/jabali-panel).**

<p>
  <a href="#install">Install</a>
  &nbsp;|&nbsp;
  <a href="#set-up">Set up</a>
  &nbsp;|&nbsp;
  <a href="#tools">Tools</a>
  &nbsp;|&nbsp;
  <a href="#safety">Safety</a>
  &nbsp;|&nbsp;
  <a href="#tool-generation">Generation</a>
</p>

<p>
  <img src="https://img.shields.io/badge/status-working_MVP-f59e0b" alt="Working MVP">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white" alt="Go 1.25">
  <img src="https://img.shields.io/badge/MCP-stdio-6E56CF" alt="MCP stdio">
  <img src="https://img.shields.io/badge/License-MIT-blue" alt="MIT">
</p>

Lets an AI assistant drive panel operations — domains, DNS, mail, applications,
databases, backups — through natural language. It's a thin, generated wrapper
over the panel's existing REST API: it reimplements no auth, validation, or
scoping, it rides them.

> **Status:** working MVP. Read tools + gated write/destructive tools + a fleet
> registry, generated from the panel's OpenAPI spec (a golden test fails if the
> checked-in code drifts from the spec).

## Install

**Prerequisites:** Go 1.25+ and a reachable Jabali Panel with an API token
(see [Set up](#set-up)). The server speaks MCP over stdio — an MCP client
(Claude Code, Claude Desktop, …) launches it; you don't run it as a daemon.

### From source (recommended)

The repo is private, so clone over SSH and build:

```sh
git clone git@github.com:shukiv/jabali-mcp.git
cd jabali-mcp
make build            # -> ./jabali-mcp
```

`make build` stamps the version from `git describe`. Move the binary onto your
`PATH` if you like:

```sh
sudo install -m 0755 jabali-mcp /usr/local/bin/jabali-mcp
```

### With `go install`

```sh
# private repo: tell the Go toolchain not to use the public proxy/sumdb,
# and make sure git can auth to github (SSH or a token).
export GOPRIVATE=github.com/shukiv/*
go install github.com/shukiv/jabali-mcp/cmd/jabali-mcp@latest
```

The binary lands in `$(go env GOBIN)` (or `$(go env GOPATH)/bin`).

### Verify

```sh
JABALI_PANEL_URL=https://panel.example:8443/api/v1 \
JABALI_API_TOKEN=jat_… \
jabali-mcp < /dev/null
# -> jabali-mcp <version> — read-only; panels: [default]
```

It reads config from the environment, prints a one-line status to **stderr**,
then serves MCP on stdin/stdout (it exits on EOF, which is why the `< /dev/null`
smoke test returns immediately).

## Set up

**1. Mint an API token.** In the panel, go to the tenant shell → **API Tokens**
and create one. The plaintext `jat_…` is shown once — copy it. Use a **non-admin**
(tenant) token so the server is confined to that one account.

**2. Point the server at your panel** with these environment variables:

| Variable | Required | Meaning |
|---|:---:|---|
| `JABALI_PANEL_URL` | ✓ | e.g. `https://panel.example:8443/api/v1` |
| `JABALI_API_TOKEN` | ✓ | the `jat_…` bearer token |
| `JABALI_PANEL_NAME` | | logical name (default `default`) |
| `JABALI_CA_FILE` | | PEM bundle to trust a self-hosted panel CA |
| `JABALI_MCP_ALLOW_WRITE` | | `1` to enable mutating tools (default: read-only) |
| `JABALI_MCP_DRY_RUN` | | `1` to force every write to preview instead of act |
| `JABALI_PANELS_FILE` | | JSON array of panels for fleet mode (overrides the single-panel vars) |

**3. Register it with your MCP client.** In every example below `jabali-mcp` is
assumed on your `PATH` (else use its full path), and the two required env vars
are shown inline. It's a stdio MCP server, so any MCP-capable client can launch
it.

**Claude Code** (CLI — writes `.mcp.json` / your Claude config):

```sh
claude mcp add jabali \
  --env JABALI_PANEL_URL=https://panel.example:8443/api/v1 \
  --env JABALI_API_TOKEN=jat_… \
  -- jabali-mcp
```

**Claude Desktop** — `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "jabali": {
      "command": "jabali-mcp",
      "env": {
        "JABALI_PANEL_URL": "https://panel.example:8443/api/v1",
        "JABALI_API_TOKEN": "jat_…"
      }
    }
  }
}
```

**OpenAI Codex** (CLI — writes `~/.codex/config.toml`):

```sh
codex mcp add jabali \
  --env JABALI_PANEL_URL=https://panel.example:8443/api/v1 \
  --env JABALI_API_TOKEN=jat_… \
  -- jabali-mcp
```

or edit `~/.codex/config.toml` by hand — note it's **TOML**, and the key is
`mcp_servers` (underscore), not the JSON `mcpServers`:

```toml
[mcp_servers.jabali]
command = "jabali-mcp"

[mcp_servers.jabali.env]
JABALI_PANEL_URL = "https://panel.example:8443/api/v1"
JABALI_API_TOKEN = "jat_…"
```

**OpenCode** — `opencode.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "jabali": {
      "type": "local",
      "command": ["jabali-mcp"],
      "environment": {
        "JABALI_PANEL_URL": "https://panel.example:8443/api/v1",
        "JABALI_API_TOKEN": "jat_…"
      },
      "enabled": true
    }
  }
}
```

**Kimi Code CLI** (writes `~/.kimi/mcp.json`):

```sh
kimi mcp add --transport stdio jabali \
  -e JABALI_PANEL_URL=https://panel.example:8443/api/v1 \
  -e JABALI_API_TOKEN=jat_… \
  -- jabali-mcp
```

**Cursor, Windsurf, Cline, Gemini CLI, and other clients** that use the standard
`mcpServers` JSON take the same entry as Claude Desktop above — a
`{ "command": "jabali-mcp", "env": { … } }` block under `mcpServers` in that
client's config file.

Start read-only. When you want mutations, add `JABALI_MCP_ALLOW_WRITE=1` to the
env (and, at first, `JABALI_MCP_DRY_RUN=1` to watch what it would do).

### Run it on a remote box over SSH

Because it speaks stdio, any of the above can launch it **on a remote host over
SSH** instead of locally — the tool stream tunnels through the SSH pipe. Set the
client's command to `ssh` and pass the remote invocation as its argument, e.g.
for Claude Desktop:

```json
{
  "mcpServers": {
    "jabali-prod": {
      "command": "ssh",
      "args": ["operator@box",
        "JABALI_PANEL_URL=https://localhost:8443/api/v1 JABALI_API_TOKEN=jat_… jabali-mcp"]
    }
  }
}
```

On the box the panel API is reachable at `localhost`, and the token still scopes
everything to its user. Nothing to install client-side beyond an SSH key.

### Fleet (multiple panels)

Set `JABALI_PANELS_FILE` to a JSON array — it overrides the single-panel vars,
and every tool then accepts an optional `panel` argument to target one:

```json
[
  { "name": "prod-a", "url": "https://a:8443/api/v1", "token": "jat_…" },
  { "name": "prod-b", "url": "https://b:8443/api/v1", "token": "jat_…" }
]
```

## Auth model — inherits the panel's tenant isolation

Tools authenticate with the per-user Bearer token. The token acts as its owning
user and the panel enforces ownership on every endpoint, so the server can only
ever reach the resources that token's user owns — a tenant token is confined to
that tenant. That is why a non-admin token is the safe default.

## Safety

This fronts a hosting control plane, so mutation is fenced in four layers:

1. **Read-only by default.** Write tools register only with `JABALI_MCP_ALLOW_WRITE=1`.
2. **Destructive tools require `confirm: true`.** `delete_domain`,
   `delete_dns_record`, `delete_mailbox`, `set_mailbox_password`, and
   `restore_backup` return a preview and act only when re-called with
   `confirm: true`. A model cannot destroy state in one step — the guard against
   a prompt-injected tool call.
3. **Tool hints.** Read tools carry `readOnlyHint`; destructive tools carry
   `destructiveHint` so clients can surface the risk.
4. **Dry-run.** Any write tool accepts `dry_run: true` to return the exact
   request it *would* send without acting; `JABALI_MCP_DRY_RUN=1` forces that
   globally.

TLS verification is always on. To trust a self-hosted panel's private CA, point
`JABALI_CA_FILE` at its bundle — that *adds* the CA, it never disables
verification.

## Tools

**Read (always on):** `list_domains`, `get_domain`, `list_dns_records`,
`list_mailboxes`, `list_forwarders`, `list_applications`, `list_databases`,
`list_backups`, `list_api_tokens`.

**Write (needs `JABALI_MCP_ALLOW_WRITE=1`):** `create_domain`,
`create_dns_record`, `update_dns_record`, `create_mailbox`, `create_forwarder`,
`create_backup` — plus the confirm-gated destructive set: `delete_domain`,
`delete_dns_record`, `delete_mailbox`, `set_mailbox_password`, `restore_backup`.

## Tool generation

Tools are generated, not hand-written, so the surface can't drift from the API:

- `openapi/openapi.yaml` — vendored copy of the panel's spec.
- `openapi/tools.yaml` — curation: which operations are exposed, their tool
  names, `read`/`write` group, and `destructive`/`paginated` flags. An operation
  absent here is not exposed (that keeps `admin/*` and `nic/update` out).
- `internal/gen` joins the two and emits `internal/tools/generated.go`; run it
  with `make gen` or `go generate ./...`.
- A curation entry naming an operation the spec lacks is a hard error (drift
  guard), and `TestGeneratedIsUpToDate` fails if the committed file is stale.

To refresh after the panel API changes: copy the new `openapi.yaml` in, adjust
`tools.yaml`, run `make gen`, review the diff.

## Development

```sh
make build     # build the binary
make gen       # regenerate internal/tools/generated.go
make test      # go test -race ./...
make vet       # go vet ./...
```

## Layout

```
cmd/jabali-mcp/     entry point (stdio MCP server)
cmd/gen-tools/      CLI wrapper over internal/gen
internal/client/    Bearer-token HTTP client + fleet registry
internal/tools/     tools.go (helpers + gate) + generated.go (the tools)
internal/gen/       openapi.yaml + tools.yaml -> generated.go
openapi/            vendored spec + curation
docs/DESIGN.md      architecture, security model, roadmap
```

## Roadmap

- **M1 — read-only MVP** ✅ Bearer client, MCP stdio server, read tools, tests.
- **M2 — gated mutations** ✅ write tools behind an opt-in flag; destructive ops
  confirm-gated.
- **M3 — fleet** ✅ multi-panel registry + `panel` tool argument.
- **Generator** ✅ tools generated from the spec + curation, golden-tested.
- **Dry-run** ✅ per-call `dry_run` + global `JABALI_MCP_DRY_RUN`.
- **Input validation** ✅ generated per-tool checks for the spec's value
  constraints (enum, minLength, min/max) — fail fast before any request.
- **Next:** admin tools behind their own opt-in.

## License

MIT — see [LICENSE](LICENSE).
