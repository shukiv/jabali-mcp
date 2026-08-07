package tools

// search_docs: read-only documentation lookup against the published Jabali
// Panel docs (an llms.txt index plus markdown pages), so an assistant can
// consult the docs mid-task and then act with the panel tools.
//
// Contract with the docs site (jabali-panel.com):
//   - GET <base>/llms.txt — llmstxt.org-style markdown index whose links point
//     at markdown doc pages on the SAME origin.
//   - Doc pages are plain markdown at stable URLs.
//
// Security: fetches are anonymous — the panel Bearer token is never sent —
// and restricted to the configured docs origin. llms.txt content is untrusted
// input; off-origin links in it are ignored so a tampered index cannot point
// this tool at internal hosts.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultDocsBase = "https://jabali-panel.com"

// docsMaxPageBytes caps a fetched doc page; docsMaxResult caps the tool output.
const (
	docsMaxPageBytes = 1 << 20 // 1 MiB
	docsMaxResult    = 24000   // characters
	docsCacheTTL     = 10 * time.Minute
	docsFetchPages   = 3 // pages fetched per search
)

var docsHTTP = &http.Client{Timeout: 10 * time.Second}

var docsCache = struct {
	sync.Mutex
	m map[string]docsCacheEntry
}{m: map[string]docsCacheEntry{}}

type docsCacheEntry struct {
	body string
	at   time.Time
}

// docsBase returns the docs origin, overridable via JABALI_DOCS_URL (useful
// for mirrors and tests). Trailing slash is stripped.
func docsBase() string {
	if v := os.Getenv("JABALI_DOCS_URL"); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return defaultDocsBase
}

