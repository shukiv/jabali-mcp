# jabali-mcp — Handover

Everything you need to pick this project up: what it is, how it works, every
decision and why, the traps we already hit, and what's next. Written 2026-08-07
at **v0.2.0**.

- **Repo:** `github.com/shukiv/jabali-mcp` (**public** since 2026-08-07;
  history secret-scanned clean before the flip), local checkout at
  `/home/shuki/projects/jabali-mcp`.
- **What it is:** a Model Context Protocol (stdio) server exposing the
  [Jabali Panel](https://github.com/shukiv/jabali-panel) REST API as MCP tools,
  so an AI assistant can drive panel operations (domains, DNS, mail, apps,
  databases, backups, logs) in natural language.
- **License:** AGPL-3.0, deliberately matching Jabali Panel.
- **Toolchain:** Go 1.25, module `github.com/shukiv/jabali-mcp`,
  MCP SDK `github.com/modelcontextprotocol/go-sdk` v1.2.0.

## Current state (v0.3.0)

- 32 read tools, 19 additive write tools, 16 confirm-gated destructive tools,
  7 admin tools — all generated from the vendored OpenAPI spec + a curation
  file (see "Generator pipeline") — plus the hand-written composite
  `diagnose_domain` (internal/tools/composite.go).
- The v0.3.0 expansion (SSL, cron, DB users/grants, mailbox depth, SSH keys,
  PHP settings, files read-only, diagnostics) was **handler-verified**: every
  request body and auth scope checked against panel handler source at deployed
  commit `01c2adaf`, not the spec (the spec's request bodies are skeletal).
  Notable: `/domains/{id}/ssl/renew` and `/ssl/retry` are RequireAdmin despite
  the tenant-looking paths → shipped as `admin_renew_ssl` / `admin_retry_ssl`.
- InputSchema now carries the spec's enum/min/max/minLength (generator grafts
  them onto the SDK-inferred schema), so clients see constraints in tools/list
  and the SDK rejects violations at the protocol level; the runtime v*
  validators remain as defense-in-depth.
- `docs/TOOLS.md` is generated (`make docs`); two tests guard it:
  TestDocsUpToDate (golden) and TestDocsCoverEveryTool (every registered tool —
  including hand-written ones — must have a heading).
- `scripts/smoke` is a live zero-mutation smoke client (reads + dry-run
  previews + a schema-rejection probe). Run before every tag:
  `make build && go run ./scripts/smoke ./jabali-mcp`.
- Registered in the operator's Claude Code as a pinned-version launch:
  `go run github.com/shukiv/jabali-mcp/cmd/jabali-mcp@v0.3.0` (the npx analog
  for a compiled Go tool; ~1.6 s first launch, ~0.6 s cached). The registration
  still carries `GOPRIVATE=github.com/shukiv/*` — harmless now the repo is
  public, drop it on the next re-register.
- Panel-side companions are **merged to jabali2 main and deployed to
  testserver** (see "Panel-side integration").
- Tests: unit + in-memory MCP round-trip + golden drift tests, all green
  (`make test`). Build and vet clean.

## Architecture

### Auth model — the single most important design fact

Tools authenticate with a **per-user Bearer token** (`jat_…`, minted on the
panel's API Tokens page, sent as `Authorization: Bearer`). The panel enforces
ownership on every endpoint (`claims.UserID == resource.UserID || is_admin`),
so the MCP server **inherits the panel's tenant isolation** and reimplements no
auth, scoping, or validation of its own. A tenant token is confined to that
tenant no matter what the model asks for.

We originally planned to use the panel's HMAC automation API (scoped
`automation_token`, ADR-0093) and pivoted after reading the panel's OpenAPI
spec: the Bearer path is documented, simpler, and already ownership-scoped.
The HMAC path remains available if machine-to-machine scope granularity is ever
needed, but nothing here uses it.

### Process model

Stdio MCP server. An MCP client launches the binary; there is no daemon. The
startup line goes to **stderr** only (stdout is the MCP stream — never print to
it). Subcommands: `init` (interactive setup), `update` (self-update via
`go install`), `version`, `help`; no args = serve.

### Package layout

```
cmd/jabali-mcp/     entry + subcommands (main.go, init.go, update.go)
cmd/gen-tools/      CLI wrapper over internal/gen (-spec -curation -out -group)
internal/client/    client.go  — Bearer HTTP client (TLS always verified)
                    registry.go — fleet registry + Options + env resolution
internal/tools/     tools.go   — helpers, gates, validators, Register()
                    generated.go / generated_admin.go — GENERATED, do not edit
internal/gen/       the generator library (spec + curation -> Go)
openapi/            openapi.yaml (vendored spec), tools.yaml (tenant curation),
                    admin-tools.yaml (admin curation)
docs/DESIGN.md      original design notes
```

### Generator pipeline (the reuse engine)

Tools are generated, never hand-written, so the surface cannot drift from the
API:

1. `openapi/openapi.yaml` — vendored copy of the panel's spec (edited to add
   request bodies where the upstream spec is skeletal).
