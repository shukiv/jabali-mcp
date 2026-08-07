// Package tools holds the MCP tool definitions for the Jabali Panel MCP server.
//
// One tool per REST operation. Tools are split into two groups:
//
//   - Read tools (ReadOnlyHint) — always registered.
//   - Write tools — registered only when opts.AllowWrite is set. Destructive
//     ones (delete, restore, password) additionally require an explicit
//     confirm=true argument, so a model cannot destroy state on a single call.
//
// Every tool inherits the panel's own tenant isolation: the Bearer token acts
// as its owning user and the panel enforces ownership server-side, so a tenant
// token can only reach that tenant's resources regardless of what a tool asks
// for.
//go:generate go run ../../cmd/gen-tools -spec ../../openapi/openapi.yaml -curation ../../openapi/tools.yaml -out generated.go
//go:generate go run ../../cmd/gen-tools -spec ../../openapi/openapi.yaml -curation ../../openapi/admin-tools.yaml -out generated_admin.go -group Admin

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shukiv/jabali-mcp/internal/client"
)

// globalDryRun forces every write tool to preview instead of act. Set once by
// Register at startup, read-only thereafter.
var globalDryRun bool

// Register adds every enabled tool to the server. The admin group is added only
// when opts.Admin is set (and, per tool, gated by the panel's RequireAdmin).
func Register(s *mcp.Server, opts client.Options) {
	globalDryRun = opts.DryRun
	reg := opts.Registry
	registerRead(s, reg)
	registerComposite(s, reg)
	registerIssueTools(s, reg, opts.AllowWrite)
	registerDocsTools(s)
	if opts.AllowWrite {
		registerWrite(s, reg)
	}
	if opts.Admin {
		registerAdminRead(s, reg)
		if opts.AllowWrite {
			registerAdminWrite(s, reg)
		}
	}
}

// panelArg is embedded in every tool input so a call can target a named panel
// in fleet mode. Omit it to use the default panel.
type panelArg struct {
	Panel string `json:"panel,omitempty" jsonschema:"target panel name for fleet mode; omit to use the default panel"`
}

func (p panelArg) panelName() string { return p.Panel }

type panelSelector interface{ panelName() string }

// confirmArg is embedded in destructive tool inputs.
type confirmArg struct {
	Confirm bool `json:"confirm,omitempty" jsonschema:"must be set to true to actually perform this destructive, irreversible action"`
}

func (c confirmArg) confirmed() bool { return c.Confirm }

type confirmer interface{ confirmed() bool }

// dryRunArg is embedded in every write tool input.
type dryRunArg struct {
	DryRun bool `json:"dry_run,omitempty" jsonschema:"if true, return the request that would be sent without performing it"`
}

func (d dryRunArg) isDryRun() bool { return d.DryRun }

type dryRunner interface{ isDryRun() bool }

// reqSpec is one upstream REST call.
type reqSpec struct {
	method string
	path   string
	body   any
}

func resolveClient(reg *client.Registry, in any) (*client.Client, error) {
	name := ""
	if ps, ok := in.(panelSelector); ok {
		name = ps.panelName()
	}
	return reg.Get(name)
}

// runRead executes a read request and returns the raw body as text.
func runRead[In any](ctx context.Context, reg *client.Registry, in In, spec reqSpec) (*mcp.CallToolResult, any, error) {
	c, err := resolveClient(reg, in)
	if err != nil {
		return errResult(err.Error()), nil, nil
	}
	return execReq(ctx, c, spec)
}

// runWrite executes a mutating request. Order:
//  1. Dry-run (global flag or per-call dry_run) previews the request and stops.
//  2. A gated (destructive) tool requires confirm=true, else it previews.
//  3. Otherwise the request is sent.
func runWrite[In any](ctx context.Context, reg *client.Registry, in In, gated bool, preview string, spec reqSpec) (*mcp.CallToolResult, any, error) {
	dry := globalDryRun
	if dr, ok := any(in).(dryRunner); ok && dr.isDryRun() {
		dry = true
	}
	if dry {
		return textResult("DRY RUN — no request was sent. Would send:\n" + describeReq(spec)), nil, nil
	}
	if gated {
		cf, ok := any(in).(confirmer)
		if !ok || !cf.confirmed() {
			return textResult("CONFIRMATION REQUIRED — this is destructive and was NOT performed.\n" +
				preview + "\nRe-call this tool with \"confirm\": true to proceed."), nil, nil
		}
	}
	c, err := resolveClient(reg, in)
	if err != nil {
		return errResult(err.Error()), nil, nil
	}
	return execReq(ctx, c, spec)
}

