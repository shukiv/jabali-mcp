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

## Next

- Generate tools from `docs/api/openapi.yaml` (`internal/gen`) so the surface
  can't drift from the API.
- Per-tool input validation against the OpenAPI request schemas.
- A dry-run mode (return the request that *would* be sent) for write tools.
- Admin tools (`/admin/*`) behind their own explicit opt-in flag.

## Open questions

- Confirmation-gate UX: the current two-call `confirm:true` vs. MCP elicitation.
- Whether to expose admin endpoints at all, and under what extra guard.
