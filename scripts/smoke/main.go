// Command smoke drives a built jabali-mcp binary over stdio MCP and exercises
// the tool surface against the live panel the local config points at. Zero
// mutation by construction: reads only, plus writes forced into preview by
// JABALI_MCP_DRY_RUN=1 and a destructive call without confirm.
//
// Run it before tagging a release:
//
//	make build && go run ./scripts/smoke ./jabali-mcp
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type check struct {
	tool string
	args map[string]any
	want string // substring expected in the text result (empty = any non-error)
}

func main() {
	bin := os.Args[1]
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "JABALI_MCP_ALLOW_WRITE=1", "JABALI_MCP_DRY_RUN=1")
	cmd.Stderr = os.Stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "smoke", Version: "0"}, nil)
	sess, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		fmt.Println("FATAL connect:", err)
		os.Exit(1)
	}
	defer sess.Close()

	tools, err := sess.ListTools(ctx, nil)
	if err != nil {
		fmt.Println("FATAL list tools:", err)
		os.Exit(1)
	}
	fmt.Printf("tools registered: %d\n", len(tools.Tools))

	// Discover a real domain ULID for the domain-scoped checks.
	domainID := ""
	if res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "list_domains", Arguments: map[string]any{}}); err == nil && !res.IsError {
		txt := text(res)
		// crude ULID sniff from the JSON — first "id":"01..."
		if i := strings.Index(txt, `"id":"01`); i >= 0 {
			domainID = txt[i+6 : i+6+26]
		}
	}
	fmt.Println("probe domain:", domainID)

	checks := []check{
		{tool: "whoami", args: map[string]any{}},
		{tool: "get_disk_usage", args: map[string]any{}},
		{tool: "list_cron_jobs", args: map[string]any{}},
		{tool: "list_ssh_keys", args: map[string]any{}},
		{tool: "list_php_versions", args: map[string]any{}},
		{tool: "list_app_catalog", args: map[string]any{}},
		{tool: "list_database_users", args: map[string]any{}},
		{tool: "list_activity", args: map[string]any{"page_size": 1}},
	}
	if domainID != "" {
		checks = append(checks,
			check{tool: "get_ssl_status", args: map[string]any{"domain_id": domainID}},
			check{tool: "get_php_settings", args: map[string]any{"domain_id": domainID}},
			check{tool: "enable_ssl", args: map[string]any{"domain_id": domainID}, want: "DRY RUN"},
			check{tool: "delete_cron_job", args: map[string]any{"cron_id": "01SMOKETESTFAKEID000000000"}, want: "DRY RUN"},
		)
	}
	// Validation check: bad enum must be rejected by the SDK against the
	// enriched InputSchema (protocol-level error), without any request.
	checks = append(checks, check{
		tool: "grant_database_access",
		args: map[string]any{"database_user_id": "x", "database_id": "x", "grant_level": "banana"},
		want: "banana does not equal any of",
	})

	pass, fail := 0, 0
	for _, c := range checks {
		res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: c.tool, Arguments: c.args})
		status := "PASS"
		detail := ""
		switch {
		case err != nil && c.want != "" && strings.Contains(err.Error(), c.want):
			detail = trunc(err.Error(), 100) // expected protocol-level rejection
		case err != nil:
			status, detail = "FAIL", err.Error()
		case c.want != "" && !strings.Contains(text(res), c.want):
			status, detail = "FAIL", trunc(text(res), 120)
		case c.want == "" && res.IsError:
			status, detail = "FAIL", trunc(text(res), 120)
		default:
			detail = trunc(text(res), 80)
		}
		if status == "PASS" {
			pass++
		} else {
			fail++
		}
		fmt.Printf("%-4s %-24s %s\n", status, c.tool, detail)
	}
	fmt.Printf("\n%d pass, %d fail\n", pass, fail)
	if fail > 0 {
		os.Exit(1)
	}
}

func text(r *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
