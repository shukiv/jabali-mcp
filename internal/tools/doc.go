// Package tools holds the MCP tool definitions for the Jabali Panel MCP server.
//
// One tool per automation-API operation. Each tool declares the exact
// automation scope it requires (read:dns, write:domains, read:backups, …) so a
// tool cannot be invoked with a credential that lacks that scope — the scope
// check is enforced server-side by the panel's RequireScope middleware, and
// mirrored here so an unauthorized call is rejected before it leaves the host.
//
// Tools are grouped read vs. write. The default server exposes only the read
// group; the write group is opt-in and every destructive tool is
// confirmation-gated (see docs/DESIGN.md, "Security").
package tools
