// Package gen generates MCP tool definitions from the Jabali Panel OpenAPI spec
// (docs/api/openapi.yaml in the jabali-panel repo).
//
// Each OpenAPI operation with an automation scope becomes a tool: the operation
// summary is the tool description, the request schema becomes the tool input
// schema, and the operation's scope is recorded so the tools package can gate
// it. Regenerating after an API change keeps the MCP surface in lockstep with
// the panel — the same property the panel's OpenAPI coverage golden test gives
// the spec itself.
package gen
