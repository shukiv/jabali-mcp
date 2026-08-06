package tools

import (
	"context"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shukiv/jabali-mcp/internal/client"
)

// registerRead adds the read-only tool surface. Always registered.
func registerRead(s *mcp.Server, reg *client.Registry) {
	// --- Domains ---
	type listDomainsArgs struct {
		panelArg
		Page     int    `json:"page,omitempty" jsonschema:"page number (1-based)"`
		PageSize int    `json:"page_size,omitempty" jsonschema:"results per page"`
		Q        string `json:"q,omitempty" jsonschema:"search filter"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_domains",
		Description: "List the domains owned by the authenticated user (paginated).",
		Annotations: roAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listDomainsArgs) (*mcp.CallToolResult, any, error) {
		return runRead(ctx, reg, in, reqSpec{http.MethodGet, "/domains" + listQuery(in.Page, in.PageSize, in.Q), nil})
	})

	type domainIDArgs struct {
		panelArg
		DomainID string `json:"domain_id" jsonschema:"the domain's ULID"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_domain",
		Description: "Get a single domain by id.",
		Annotations: roAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in domainIDArgs) (*mcp.CallToolResult, any, error) {
		return runRead(ctx, reg, in, reqSpec{http.MethodGet, "/domains/" + url.PathEscape(in.DomainID), nil})
	})

	// --- DNS ---
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_dns_records",
		Description: "List DNS records for a domain.",
		Annotations: roAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in domainIDArgs) (*mcp.CallToolResult, any, error) {
		return runRead(ctx, reg, in, reqSpec{http.MethodGet, "/domains/" + url.PathEscape(in.DomainID) + "/dns/records", nil})
	})

	// --- Mail ---
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_mailboxes",
		Description: "List mailboxes for a domain.",
		Annotations: roAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in domainIDArgs) (*mcp.CallToolResult, any, error) {
		return runRead(ctx, reg, in, reqSpec{http.MethodGet, "/domains/" + url.PathEscape(in.DomainID) + "/mailboxes", nil})
	})
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_forwarders",
		Description: "List mail forwarders for a domain.",
		Annotations: roAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in domainIDArgs) (*mcp.CallToolResult, any, error) {
		return runRead(ctx, reg, in, reqSpec{http.MethodGet, "/domains/" + url.PathEscape(in.DomainID) + "/forwarders", nil})
	})

	// --- Applications / Databases / Backups / Tokens ---
	type pageArgs struct {
		panelArg
		Page     int    `json:"page,omitempty" jsonschema:"page number (1-based)"`
		PageSize int    `json:"page_size,omitempty" jsonschema:"results per page"`
		Q        string `json:"q,omitempty" jsonschema:"search filter"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_applications",
		Description: "List installed applications for the authenticated user.",
		Annotations: roAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pageArgs) (*mcp.CallToolResult, any, error) {
		return runRead(ctx, reg, in, reqSpec{http.MethodGet, "/applications" + listQuery(in.Page, in.PageSize, in.Q), nil})
	})
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_databases",
		Description: "List databases for the authenticated user.",
		Annotations: roAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pageArgs) (*mcp.CallToolResult, any, error) {
		return runRead(ctx, reg, in, reqSpec{http.MethodGet, "/databases" + listQuery(in.Page, in.PageSize, in.Q), nil})
	})
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_backups",
		Description: "List the authenticated user's backups and their status.",
		Annotations: roAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pageArgs) (*mcp.CallToolResult, any, error) {
		return runRead(ctx, reg, in, reqSpec{http.MethodGet, "/me/backups" + listQuery(in.Page, in.PageSize, in.Q), nil})
	})
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_api_tokens",
		Description: "List the authenticated user's API tokens (metadata only; the panel never returns the secret).",
		Annotations: roAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pageArgs) (*mcp.CallToolResult, any, error) {
		return runRead(ctx, reg, in, reqSpec{http.MethodGet, "/me/api-tokens" + listQuery(in.Page, in.PageSize, in.Q), nil})
	})
}
