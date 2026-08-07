# jabali-mcp — design

## Goal

Expose the Jabali Panel REST API as MCP tools so an LLM agent can perform panel
operations. Optimise for **reuse** (the REST API + OpenAPI already exist) and
**safety** (this fronts a hosting control plane).

## Architecture

```
LLM client (Claude, …)
      │  MCP (stdio)
      ▼
jabali-mcp   ── internal/tools (read.go, write.go)
      │  HTTPS  Authorization: Bearer jat_…
      ▼
Jabali Panel REST API  ──►  ownership check (claims.UserID == resource.UserID | is_admin)
```

- **Auth = per-user Bearer token** (`jat_…`). The token acts as its user and the
  panel enforces ownership server-side, so the MCP inherits the panel's tenant
  isolation with no extra work. (An earlier draft assumed the HMAC automation
  API; the documented Bearer path in `docs/api/openapi.yaml` is simpler and
  already ownership-scoped, so we use it.)
- **Tools** are hand-written from the OpenAPI spec today, one per operation.
  Generating them from the spec is the next step (`internal/gen`).
- **Fleet** is a registry of named clients; a tool's optional `panel` argument
  selects one, defaulting to the first configured.

## Security

1. **Non-admin token, per-user isolation.** Use a tenant token; the panel
   confines it to that tenant. An admin token is is_admin and far more
   dangerous — avoid unless the task genuinely needs it.
2. **Read-only default.** Write tools register only when
   `JABALI_MCP_ALLOW_WRITE=1`.
3. **Confirm gate on destructive tools.** delete_*, set_mailbox_password, and
   restore_backup return a preview and act only on a second call with
   `confirm: true`. This is the guard against a prompt-injected destructive
   call — the model reads tenant-controlled data (domain names, records) and
   could be steered to delete; one call can't.
4. **TLS always verified.** Trust a private CA via `JABALI_CA_FILE` (adds the
   CA); never disable verification.
5. **No secret leakage.** `list_api_tokens` returns metadata only — the panel
   never returns the token secret (`json:"-"`).
6. **Audit trail.** Every tool call is a REST request in the panel's own logs.

## Deployment shapes

- **Operator-local (default):** one panel, one token, stdio server on the
  operator's machine. Smallest blast radius.
- **Fleet:** `JABALI_PANELS_FILE` with several panels; tools take a `panel`
  argument. For bulk cross-panel work. Larger blast radius — pair with
  read-only or per-panel non-admin tokens.

## Milestones

- **M1 read-only MVP** ✅ — Bearer client, MCP stdio server, read tools, tests.
- **M2 gated mutations** ✅ — write tools behind the allow-write flag;
  destructive ops confirm-gated; readOnly/destructive hints.
- **M3 fleet** ✅ — multi-panel registry + `panel` tool argument.

## Done since M1–M3

- Tools generated from `openapi/openapi.yaml` + `openapi/tools.yaml`
  (`internal/gen`), golden-tested against drift.
- Dry-run mode (per-call `dry_run` + global `JABALI_MCP_DRY_RUN`).
- Per-tool input validation: the generator emits checks for the value
  constraints the spec declares (enum, minLength, minimum/maximum). The SDK
  already enforces types + required-presence from the inferred schema; these add
  the value constraints and fail fast with a precise message before any request.
  (The `jsonschema` struct tag is description-only in the SDK, so the constraints
  are enforced in the handler rather than encoded in the schema.)

- `jabali-mcp init`: interactive setup wizard that verifies each token against
  the panel and writes a `0600` `panels.json` to the default config dir, which
  the server auto-discovers (no env needed after setup). Config resolution:
  `JABALI_PANELS_FILE` → single-panel env → default `panels.json`.

## Done since (v0.3.0)

- Admin tools (`/admin/*`) behind `JABALI_MCP_ADMIN=1` + an admin token —
  same binary, separate registration group.
- InputSchema enrichment: the generator grafts the spec's enum/min/max/
  minLength onto the SDK-inferred schema; clients see constraints in
  tools/list and the SDK rejects violations before the handler.
- 39 handler-verified tools (SSL, cron, DB users/grants, mailbox depth, SSH
  keys, PHP settings, read-only files, diagnostics) + the hand-written
  composite `diagnose_domain`.
- Generated tool reference (`docs/TOOLS.md`, `make docs`) with drift tests.

## Open questions

- Confirmation-gate UX: the current two-call `confirm:true` vs. MCP elicitation.
