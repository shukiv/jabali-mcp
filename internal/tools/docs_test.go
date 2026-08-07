package tools_test

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/shukiv/jabali-mcp/internal/gen"
)

// TestDocsUpToDate fails when docs/TOOLS.md differs from what the doc
// generator produces — run `make docs` (or `make gen`) and commit.
func TestDocsUpToDate(t *testing.T) {
	want, err := gen.GenerateDocs("../../openapi/openapi.yaml", "../../openapi/tools.yaml", "../../openapi/admin-tools.yaml")
	if err != nil {
		t.Fatalf("generate docs: %v", err)
	}
	got, err := os.ReadFile("../../docs/TOOLS.md")
	if err != nil {
		t.Fatalf("read docs/TOOLS.md: %v", err)
	}
	if string(got) != string(want) {
		t.Error("docs/TOOLS.md is stale — run `make docs` and commit the result")
	}
}

// TestDocsCoverEveryTool asserts every registered tool — generated AND
// hand-written — has a heading in docs/TOOLS.md. This is what keeps the
// hand-written composite section honest.
func TestDocsCoverEveryTool(t *testing.T) {
	md, err := os.ReadFile("../../docs/TOOLS.md")
	if err != nil {
		t.Fatalf("read docs/TOOLS.md: %v", err)
	}

	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()

	opts := newOpts(t, ts.URL, true)
	opts.Admin = true
	cs := connect(t, opts)
	for name := range toolNames(t, cs) {
		if !strings.Contains(string(md), "### "+name+"\n") {
			t.Errorf("tool %q is registered but undocumented in docs/TOOLS.md", name)
		}
	}
}
