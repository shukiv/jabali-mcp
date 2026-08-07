package tools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

import "github.com/modelcontextprotocol/go-sdk/mcp"

// diagPanel serves distinct bodies per probe path so the test can assert the
// aggregation, and fails one probe to prove partial-failure tolerance.
func diagPanel(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /domains/01D", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"01D","name":"example.com","is_enabled":true}`))
	})
	mux.HandleFunc("GET /domains/01D/ssl", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	})
	mux.HandleFunc("GET /domains/01D/dns/records", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"type":"A","name":"@"}]}`))
	})
	mux.HandleFunc("GET /domains/01D/bandwidth", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	})
	mux.HandleFunc("GET /logs/tail", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("log_type") != "error" {
			t.Errorf("expected log_type=error, got %q", r.URL.Query().Get("log_type"))
		}
		if r.URL.Query().Get("lines") != "50" {
			t.Errorf("expected default lines=50, got %q", r.URL.Query().Get("lines"))
		}
		_, _ = w.Write([]byte(`{"lines":["err1"]}`))
	})
	return httptest.NewServer(mux)
}

func TestDiagnoseDomainAggregatesAndToleratesFailures(t *testing.T) {
	ts := diagPanel(t)
	defer ts.Close()

	cs := connect(t, newOpts(t, ts.URL, false)) // read-only: composite must register
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "diagnose_domain",
		Arguments: map[string]any{"domain_id": "01D"},
	})
	if err != nil {
		t.Fatalf("call diagnose_domain: %v", err)
	}
	if res.IsError {
		t.Fatalf("diagnosis must not fail on a failed probe: %q", firstText(res))
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(firstText(res)), &got); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	for _, key := range []string{"domain", "ssl", "dns_records", "recent_errors", "probe_errors"} {
		if _, ok := got[key]; !ok {
			t.Errorf("diagnosis missing %q; got keys %v", key, keys(got))
		}
	}
	// SSL 404 is a diagnosis ("no certificate"), not a probe error.
	if !strings.Contains(string(got["ssl"]), `"status":"none"`) {
		t.Errorf("ssl 404 should map to status none, got %s", got["ssl"])
	}
	var pe map[string]string
	_ = json.Unmarshal(got["probe_errors"], &pe)
	if !strings.Contains(pe["bandwidth"], "HTTP 500") {
		t.Errorf("failed bandwidth probe should be reported, got %v", pe)
	}
	if _, bad := pe["ssl"]; bad {
		t.Errorf("ssl must not appear in probe_errors: %v", pe)
	}
}

func keys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
