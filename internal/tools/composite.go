package tools

// Hand-written composite tools. Unlike generated.go these aggregate several
// REST calls into one triage answer; they are read-only and register
// unconditionally alongside the generated read tools.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shukiv/jabali-mcp/internal/client"
)

// maxProbeErrBody caps how much of an upstream error body a probe error keeps.
const maxProbeErrBody = 500

func registerComposite(s *mcp.Server, reg *client.Registry) {
	type DiagnoseDomainIn struct {
		panelArg
		DomainId   string `json:"domain_id" jsonschema:"the domain's ULID"`
		ErrorLines int    `json:"error_lines,omitempty" jsonschema:"trailing nginx error-log lines to include (default 50, max 500)"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "diagnose_domain",
		Description: "One-call domain triage: config, SSL certificate status, DNS records, " +
			"recent nginx error-log lines, and bandwidth, in a single JSON answer. " +
			"A failed probe is reported under probe_errors without failing the rest.",
		Annotations: roAnno(),
	},
		func(ctx context.Context, _ *mcp.CallToolRequest, in DiagnoseDomainIn) (*mcp.CallToolResult, any, error) {
			c, err := resolveClient(reg, in)
			if err != nil {
				return errResult(err.Error()), nil, nil
			}
			lines := in.ErrorLines
			if lines <= 0 {
				lines = 50
			}
			if lines > 500 {
				lines = 500
			}

			out := map[string]any{}
			probeErrs := map[string]string{}
			probe := func(key, path string) (status int) {
				raw, status, err := c.Do(ctx, "GET", path, nil)
				switch {
				case err != nil:
					probeErrs[key] = err.Error()
				case status < 200 || status >= 300:
					body := string(raw)
					if len(body) > maxProbeErrBody {
						body = body[:maxProbeErrBody] + "…"
					}
					probeErrs[key] = fmt.Sprintf("HTTP %d: %s", status, body)
				default:
					out[key] = json.RawMessage(raw)
				}
				return status
			}

			id := url.PathEscape(in.DomainId)
			probe("domain", "/domains/"+id)
			if st := probe("ssl", "/domains/"+id+"/ssl"); st == 404 {
				// No certificate is a diagnosis, not a probe failure.
				delete(probeErrs, "ssl")
				out["ssl"] = json.RawMessage(`{"status":"none","note":"no certificate provisioned"}`)
			}
			probe("dns_records", "/domains/"+id+"/dns/records")
			probe("bandwidth", "/domains/"+id+"/bandwidth")
			probe("recent_errors", "/logs/tail?domain_id="+url.QueryEscape(in.DomainId)+
				"&log_type=error&lines="+strconv.Itoa(lines))

			if len(probeErrs) > 0 {
				out["probe_errors"] = probeErrs
			}
			b, err := json.Marshal(out)
			if err != nil {
				return errResult("assemble diagnosis: " + err.Error()), nil, nil
			}
			return textResult(string(b)), nil, nil
		})
}