2. `openapi/tools.yaml` / `openapi/admin-tools.yaml` — curation: an allow-list
   mapping `"<METHOD> <path>"` to a tool name plus flags
   (`group: read|write`, `destructive: true`, `paginated: true`). An operation
   absent from curation is **not exposed** — that is how `nic/update` stays out
   and how admin stays in its own file.
3. `internal/gen` joins the two and emits Go: input structs (jsonschema
   description tags from the spec), per-field validation (enum / minLength /
   min / max checked before any request), path-param URL escaping, query-param
   assembly (`url.Values`), body assembly (present-only semantics), and the
   right runtime gate (`runRead` / `runWrite`).
4. `make gen` regenerates both files; `TestGeneratedIsUpToDate` (golden) fails
   CI if the committed output is stale; a curation entry naming a missing
   operation is a hard generator error.

Refresh flow after a panel API change: copy the new spec in, adjust curation,
`make gen`, review the diff.

### Safety gates (four layers, in runWrite order)

1. **Read-only default** — write tools register only with
   `JABALI_MCP_ALLOW_WRITE=1`; admin tools additionally need
   `JABALI_MCP_ADMIN=1` *and* an admin token (the panel's `RequireAdmin`
   returns 403 for non-admin tokens regardless of the flag).
2. **Dry-run** — every write tool accepts `dry_run: true` and returns exactly
   the METHOD/path/body it would send; `JABALI_MCP_DRY_RUN=1` forces this
   globally. Dry-run is checked **before** the confirm gate so you can preview
   a destructive call without confirming it.
3. **Confirm gate** — destructive tools (`delete_*`, `set_mailbox_password`,
   `restore_backup`, `admin_run_updates`) return a preview and act only when
   re-called with `confirm: true`. A prompt-injected single tool call cannot
   destroy state.
4. **Tool annotations** — `readOnlyHint` / `destructiveHint` so clients can
   surface risk in their UI.

TLS verification is **always on**. `JABALI_CA_FILE` *adds* a private CA to the
trust pool; there is no insecure-skip option and one must never be added
(an `InsecureSkipVerify` draft was removed on security review — hard rule).

### Configuration resolution

`JABALI_PANELS_FILE` (fleet JSON array) → single-panel env
(`JABALI_PANEL_URL` + `JABALI_API_TOKEN`, optional `JABALI_PANEL_NAME`,
`JABALI_CA_FILE`) → default `~/.config/jabali-mcp/panels.json` (written 0600 by
`jabali-mcp init`, which verifies each token against `GET /domains` before
saving). In fleet mode every tool takes an optional `panel` argument; omitted =
first panel.

## Decision log (all operator-confirmed)

| Decision | Why |
|---|---|
| Go, not TypeScript | single binary, no runtime deps, same language as the panel |
| GitHub, private repo (made public 2026-08-07) | GitHub is the panel's source of truth; MCP follows |
| Bearer token, not HMAC automation API | documented, simpler, ownership already enforced server-side |
| Tools generated from OpenAPI | zero drift; new tools are curation entries, not code |
| Read-only by default, write opt-in, confirm for destructive | fronts a hosting control plane; prompt-injection is the threat model |
| Admin + tenant in the **same binary**, separate opt-in group | one artifact to ship; blast radius controlled by flag + token role |
| AGPL-3.0 | match Jabali Panel |
| Client registration pinned to a tag (`go run …@vX.Y.Z`) | reproducible, updatable by re-registering; no stale local binary |
| Panel-side setup UI (not a config wizard app) | users mint the token in the panel anyway; the page assembles client config in-browser and **never sends the token to the server** |

