package tools

// report_issue: draft or file a GitHub issue on jabali-panel / jabali-mcp when
// a session surfaces a bug or a feature idea. Outward-facing, so it follows the
// project's write discipline: read-only servers only ever produce a prefilled
// issues/new link for the user to review and submit; write mode adds direct
// creation via the gh CLI behind the same two-call confirm gate as destructive
// panel tools, and dry-run forces a preview.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shukiv/jabali-mcp/internal/client"
)

const issueOwner = "shukiv"

// issueRunner executes the argv (argv[0] resolved via PATH) and returns
// trimmed stdout. Package variable so tests can inject a fake.
var issueRunner = func(ctx context.Context, argv []string) (string, error) {
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return "", fmt.Errorf("%s is not installed", argv[0])
	}
	out, err := exec.CommandContext(ctx, bin, argv[1:]...).Output()
	return strings.TrimSpace(string(out)), err
}

// issueSecretPattern is a tripwire, not the safety story: the real guard is
// that both paths end in human review (the confirm preview, or GitHub's form).
var issueSecretPattern = regexp.MustCompile(
	`jat_[A-Za-z0-9_-]{20,}|ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|-----BEGIN `)

// maxIssueURLBody caps the query-escaped body length in the prefilled link;
// GitHub truncates very long URLs. The full body is always in the text result.
const maxIssueURLBody = 6000

// maxDiagnosticsAttach caps the auto-attached diagnostics section of an issue.
const maxDiagnosticsAttach = 3000