// describeReq renders a request for a dry-run preview.
func describeReq(spec reqSpec) string {
	s := spec.method + " " + spec.path
	if spec.body != nil {
		if b, err := json.Marshal(spec.body); err == nil {
			s += "\nbody: " + string(b)
		}
	}
	return s
}

func execReq(ctx context.Context, c *client.Client, spec reqSpec) (*mcp.CallToolResult, any, error) {
	raw, status, err := c.Do(ctx, spec.method, spec.path, spec.body)
	if err != nil {
		return errResult(err.Error()), nil, nil
	}
	if status < 200 || status >= 300 {
		return errResult(fmt.Sprintf("panel returned HTTP %d: %s", status, string(raw))), nil, nil
	}
	return textResult(string(raw)), nil, nil
}

// Input validators used by the generated tools. Each returns nil when the value
// is valid, or an error result to return to the caller. They enforce the value
// constraints the OpenAPI spec declares (the SDK already enforces types and
// required-presence), failing fast with a precise message before any request.

func vEnum(field, val string, allowed []string) *mcp.CallToolResult {
	for _, a := range allowed {
		if val == a {
			return nil
		}
	}
	return errResult(fmt.Sprintf("invalid %s %q: must be one of %s", field, val, strings.Join(allowed, ", ")))
}

func vMinLen(field, val string, n int) *mcp.CallToolResult {
	if len([]rune(val)) < n {
		return errResult(fmt.Sprintf("%s must be at least %d characters", field, n))
	}
	return nil
}

func vMin(field string, val, min int) *mcp.CallToolResult {
	if val < min {
		return errResult(fmt.Sprintf("%s must be >= %d (got %d)", field, min, val))
	}
	return nil
}

func vMax(field string, val, max int) *mcp.CallToolResult {
	if val > max {
		return errResult(fmt.Sprintf("%s must be <= %d (got %d)", field, max, val))
	}
	return nil
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func errResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// listQuery builds a ?page=&page_size=&q= suffix, omitting empty parts.
func listQuery(page, pageSize int, q string) string {
	v := url.Values{}
	if page > 0 {
		v.Set("page", strconv.Itoa(page))
	}
	if pageSize > 0 {
		v.Set("page_size", strconv.Itoa(pageSize))
	}
	if q != "" {
		v.Set("q", q)
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

// Schema helpers for the generated tools: infer the SDK's schema for the input
// struct, then let the generator graft on the value constraints (enum/min/max/
// minLength) the OpenAPI spec declares, so MCP clients see them in tools/list
// and the SDK enforces them before the handler runs. The runtime v* validators
// above stay as defense-in-depth.

func inferSchema[T any]() *jsonschema.Schema {
	s, err := jsonschema.For[T](&jsonschema.ForOptions{})
	if err != nil {
		// Registration-time and deterministic: a failure here is a generator bug.
		panic(fmt.Sprintf("infer schema: %v", err))
	}
	return s
}

func f64(v int) *float64 { f := float64(v); return &f }

func iptr(v int) *int { return &v }

func enumVals(vs ...string) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = v
	}
	return out
}

// readonly / destructive annotation helpers.
func roAnno() *mcp.ToolAnnotations { return &mcp.ToolAnnotations{ReadOnlyHint: true} }

func destructiveAnno() *mcp.ToolAnnotations {
	t := true
	return &mcp.ToolAnnotations{DestructiveHint: &t}
}

func additiveAnno() *mcp.ToolAnnotations {
	f := false
	return &mcp.ToolAnnotations{DestructiveHint: &f}
}
