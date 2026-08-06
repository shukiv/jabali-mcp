// Command jabali-mcp is a Model Context Protocol server that exposes the
// Jabali Panel REST API as MCP tools.
//
// Auth is a per-user Bearer token (jat_…); the panel enforces ownership, so the
// server inherits the panel's tenant isolation. Read tools are always exposed;
// mutating tools require JABALI_MCP_ALLOW_WRITE, and destructive ones require an
// explicit confirm=true per call.
//
// Configuration (environment):
//
//	JABALI_PANEL_URL     https://panel.example:8443/api/v1   (single-panel)
//	JABALI_API_TOKEN     jat_…                               (single-panel)
//	JABALI_PANEL_NAME    logical name (optional; default "default")
//	JABALI_CA_FILE       PEM bundle to trust a self-hosted panel CA (optional)
//	JABALI_PANELS_FILE   JSON array of panels for fleet mode (overrides the above)
//	JABALI_MCP_ALLOW_WRITE=1  enable mutating tools (default: read-only)
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shukiv/jabali-mcp/internal/client"
	"github.com/shukiv/jabali-mcp/internal/tools"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "jabali-mcp: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	opts, err := client.LoadOptions()
	if err != nil {
		return err
	}

	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "jabali-mcp",
		Title:   "Jabali Panel",
		Version: version,
	}, nil)

	tools.Register(srv, opts)

	// Startup line goes to stderr so it never corrupts the stdio MCP stream.
	mode := "read-only"
	if opts.AllowWrite {
		mode = "read-write (mutations enabled; destructive ops still require confirm=true)"
	}
	if opts.DryRun {
		mode += " [DRY RUN: writes preview only]"
	}
	fmt.Fprintf(os.Stderr, "jabali-mcp %s — %s; panels: %v\n", version, mode, opts.Registry.Names())

	return srv.Run(context.Background(), &mcp.StdioTransport{})
}
