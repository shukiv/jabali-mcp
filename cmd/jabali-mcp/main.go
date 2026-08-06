// Command jabali-mcp is a Model Context Protocol server that exposes the
// Jabali Panel automation API as MCP tools.
//
// Design (see docs/DESIGN.md):
//   - Each MCP tool maps 1:1 to a Jabali automation-API operation.
//   - Tools authenticate with a scoped automation_token (HMAC-SHA256, ADR-0093);
//     the server NEVER holds the write:everything scope by default.
//   - Read-only tools are the default surface. Mutating and destructive tools
//     (delete user, restore, DNS/SSL writes) are opt-in and confirmation-gated.
//   - Tool definitions are generated from the panel's docs/api/openapi.yaml so
//     the surface stays in lockstep with the API (the panel's OpenAPI coverage
//     golden test keeps that spec honest).
//
// This is the entry-point skeleton. Wiring the MCP SDK, the HMAC client, and
// the openapi->tools generator is the first implementation milestone — see
// docs/DESIGN.md and the README.
package main

import (
	"fmt"
	"os"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "0.0.0-dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "jabali-mcp: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// TODO(milestone-1): construct the MCP server over stdio, register the
	// generated read-only tools, and serve. Kept as a stub so the module
	// compiles with zero external dependencies until the SDK is wired (the
	// SDK API is verified against current docs before it is imported).
	fmt.Printf("jabali-mcp %s — skeleton; see docs/DESIGN.md\n", version)
	return nil
}
