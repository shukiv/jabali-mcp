# jabali-mcp — design

## Goal

Expose the Jabali Panel automation API as MCP tools so an LLM agent can perform
panel operations. Optimise for **reuse** (the automation API + OpenAPI already
exist) and **safety** (this fronts a root-capable control plane).

## Architecture

```
LLM client (Claude, …)
      │  MCP (stdio)
      ▼
jabali-mcp
      │  HTTPS + X-Jabali-Signature (HMAC-SHA256, ADR-0093)
      ▼
Jabali Panel automation API  ──►  RequireScope ──►  handlers
```

- **Tools are generated from `docs/api/openapi.yaml`** (jabali-panel repo). Each
  operation carrying an automation scope becomes one tool. Regeneration keeps
  the MCP surface in lockstep with the API.
- **Auth**: the server signs each request with an `automation_token`
  (HMAC-SHA256 over `METHOD || PATH || ts || sha256(BODY)`, 5-minute skew).
- **Idempotency**: reuse the panel's `automation_operation` replay mechanism so
  a retried tool call is safe.

## Security (the dominant concern)

1. **Scope-per-tool, minimally scoped token.** Every tool declares its required
   scope; the server's credential is granted only the scopes for the tools it
   exposes. `write:everything` is never granted to an MCP credential.
2. **Read-only default.** The default build registers only `read:*` tools. The
   write set is a separate, opt-in registration.
3. **Confirmation gate on destructive tools.** Delete user, restore, and DNS/SSL
   writes require an explicit confirmation turn — they are never invoked purely
   on model output. Prompt injection (model reads tenant-controlled domain
   names / file contents / logs, then is steered to mutate) is the threat model.
4. **No secret leakage.** Tool outputs are audited so no token id, secret, or
   password hash is ever returned (the panel already marks these `json:"-"`).
5. **Audit trail.** Every tool call is an automation-API request, so it lands in
   the panel's existing automation operation log — no separate audit path.

## Deployment shapes

- **Operator-local (phase 1–2):** runs on the operator's machine, one panel,
  one credential. Smallest blast radius. Ship as a single static binary.
- **Fleet (phase 3):** one server fronting many panels; per-panel credential
  custody; for bulk migration/DNS/SSL work. Deferred — larger blast radius.

## Milestones

1. **M1 — read-only MVP.** openapi→tools generator for `read:*` operations; HMAC
   client; MCP stdio server; config loading. Verify against a live panel's
   automation API with a read-scoped token.
2. **M2 — gated mutations.** Write tools behind an opt-in scope set +
   confirmation gating + a dry-run mode.
3. **M3 — fleet.** Multi-panel credential store; panel selection as a tool
   argument or per-session binding.

## Open questions

- MCP Go SDK choice + version (verify current API via live docs before wiring).
- Generator: hand-rolled over `openapi.yaml`, or an existing openapi→MCP tool
  adapted to Go.
- Confirmation-gate UX in MCP (elicitation vs. a two-call confirm token).
