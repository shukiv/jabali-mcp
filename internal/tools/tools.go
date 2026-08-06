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
package tools

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shukiv/jabali-mcp/internal/client"
)

// Register adds every enabled tool to the server.
func Register(s *mcp.Server, opts client.Options) {
	reg := opts.Registry
	registerRead(s, reg)
	if opts.AllowWrite {
		registerWrite(s, reg)
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
	return exec(ctx, c, spec)
}

// runWrite executes a mutating request. When gated is true the input must carry
// confirm=true; otherwise the call returns a preview instead of acting.
func runWrite[In any](ctx context.Context, reg *client.Registry, in In, gated bool, preview string, spec reqSpec) (*mcp.CallToolResult, any, error) {
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
	return exec(ctx, c, spec)
}

func exec(ctx context.Context, c *client.Client, spec reqSpec) (*mcp.CallToolResult, any, error) {
	raw, status, err := c.Do(ctx, spec.method, spec.path, spec.body)
	if err != nil {
		return errResult(err.Error()), nil, nil
	}
	if status < 200 || status >= 300 {
		return errResult(fmt.Sprintf("panel returned HTTP %d: %s", status, string(raw))), nil, nil
	}
	return textResult(string(raw)), nil, nil
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