## Tool surface (v0.3.0)

75 tools; the full generated reference is `docs/TOOLS.md` (`make docs`).
Groups: 32 read (+ composite `diagnose_domain`), 19 write (`ALLOW_WRITE`),
16 destructive (`ALLOW_WRITE` + `confirm: true` — every delete/revoke/
rotate-password plus `set_mailbox_password`, `restore_backup`, `disable_ssl`),
7 admin (`ADMIN` + admin token; writes also need `ALLOW_WRITE`;
`admin_run_updates` confirm-gated).

Secret-bearing tools (by panel design, reveal-once): `create_database_user`,
`rotate_database_password`, `rotate_mailbox_password` return plaintext
passwords; `preview_file` can read files containing credentials
(wp-config.php). All flagged in their descriptions.

`update_domain`'s field set was **verified against the panel handler**
(`updateDomainRequest` in `panel-api/internal/api/domains.go`), not just the
spec: `is_enabled`, `webmail_enabled`, `temp_url_enabled`, `ssl_mode`
(`le`/`self`/`none` — the handler rejects `custom`, and `shared` is
operator-managed), `redirect_all_to`, `redirect_all_type`
(`301`/`302`/`307`/`308`). Admin-only fields (doc_root, nginx directives/rules)
are deliberately not exposed.

## Release process

1. Bump `version` in `cmd/jabali-mcp/main.go` (the default tracks the latest
   tag because `go run …@tag` does not apply ldflags).
2. Commit, `git tag vX.Y.Z`, `git push origin main vX.Y.Z`.
3. Re-register clients on the new pin:
   `claude mcp add jabali --scope user -- go run github.com/shukiv/jabali-mcp/cmd/jabali-mcp@vX.Y.Z`
4. Users on installed binaries run `jabali-mcp update` (or
   `update --ref vX.Y.Z`).

History: v0.1.0 (first pinned release), v0.2.0 (update_domain,
install_application, create_database), v0.3.0 (39 handler-verified tools —
SSL/cron/DB-users/mail-depth/SSH/PHP/files, `diagnose_domain` composite,
InputSchema constraints, generated docs/TOOLS.md, scripts/smoke).

## Panel-side integration (jabali2)

All merged to `jabali-panel` main (`01c2adaf`) and deployed to testserver:

- **PR #960 — MCP setup guide UI** (`MCPSetupGuide.tsx` + a tab on the tenant
  API Tokens page): per-client setup snippets, and a **Read-only / Read-write**
  toggle that injects `JABALI_MCP_ALLOW_WRITE=1` into the generated config.
  Config is assembled in the browser; the token never leaves the page.
- **PR #961 — `GET /logs/tail`** (`panel-api/internal/api/logs.go`): last-N-lines
  snapshot of a domain's nginx access/error log (web logs were
  WebSocket-stream-only before, which an MCP tool can't consume). Reuses the
  vetted `logFilePathForDomain` + `isSafeDomainSegment`, ownership → 404,
  `lines` clamped 1..2000, missing file → 200 with empty lines. Also documents
  `GET /mail/logs` for the MCP.
- **PR #955** — closed as superseded by #961 (one-line OpenAPI YAML quote fix).

**Trap fixed at merge time (`01c2adaf`):** the panel has **two** OpenAPI files.
`docs/api/openapi.yaml` is the human/docs spec; the CI coverage golden
(`TestOpenAPICoverage` in `panel-api/internal/app`) gates on
`panel-api/internal/api/openapi.yaml`. A new route must be documented in the
**internal** one or the app-package test fails.

## Traps already hit (don't rediscover these)

