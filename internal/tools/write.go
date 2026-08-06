package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shukiv/jabali-mcp/internal/client"
)

// registerWrite adds the mutating tool surface. Registered only when
// JABALI_MCP_ALLOW_WRITE is set. Destructive tools additionally require
// confirm=true (enforced by runWrite's gate) so a model cannot delete or
// overwrite state on a single call.
func registerWrite(s *mcp.Server, reg *client.Registry) {
	// --- Domains ---
	type createDomainArgs struct {
		panelArg
		Name string `json:"name" jsonschema:"the domain name, e.g. example.com"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_domain",
		Description: "Add a domain to the authenticated user's account.",
		Annotations: additiveAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createDomainArgs) (*mcp.CallToolResult, any, error) {
		return runWrite(ctx, reg, in, false, "", reqSpec{http.MethodPost, "/domains", map[string]any{"name": in.Name}})
	})

	type deleteDomainArgs struct {
		panelArg
		confirmArg
		DomainID string `json:"domain_id" jsonschema:"the domain's ULID"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_domain",
		Description: "Delete a domain and all its resources. DESTRUCTIVE and irreversible — requires confirm=true.",
		Annotations: destructiveAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteDomainArgs) (*mcp.CallToolResult, any, error) {
		preview := fmt.Sprintf("Delete domain %s (and its DNS, mail, files, databases).", in.DomainID)
		return runWrite(ctx, reg, in, true, preview, reqSpec{http.MethodDelete, "/domains/" + url.PathEscape(in.DomainID), nil})
	})

	// --- DNS ---
	type createRecordArgs struct {
		panelArg
		DomainID string `json:"domain_id" jsonschema:"the domain's ULID"`
		Name     string `json:"name" jsonschema:"record name relative to the zone apex; '@' = apex"`
		Type     string `json:"type" jsonschema:"record type: A, AAAA, CNAME, MX, TXT, SRV, CAA, NS"`
		Content  string `json:"content" jsonschema:"record value, e.g. 203.0.113.5"`
		TTL      int    `json:"ttl,omitempty" jsonschema:"TTL seconds (60-604800); omit for the server default"`
		Priority int    `json:"priority,omitempty" jsonschema:"priority (MX/SRV)"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_dns_record",
		Description: "Create a DNS record on a domain's zone.",
		Annotations: additiveAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createRecordArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{"name": in.Name, "type": in.Type, "content": in.Content}
		if in.TTL > 0 {
			body["ttl"] = in.TTL
		}
		if in.Priority > 0 {
			body["priority"] = in.Priority
		}
		return runWrite(ctx, reg, in, false, "", reqSpec{http.MethodPost, "/domains/" + url.PathEscape(in.DomainID) + "/dns/records", body})
	})

	type updateRecordArgs struct {
		panelArg
		RecordID string `json:"record_id" jsonschema:"the DNS record's ULID"`
		Content  string `json:"content,omitempty" jsonschema:"new record value"`
		TTL      int    `json:"ttl,omitempty" jsonschema:"new TTL seconds (60-604800)"`
		Priority int    `json:"priority,omitempty" jsonschema:"new priority (MX/SRV)"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_dns_record",
		Description: "Update fields of an existing DNS record.",
		Annotations: additiveAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateRecordArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{}
		if in.Content != "" {
			body["content"] = in.Content
		}
		if in.TTL > 0 {
			body["ttl"] = in.TTL
		}
		if in.Priority > 0 {
			body["priority"] = in.Priority
		}
		return runWrite(ctx, reg, in, false, "", reqSpec{http.MethodPatch, "/dns/records/" + url.PathEscape(in.RecordID), body})
	})

	type deleteRecordArgs struct {
		panelArg
		confirmArg
		RecordID string `json:"record_id" jsonschema:"the DNS record's ULID"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_dns_record",
		Description: "Delete a DNS record. DESTRUCTIVE — requires confirm=true.",
		Annotations: destructiveAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteRecordArgs) (*mcp.CallToolResult, any, error) {
		preview := fmt.Sprintf("Delete DNS record %s.", in.RecordID)
		return runWrite(ctx, reg, in, true, preview, reqSpec{http.MethodDelete, "/dns/records/" + url.PathEscape(in.RecordID), nil})
	})

	// --- Mail ---
	type createMailboxArgs struct {
		panelArg
		DomainID  string `json:"domain_id" jsonschema:"the domain's ULID"`
		LocalPart string `json:"local_part" jsonschema:"the part before @, e.g. alice"`
		Password  string `json:"password" jsonschema:"mailbox password (min 12 chars)"`
		QuotaMB   int    `json:"quota_mb" jsonschema:"mailbox quota in MB"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_mailbox",
		Description: "Create a mailbox on a domain.",
		Annotations: additiveAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createMailboxArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{"local_part": in.LocalPart, "password": in.Password, "quota_mb": in.QuotaMB}
		return runWrite(ctx, reg, in, false, "", reqSpec{http.MethodPost, "/domains/" + url.PathEscape(in.DomainID) + "/mailboxes", body})
	})

	type mailboxIDArgs struct {
		panelArg
		confirmArg
		MailboxID string `json:"mailbox_id" jsonschema:"the mailbox's ULID"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_mailbox",
		Description: "Delete a mailbox and its stored mail. DESTRUCTIVE — requires confirm=true.",
		Annotations: destructiveAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mailboxIDArgs) (*mcp.CallToolResult, any, error) {
		preview := fmt.Sprintf("Delete mailbox %s and all its stored mail.", in.MailboxID)
		return runWrite(ctx, reg, in, true, preview, reqSpec{http.MethodDelete, "/mailboxes/" + url.PathEscape(in.MailboxID), nil})
	})

	type setMailboxPasswordArgs struct {
		panelArg
		confirmArg
		MailboxID string `json:"mailbox_id" jsonschema:"the mailbox's ULID"`
		Password  string `json:"password" jsonschema:"the new password (min 12 chars)"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_mailbox_password",
		Description: "Change a mailbox password. Credential change — requires confirm=true.",
		Annotations: destructiveAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in setMailboxPasswordArgs) (*mcp.CallToolResult, any, error) {
		preview := fmt.Sprintf("Reset the password for mailbox %s (existing sessions may be invalidated).", in.MailboxID)
		body := map[string]any{"password": in.Password}
		return runWrite(ctx, reg, in, true, preview, reqSpec{http.MethodPut, "/mailboxes/" + url.PathEscape(in.MailboxID) + "/password", body})
	})

	type createForwarderArgs struct {
		panelArg
		DomainID string `json:"domain_id" jsonschema:"the domain's ULID"`
		Source   string `json:"source" jsonschema:"source address, e.g. sales@example.com"`
		Dest     string `json:"dest" jsonschema:"destination address, e.g. alice@example.com"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_forwarder",
		Description: "Create a mail forwarder on a domain.",
		Annotations: additiveAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createForwarderArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{"source": in.Source, "dest": in.Dest}
		return runWrite(ctx, reg, in, false, "", reqSpec{http.MethodPost, "/domains/" + url.PathEscape(in.DomainID) + "/forwarders", body})
	})

	// --- Backups ---
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_backup",
		Description: "Trigger an on-demand backup for the authenticated user.",
		Annotations: additiveAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in panelArg) (*mcp.CallToolResult, any, error) {
		return runWrite(ctx, reg, in, false, "", reqSpec{http.MethodPost, "/me/backups", nil})
	})

	type restoreArgs struct {
		panelArg
		confirmArg
		BackupID string `json:"backup_id" jsonschema:"the snapshot's ULID to restore"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "restore_backup",
		Description: "Restore a snapshot. DESTRUCTIVE — overwrites live files and databases. Requires confirm=true.",
		Annotations: destructiveAnno(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in restoreArgs) (*mcp.CallToolResult, any, error) {
		preview := fmt.Sprintf("Restore snapshot %s — this OVERWRITES current live files and databases with the snapshot's contents.", in.BackupID)
		return runWrite(ctx, reg, in, true, preview, reqSpec{http.MethodPost, "/me/backups/" + url.PathEscape(in.BackupID) + "/restore", nil})
	})
}
