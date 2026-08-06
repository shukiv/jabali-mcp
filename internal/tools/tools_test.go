package tools_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shukiv/jabali-mcp/internal/client"
	"github.com/shukiv/jabali-mcp/internal/tools"
)

// fakePanel records the requests the MCP server makes so the tests can assert
// on method/path/auth and control the response.
type fakePanel struct {
	mu    sync.Mutex
	calls []string // "METHOD PATH"
	auth  string
}

func (f *fakePanel) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		f.auth = r.Header.Get("Authorization")
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"total":0,"page":1,"page_size":50}`))
	}
}

func (f *fakePanel) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

// connect stands up the MCP server (with the given options) wired to an
// in-memory transport and returns a connected client session.
func connect(t *testing.T, opts client.Options) *mcp.ClientSession {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "jabali-mcp-test", Version: "test"}, nil)
	tools.Register(srv, opts)

	serverT, clientT := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go func() { _ = srv.Run(ctx, serverT) }()

	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	cs, err := c.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func newOpts(t *testing.T, url string, allowWrite bool) client.Options {
	t.Helper()
	reg, err := client.NewRegistry([]client.Config{{BaseURL: url, Token: "jat_testtoken"}})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return client.Options{Registry: reg, AllowWrite: allowWrite}
}

func toolNames(t *testing.T, cs *mcp.ClientSession) map[string]bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range res.Tools {
		names[tl.Name] = true
	}
	return names
}

func TestReadOnlyModeHidesWriteTools(t *testing.T) {
	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()

	cs := connect(t, newOpts(t, ts.URL, false))
	names := toolNames(t, cs)

	if !names["list_domains"] {
		t.Error("read-only server should expose list_domains")
	}
	for _, w := range []string{"create_domain", "delete_domain", "restore_backup", "delete_dns_record"} {
		if names[w] {
			t.Errorf("read-only server must NOT expose write tool %q", w)
		}
	}
}

func TestWriteModeExposesWriteTools(t *testing.T) {
	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()

	cs := connect(t, newOpts(t, ts.URL, true))
	names := toolNames(t, cs)

	for _, w := range []string{"create_domain", "delete_domain", "restore_backup"} {
		if !names[w] {
			t.Errorf("write server should expose %q", w)
		}
	}
}

func TestReadToolCallsPanelWithBearer(t *testing.T) {
	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()

	cs := connect(t, newOpts(t, ts.URL, false))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_domains", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("call list_domains: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_domains returned error result: %+v", res.Content)
	}
	got := fp.methods()
	if len(got) != 1 || got[0] != "GET /domains" {
		t.Fatalf("expected one GET /domains call, got %v", got)
	}
	if fp.auth != "Bearer jat_testtoken" {
		t.Errorf("expected bearer auth header, got %q", fp.auth)
	}
}

func TestDestructiveToolRequiresConfirm(t *testing.T) {
	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()

	cs := connect(t, newOpts(t, ts.URL, true))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Without confirm: must NOT touch the panel, must return the gate message.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "delete_domain",
		Arguments: map[string]any{"domain_id": "01ABC"},
	})
	if err != nil {
		t.Fatalf("call delete_domain: %v", err)
	}
	text := firstText(res)
	if !strings.Contains(text, "CONFIRMATION REQUIRED") {
		t.Errorf("expected confirmation-required message, got %q", text)
	}
	if n := len(fp.methods()); n != 0 {
		t.Fatalf("unconfirmed destructive call must not hit the panel, got %d calls", n)
	}

	// With confirm: performs the DELETE.
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "delete_domain",
		Arguments: map[string]any{"domain_id": "01ABC", "confirm": true},
	}); err != nil {
		t.Fatalf("confirmed call: %v", err)
	}
	got := fp.methods()
	if len(got) != 1 || got[0] != "DELETE /domains/01ABC" {
		t.Fatalf("confirmed delete should issue DELETE /domains/01ABC, got %v", got)
	}
}

func TestDryRunDoesNotHitPanel(t *testing.T) {
	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()

	cs := connect(t, newOpts(t, ts.URL, true))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Per-call dry_run on an additive tool: previews, never sends.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "create_domain",
		Arguments: map[string]any{"name": "example.com", "dry_run": true},
	})
	if err != nil {
		t.Fatalf("call create_domain dry-run: %v", err)
	}
	text := firstText(res)
	if !strings.Contains(text, "DRY RUN") || !strings.Contains(text, "POST /domains") {
		t.Errorf("expected a dry-run preview naming POST /domains, got %q", text)
	}
	if n := len(fp.methods()); n != 0 {
		t.Fatalf("dry-run must not hit the panel, got %d calls", n)
	}
}

func TestGlobalDryRunForcesPreview(t *testing.T) {
	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()

	opts := newOpts(t, ts.URL, true)
	opts.DryRun = true
	cs := connect(t, opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Even a confirmed destructive call is only previewed under global dry-run.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "delete_domain",
		Arguments: map[string]any{"domain_id": "01ABC", "confirm": true},
	})
	if err != nil {
		t.Fatalf("call delete_domain: %v", err)
	}
	if !strings.Contains(firstText(res), "DRY RUN") {
		t.Errorf("global dry-run should preview even a confirmed delete, got %q", firstText(res))
	}
	if n := len(fp.methods()); n != 0 {
		t.Fatalf("global dry-run must not hit the panel, got %d calls", n)
	}
}

func firstText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}