- **go-sdk `jsonschema` struct tag is description-only** — confirmed in
  `google/jsonschema-go` (`infer.go:206`, `fs.Description = tag`). You cannot
  ride enum/min/max constraints on the tag; that's why the generator emits
  explicit `vEnum`/`vMinLen`/`vMin`/`vMax` checks in handlers. The SDK panics
  on an **empty** tag, so the generator falls back to the field name when the
  spec has no description. Since v0.3.0 the generator ALSO sets
  `Tool.InputSchema` explicitly (SDK-inferred via `jsonschema.For` + grafted
  constraints) — a pre-set schema is respected by `mcp.AddTool` (`setSchema`
  in the SDK), and violations then fail the `tools/call` request itself
  (JSON-RPC error), not an in-band IsError result. Tests must expect that.
- **The panel spec is skeletal** — request bodies and query params are mostly
  absent from `panel-api/internal/api/openapi.yaml`. Handler source is truth;
  the vendored spec here carries hand-authored bodies verified against
  handlers. When refreshing the spec, MERGE selectively — a wholesale copy
  clobbers those bodies.
- **The jabali2 checkout may sit on a feature branch** — for spec/handler
  verification, read from the deployed commit (`git show <deployed-sha>:…`),
  not the working tree (bit us: the checkout predated the `/logs/tail` merge).
- **Generator body semantics:** optional string fields are skipped when empty,
  so "send empty string to clear a field" API semantics are unreachable through
  MCP tools — don't document them as reachable (bit us on
  `update_domain.redirect_all_to`).
- **Upstream spec YAML:** flow-style descriptions containing `{id}` are invalid
  YAML unless quoted (hit on `poll /applications/{id}`; fixed in the vendored
  copy and upstream via #961).
- **Private repo mechanics (historical):** while the repo was private, every
  `go run`/`go install` path needed `GOPRIVATE=github.com/shukiv/*` + git auth.
  Public since 2026-08-07 — plain `go install` works; GOPRIVATE removed from
  `update` and the README. Note: the Go module proxy can lag a few minutes
  behind a freshly pushed tag.
- **Never `InsecureSkipVerify`** — `JABALI_CA_FILE` adds a CA instead. Security
  hardening is never relaxed for convenience.
- **Repo hooks (jabali2 side):** commit messages containing the literal string
  `--no-verify` are rejected; `gh pr close`/`comment` need explicit operator
  approval markers; `gh pr merge` is never allowed (merge locally and push).

## Verification habits that caught real bugs

- Verify tool field sets against the **panel handler source**, not just the
  spec (spec had no PATCH body at all; the handler's allowlist is truth).
- Verify deploys via the **user-facing path**: SPA 200, the new route returning
  401-unauthenticated (proves it exists — 404 means it doesn't), and the
  embedded `index.html` asset hash matching the on-disk `dist/` (panel-api
  go:embeds `index.html`; `/assets` are served from disk — they can desync).
- In-memory MCP transport (`mcp.NewInMemoryTransports`) exercises the real
  register/gate/serialize path in unit tests without a client.

## Ideas / next steps

Done in v0.3.0: InputSchema constraints, `diagnose_domain`, `enable_ssl`,
generated `docs/TOOLS.md`, live smoke script, `update --check` version check.

Remaining:

- CLI mode (tools as subcommands for SSH-only users).
- MCP resources/prompts: expose panel runbooks as resources, canned prompts.

Done in v0.4.0: `report_issue` (hand-written, internal/tools/issues.go) —
drafts GitHub issues for jabali-panel/jabali-mcp as prefilled issues/new
links; write mode + `confirm: true` files directly via the gh CLI. Repo list
is a closed enum (injection guard), secret-pattern tripwire on the text,
read-only servers never create anything.

Public release: DONE (2026-08-07) — repo public, tagged releases build
binaries via `.github/workflows/release.yml`, GOPRIVATE dropped everywhere.

## Operational notes

- testserver runs deployed main `01c2adaf`; previous binaries kept at
  `/root/deploy-01c2adaf/*.prev` for rollback. A demo tenant account exists on
  testserver for UI testing (credentials in the operator's notes, not in this
  repo).
- The panel fleet auto-updates daily at 04:30 from the stable channel; the MCP
  server itself is versioned and updated independently (tags + `update`).