func registerDocsTools(s *mcp.Server) {
	type SearchDocsIn struct {
		Query string `json:"query" jsonschema:"what to look up in the Jabali Panel documentation, e.g. 'configure MTA-STS' or 'backup destinations'"`
		Doc   string `json:"doc,omitempty" jsonschema:"optional: a doc path or URL from a previous search result to fetch in full instead of searching"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "search_docs",
		Description: "Search the official Jabali Panel documentation (jabali-panel.com) and return the " +
			"relevant sections. Use this to answer how-to and configuration questions before acting " +
			"with the panel tools. Pass doc to fetch one full page from a previous result.",
		Annotations: roAnno(),
	},
		func(ctx context.Context, _ *mcp.CallToolRequest, in SearchDocsIn) (*mcp.CallToolResult, any, error) {
			base, err := url.Parse(docsBase())
			if err != nil || base.Host == "" {
				return errResult("invalid docs base URL (JABALI_DOCS_URL): " + docsBase()), nil, nil
			}

			if in.Doc != "" {
				return fetchDocPage(ctx, base, in.Doc)
			}
			if strings.TrimSpace(in.Query) == "" {
				return errResult("query is required (or pass doc to fetch a specific page)"), nil, nil
			}

			index, err := docsFetch(ctx, base.String()+"/llms.txt")
			if err != nil {
				return errResult(fmt.Sprintf("the docs index is not available at %s/llms.txt (%v) — "+
					"the docs site may not publish it yet", base, err)), nil, nil
			}
			entries := parseDocsIndex(base, index)
			if len(entries) == 0 {
				return errResult("the docs index at " + base.String() + "/llms.txt contains no same-origin doc links"), nil, nil
			}

			terms := queryTerms(in.Query)
			ranked := rankDocs(entries, terms)
			if len(ranked) == 0 {
				// Nothing matched by title: return the index so the model can pick.
				var b strings.Builder
				b.WriteString("No doc titles matched. Available documentation pages (re-call with doc:<url> to fetch one):\n")
				for _, e := range entries {
					fmt.Fprintf(&b, "- %s — %s\n", e.title, e.url)
				}
				return textResult(clipDocs(b.String())), nil, nil
			}

			var b strings.Builder
			for i, e := range ranked {
				if i >= docsFetchPages {
					break
				}
				page, err := docsFetch(ctx, e.url)
				if err != nil {
					fmt.Fprintf(&b, "## %s (%s)\n\n(fetch failed: %v)\n\n", e.title, e.url, err)
					continue
				}
				fmt.Fprintf(&b, "## %s (%s)\n\n%s\n\n", e.title, e.url, excerptDocs(page, terms))
			}
			b.WriteString("(Re-call with doc:<url> for a full page.)")
			return textResult(clipDocs(b.String())), nil, nil
		})
}

func fetchDocPage(ctx context.Context, base *url.URL, ref string) (*mcp.CallToolResult, any, error) {
	u, err := base.Parse(ref)
	if err != nil {
		return errResult("invalid doc reference: " + ref), nil, nil
	}
	if !sameDocsOrigin(base, u) {
		return errResult(fmt.Sprintf("refusing to fetch %s: outside the docs origin %s", u, base)), nil, nil
	}
	page, err := docsFetch(ctx, u.String())
	if err != nil {
		return errResult(fmt.Sprintf("fetch %s: %v", u, err)), nil, nil
	}
	return textResult(clipDocs(page)), nil, nil
}

// docsFetch GETs a docs URL anonymously with caching and a size cap.
func docsFetch(ctx context.Context, rawURL string) (string, error) {
	docsCache.Lock()
	if e, ok := docsCache.m[rawURL]; ok && time.Since(e.at) < docsCacheTTL {
		docsCache.Unlock()
		return e.body, nil
	}
	docsCache.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "jabali-mcp")
	resp, err := docsHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, docsMaxPageBytes))
	if err != nil {
		return "", err
	}
	body := string(raw)
	docsCache.Lock()
	docsCache.m[rawURL] = docsCacheEntry{body: body, at: time.Now()}
	docsCache.Unlock()
	return body, nil
}

type docsEntry struct {
	title string
	url   string
}

var docsLinkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)

// parseDocsIndex extracts same-origin doc links from llms.txt. Off-origin
// links are dropped (the index is untrusted input).
func parseDocsIndex(base *url.URL, index string) []docsEntry {
	var out []docsEntry
	seen := map[string]bool{}
	for _, m := range docsLinkRe.FindAllStringSubmatch(index, -1) {
		u, err := base.Parse(m[2])
		if err != nil || !sameDocsOrigin(base, u) || seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		out = append(out, docsEntry{title: strings.TrimSpace(m[1]), url: u.String()})
	}
	return out
}

func sameDocsOrigin(base, u *url.URL) bool {
	return u.Scheme == base.Scheme && u.Host == base.Host
}

func queryTerms(q string) []string {
	var terms []string
	for _, t := range strings.Fields(strings.ToLower(q)) {
		if len(t) > 2 {
			terms = append(terms, t)
		}
	}
	if len(terms) == 0 {
		terms = strings.Fields(strings.ToLower(q))
	}
	return terms
}

// rankDocs orders index entries by how many query terms their title/URL hit.
func rankDocs(entries []docsEntry, terms []string) []docsEntry {
	type scored struct {
		e docsEntry
		n int
	}
	var hits []scored
	for _, e := range entries {
		hay := strings.ToLower(e.title + " " + e.url)
		n := 0
		for _, t := range terms {
			if strings.Contains(hay, t) {
				n++
			}
		}
		if n > 0 {
			hits = append(hits, scored{e, n})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].n > hits[j].n })
	out := make([]docsEntry, len(hits))
	for i, h := range hits {
		out[i] = h.e
	}
	return out
}

// excerptDocs returns the heading-delimited sections of a markdown page that
// mention any query term, falling back to the page head.
func excerptDocs(page string, terms []string) string {
	const perPage = 4000
	lines := strings.Split(page, "\n")
	var sections []string
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		body := strings.ToLower(strings.Join(cur, "\n"))
		for _, t := range terms {
			if strings.Contains(body, t) {
				sections = append(sections, strings.Join(cur, "\n"))
				return
			}
		}
	}
	for _, ln := range lines {
		if strings.HasPrefix(ln, "#") {
			flush()
			cur = cur[:0]
		}
		cur = append(cur, ln)
	}
	flush()
	out := strings.Join(sections, "\n\n")
	if out == "" {
		out = page
	}
	if len(out) > perPage {
		out = out[:perPage] + "\n… (truncated — fetch the full page with doc:<url>)"
	}
	return out
}

func clipDocs(s string) string {
	if len(s) > docsMaxResult {
		return s[:docsMaxResult] + "\n… (truncated)"
	}
	return s
}
