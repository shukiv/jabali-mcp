package tools_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callReportIssue(t *testing.T, cs *mcp.ClientSession, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "report_issue", Arguments: args})
	if err != nil {
		t.Fatalf("call report_issue: %v", err)
	}
	return res
}

func TestReportIssueRegistersInBothModes(t *testing.T) {
	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()

	for _, write := range []bool{false, true} {
		cs := connect(t, newOpts(t, ts.URL, write))
		if !toolNames(t, cs)["report_issue"] {
			t.Errorf("report_issue should register with allowWrite=%v", write)
		}
	}
}

func TestReportIssueReadOnlyReturnsLinkOnly(t *testing.T) {
	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()

	cs := connect(t, newOpts(t, ts.URL, false))
	// Even with confirm:true, read-only mode must never create — link only.
	res := callReportIssue(t, cs, map[string]any{
		"repo": "jabali-mcp", "kind": "bug",
		"title": "tail_web_log misparses lines", "body": "repro: …", "confirm": true,
	})
	text := firstText(res)
	if res.IsError {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "https://github.com/shukiv/jabali-mcp/issues/new?labels=bug") {
		t.Errorf("expected prefilled link, got %q", text)
	}
	if !strings.Contains(text, "requires JABALI_MCP_ALLOW_WRITE=1") {
		t.Errorf("read-only mode should say creation is unavailable, got %q", text)
	}
	if strings.Contains(text, "Issue created") {
		t.Errorf("read-only mode must never create an issue")
	}
}

func TestReportIssueWriteWithoutConfirmPreviews(t *testing.T) {
	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()

	cs := connect(t, newOpts(t, ts.URL, true))
	res := callReportIssue(t, cs, map[string]any{
		"repo": "jabali-panel", "kind": "feature", "title": "t", "body": "b",
	})
	text := firstText(res)
	if !strings.Contains(text, "CONFIRMATION REQUIRED") ||
		!strings.Contains(text, "issues/new?labels=enhancement") {
		t.Errorf("expected confirm gate + enhancement link, got %q", text)
	}
}

func TestReportIssueBlocksSecrets(t *testing.T) {
	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()

	cs := connect(t, newOpts(t, ts.URL, true))
	res := callReportIssue(t, cs, map[string]any{
		"repo": "jabali-mcp", "kind": "bug", "title": "t",
		"body": "my token is jat_abcdefghijklmnopqrstuvwx leaking", "confirm": true,
	})
	if !res.IsError || !strings.Contains(firstText(res), "secret") {
		t.Errorf("secret-bearing body must be refused, got IsError=%v %q", res.IsError, firstText(res))
	}
}

func TestReportIssueAttachesDiagnostics(t *testing.T) {
	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()

	cs := connect(t, newOpts(t, ts.URL, false))
	res := callReportIssue(t, cs, map[string]any{
		"repo": "jabali-panel", "kind": "bug", "title": "SSL stuck pending",
		"body": "repro", "diagnose_domain_id": "01D",
	})
	text := firstText(res)
	if !strings.Contains(text, "## Diagnostics (auto-attached by jabali-mcp)") {
		t.Errorf("expected diagnostics section, got %q", text)
	}
	if !strings.Contains(text, "review them for anything you don't want public") {
		t.Errorf("expected the review warning, got %q", text)
	}
	if len(fp.methods()) == 0 {
		t.Error("diagnostics attachment should have probed the panel")
	}
}

func TestReportIssueGlobalDryRunPreviews(t *testing.T) {
	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()

	opts := newOpts(t, ts.URL, true)
	opts.DryRun = true
	cs := connect(t, opts)
	res := callReportIssue(t, cs, map[string]any{
		"repo": "jabali-mcp", "kind": "bug", "title": "t", "body": "b", "confirm": true,
	})
	if !strings.Contains(firstText(res), "DRY RUN — no issue was created") {
		t.Errorf("global dry-run must preview, got %q", firstText(res))
	}
}
