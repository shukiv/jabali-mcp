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
)

// docsSite serves a fake llms.txt + doc pages and records request auth headers.
func docsSite(t *testing.T, evilURL string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var auths []string
	mux := http.NewServeMux()
	record := func(r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
	}
	mux.HandleFunc("/llms.txt", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_, _ = w.Write([]byte("# Jabali Panel docs\n\n" +
			"- [MTA-STS setup](/docs/mta-sts.md): enforce TLS for mail\n" +
			"- [Backup destinations](/docs/backups.md): S3, SFTP, local\n" +
			"- [Evil page](" + evilURL + "/pwned.md): off-origin\n"))
	})
	mux.HandleFunc("/docs/mta-sts.md", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_, _ = w.Write([]byte("# MTA-STS\n\nIntro text.\n\n## Enabling MTA-STS\n\nToggle mta_sts_enabled on the domain.\n\n## Unrelated\n\nOther stuff.\n"))
	})
	mux.HandleFunc("/docs/backups.md", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_, _ = w.Write([]byte("# Backups\n\nDestinations: S3, SFTP.\n"))
	})
	return httptest.NewServer(mux), &auths
}

func callSearchDocs(t *testing.T, cs *mcp.ClientSession, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "search_docs", Arguments: args})
	if err != nil {
		t.Fatalf("call search_docs: %v", err)
	}
	return res
}

func TestSearchDocsFindsRelevantSections(t *testing.T) {
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("off-origin URL must never be fetched")
	}))
	defer evil.Close()
	site, auths := docsSite(t, evil.URL)
	defer site.Close()
	t.Setenv("JABALI_DOCS_URL", site.URL)

	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()
	cs := connect(t, newOpts(t, ts.URL, false))

	res := callSearchDocs(t, cs, map[string]any{"query": "enable MTA-STS"})
	text := firstText(res)
	if res.IsError {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Enabling MTA-STS") || !strings.Contains(text, "mta_sts_enabled") {
		t.Errorf("expected the relevant doc section, got %q", text)
	}
	for _, a := range *auths {
		if a != "" {
			t.Fatalf("docs fetches must be anonymous; saw Authorization %q", a)
		}
	}
	if len(fp.methods()) != 0 {
		t.Error("search_docs must not touch the panel API")
	}
}

func TestSearchDocsFetchSinglePageAndOriginGuard(t *testing.T) {
	site, _ := docsSite(t, "http://127.0.0.1:1")
	defer site.Close()
	t.Setenv("JABALI_DOCS_URL", site.URL)

	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()
	cs := connect(t, newOpts(t, ts.URL, false))

	// Full page by path.
	res := callSearchDocs(t, cs, map[string]any{"query": "x", "doc": "/docs/backups.md"})
	if !strings.Contains(firstText(res), "Destinations: S3, SFTP.") {
		t.Errorf("expected full page, got %q", firstText(res))
	}

	// Off-origin doc reference refused.
	res = callSearchDocs(t, cs, map[string]any{"query": "x", "doc": "http://127.0.0.1:1/pwned.md"})
	if !res.IsError || !strings.Contains(firstText(res), "outside the docs origin") {
		t.Errorf("off-origin doc must be refused, got IsError=%v %q", res.IsError, firstText(res))
	}
}

func TestSearchDocsIndexUnavailable(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close() // immediately unreachable
	t.Setenv("JABALI_DOCS_URL", dead.URL)

	fp := &fakePanel{}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()
	cs := connect(t, newOpts(t, ts.URL, false))

	res := callSearchDocs(t, cs, map[string]any{"query": "anything"})
	if !res.IsError || !strings.Contains(firstText(res), "docs index is not available") {
		t.Errorf("expected clean index-unavailable error, got IsError=%v %q", res.IsError, firstText(res))
	}
}
