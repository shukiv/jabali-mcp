package tools

// White-box test: injects a fake issueRunner to verify the confirmed create
// path builds the right gh argv without ever executing anything.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shukiv/jabali-mcp/internal/client"
)

func TestReportIssueConfirmedCreateInvokesGh(t *testing.T) {
	orig := issueRunner
	t.Cleanup(func() { issueRunner = orig })

	var gotArgv []string
	issueRunner = func(_ context.Context, argv []string) (string, error) {
		gotArgv = argv
		return "https://github.com/shukiv/jabali-panel/issues/999", nil
	}

	reg, err := client.NewRegistry([]client.Config{{BaseURL: "http://unused", Token: "jat_t"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "t"}, nil)
	Register(srv, client.Options{Registry: reg, AllowWrite: true})

	serverT, clientT := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = srv.Run(context.Background(), serverT) }()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "t"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "report_issue", Arguments: map[string]any{
		"repo": "jabali-panel", "kind": "bug", "title": "5xx on GET /domains", "body": "repro", "confirm": true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	text := ""
	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		text = tc.Text
	}
	if !strings.Contains(text, "Issue created: https://github.com/shukiv/jabali-panel/issues/999") {
		t.Errorf("expected created-issue URL, got %q", text)
	}
	want := []string{"gh", "issue", "create", "-R", "shukiv/jabali-panel", "--title", "5xx on GET /domains"}
	for i, w := range want {
		if i >= len(gotArgv) || gotArgv[i] != w {
			t.Fatalf("argv[%d]: want %q, got %v", i, w, gotArgv)
		}
	}
	if !strings.Contains(gotArgv[len(gotArgv)-1], "**Kind:** bug") {
		t.Errorf("body should carry the kind header, got %q", gotArgv[len(gotArgv)-1])
	}
}