func registerIssueTools(s *mcp.Server, reg *client.Registry, allowWrite bool) {
	type ReportIssueIn struct {
		panelArg
		Repo             string `json:"repo" jsonschema:"target repo: jabali-panel for panel/API behavior (wrong responses, unexpected 5xx, ownership bugs), jabali-mcp for MCP tool problems (schemas, gating, tool generation)"`
		Kind             string `json:"kind" jsonschema:"bug or feature"`
		Title            string `json:"title" jsonschema:"one-line issue title"`
		Body             string `json:"body" jsonschema:"markdown body. For bugs include: the tool called, its arguments (redacted), expected vs actual behavior, and the response or error. NEVER include tokens, passwords, or file contents."`
		DiagnoseDomainId string `json:"diagnose_domain_id,omitempty" jsonschema:"optional domain ULID: runs the diagnose_domain probes (config, SSL, DNS, recent errors) and attaches the results to the issue body as evidence"`
		Confirm          bool   `json:"confirm,omitempty" jsonschema:"set true to create the issue directly via the gh CLI (write mode only); otherwise the tool returns a preview plus a prefilled link"`
		DryRun           bool   `json:"dry_run,omitempty" jsonschema:"if true, preview only"`
	}

	desc := "Draft a GitHub issue (bug report or feature request) for jabali-panel or jabali-mcp " +
		"and return a prefilled issues/new link for the user to review and submit."
	anno := roAnno()
	if allowWrite {
		desc = "File a GitHub issue (bug report or feature request) on jabali-panel or jabali-mcp. " +
			"Without confirm:true it returns a preview plus a prefilled link; with confirm:true it " +
			"creates the issue directly via the gh CLI, posting under the user's GitHub identity."
		anno = additiveAnno()
	}
	desc += " Offer this when a panel call returns an unexpected 5xx, behavior contradicts the " +
		"documentation, or the user voices a feature idea. Never put tokens, passwords, or file " +
		"contents in the issue text."

	schema := inferSchema[ReportIssueIn]()
	schema.Properties["repo"].Enum = enumVals("jabali-panel", "jabali-mcp")
	schema.Properties["kind"].Enum = enumVals("bug", "feature")

	mcp.AddTool(s, &mcp.Tool{Name: "report_issue", Description: desc, Annotations: anno, InputSchema: schema},
		func(ctx context.Context, _ *mcp.CallToolRequest, in ReportIssueIn) (*mcp.CallToolResult, any, error) {
			// Defense-in-depth: the schema already enforces these enums.
			if r := vEnum("repo", in.Repo, []string{"jabali-panel", "jabali-mcp"}); r != nil {
				return r, nil, nil
			}
			if r := vEnum("kind", in.Kind, []string{"bug", "feature"}); r != nil {
				return r, nil, nil
			}
			if in.Title == "" || in.Body == "" {
				return errResult("title and body are required"), nil, nil
			}
			if issueSecretPattern.MatchString(in.Title) || issueSecretPattern.MatchString(in.Body) {
				return errResult("refusing: the issue text appears to contain a secret (token or key). Redact it and retry."), nil, nil
			}

			body := in.Body
			diagNote := ""
			if in.DiagnoseDomainId != "" {
				c, err := resolveClient(reg, in)
				if err != nil {
					return errResult(err.Error()), nil, nil
				}
				body += "\n\n## Diagnostics (auto-attached by jabali-mcp)\n\n```json\n" +
					diagnosticsAttachment(ctx, c, in.DiagnoseDomainId) + "\n```"
				diagNote = "\n(diagnostics attached — review them for anything you don't want public)"
			}

			label := "bug"
			if in.Kind == "feature" {
				label = "enhancement"
			}
			link, linkTruncated := issueLink(in.Repo, label, in.Title, body)

			preview := fmt.Sprintf("Issue draft for github.com/%s/%s [%s]\nTitle: %s\n\n%s\n\nPrefilled link (review and submit):\n%s%s",
				issueOwner, in.Repo, in.Kind, in.Title, body, link, diagNote)
			if linkTruncated {
				preview += "\n(body truncated in the link — paste the full body above over it)"
			}

			if !allowWrite {
				return textResult(preview + "\n\nDirect creation requires JABALI_MCP_ALLOW_WRITE=1."), nil, nil
			}
			if globalDryRun || in.DryRun {
				return textResult("DRY RUN — no issue was created.\n" + preview), nil, nil
			}
			if !in.Confirm {
				return textResult("CONFIRMATION REQUIRED — no issue was created.\n" + preview +
					"\n\nRe-call with \"confirm\": true to create it directly (posts under your GitHub identity via gh)."), nil, nil
			}

			out, err := issueRunner(ctx, []string{"gh", "issue", "create",
				"-R", issueOwner + "/" + in.Repo,
				"--title", in.Title,
				"--body", "**Kind:** " + in.Kind + "\n\n" + body})
			if err != nil {
				return errResult("gh issue create failed: " + err.Error() +
					"\n(install gh / run `gh auth login`, or use the prefilled link)\n" + link), nil, nil
			}
			return textResult("Issue created: " + out), nil, nil
		})
}

// diagnosticsAttachment runs the diagnose_domain probes and renders a compact,
// size-capped JSON blob for embedding in an issue body. Probe failures are
// evidence too (often the bug itself), so they are included, and the whole
// attachment goes through the same human review as the rest of the issue.
func diagnosticsAttachment(ctx context.Context, c *client.Client, domainID string) string {
	out, probeErrs := runDomainProbes(ctx, c, domainID, 50)
	if len(probeErrs) > 0 {
		out["probe_errors"] = probeErrs
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "diagnostics unavailable: " + err.Error()
	}
	s := string(b)
	if len(s) > maxDiagnosticsAttach {
		s = s[:maxDiagnosticsAttach] + "\n… (truncated)"
	}
	return s
}

// issueLink builds the prefilled issues/new URL, trimming the body to keep the
// escaped form under maxIssueURLBody. Reports whether it trimmed.
func issueLink(repo, label, title, body string) (string, bool) {
	truncated := false
	b := body
	for len(url.QueryEscape(b)) > maxIssueURLBody {
		r := []rune(b)
		b = string(r[:len(r)*3/4])
		truncated = true
	}
	if truncated {
		b += "\n\n[truncated — see full report]"
	}
	return fmt.Sprintf("https://github.com/%s/%s/issues/new?labels=%s&title=%s&body=%s",
		issueOwner, repo, label, url.QueryEscape(title), url.QueryEscape(b)), truncated
}
