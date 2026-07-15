package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mckay22/midpoint-mcp-server/internal/midpoint"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerAuditTools installs the M5 read-only reporting tools. They are not
// gated by MIDPOINT_MCP_ALLOW_WRITES.
func registerAuditTools(server *mcp.Server, client *midpoint.Client) {
	registerSearchObjects(server, client)
	registerSearchAudit(server, client)
}

// --- search_objects ---

type searchObjectsInput struct {
	Type   string `json:"type" jsonschema:"object type: users, roles, orgs, services, shadows, or resources"`
	Filter string `json:"filter,omitempty" jsonschema:"midPoint query-language filter, e.g. resourceRef matches (oid = \"...\") ; omit to match all"`
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum results, default 20, max 100"`
}

type searchObjectsOutput struct {
	Objects []midpoint.ObjectSummary `json:"objects"`
	Count   int                      `json:"count"`
}

func registerSearchObjects(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "search_objects",
		Title: "Search objects",
		Description: "Filtered search across midPoint object types (" + strings.Join(midpoint.SearchObjectTypes(), ", ") +
			") using a midPoint query-language filter — the building block for ad-hoc reports (orphaned accounts, unused roles, disabled users with access, ...).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchObjectsInput) (*mcp.CallToolResult, searchObjectsOutput, error) {
		objs, err := client.SearchObjects(ctx, in.Type, in.Filter, in.Limit)
		if err != nil {
			return nil, searchObjectsOutput{}, err
		}
		return text(fmt.Sprintf("Found %d %s object(s).", len(objs), in.Type)),
			searchObjectsOutput{Objects: objs, Count: len(objs)}, nil
	})
}

// --- search_audit ---

type searchAuditInput struct {
	From      string `json:"from,omitempty" jsonschema:"start of the time range (RFC3339); defaults to 30 days ago"`
	To        string `json:"to,omitempty" jsonschema:"end of the time range (RFC3339); omit for no upper bound"`
	EventType string `json:"eventType,omitempty" jsonschema:"filter by event type substring, e.g. modifyObject"`
	Outcome   string `json:"outcome,omitempty" jsonschema:"filter by outcome substring, e.g. success, fatalError"`
	Initiator string `json:"initiator,omitempty" jsonschema:"filter by initiator substring (who acted)"`
	Target    string `json:"target,omitempty" jsonschema:"filter by target substring (what was acted on)"`
	Channel   string `json:"channel,omitempty" jsonschema:"filter by channel substring"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum records, default 20, max 100"`
}

type searchAuditOutput struct {
	Records []midpoint.AuditRecord `json:"records"`
	Count   int                    `json:"count"`
	Status  string                 `json:"status,omitempty"`
	Note    string                 `json:"note,omitempty"`
}

func registerSearchAudit(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "search_audit",
		Title: "Search audit trail",
		Description: "Query the midPoint audit trail — who changed what and when, logins, approvals — " +
			"over a time range with optional initiator/target/event-type/outcome/channel filters. " +
			"midPoint 4.10 has no REST audit endpoint, so this runs a server-side script and needs " +
			"script-execution authorization; it therefore does not work under resource-server (OIDC) impersonation.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchAuditInput) (*mcp.CallToolResult, searchAuditOutput, error) {
		q := midpoint.AuditQuery{
			EventType: in.EventType,
			Outcome:   in.Outcome,
			Initiator: in.Initiator,
			Target:    in.Target,
			Channel:   in.Channel,
			Limit:     in.Limit,
		}

		if in.From == "" {
			q.From = time.Now().AddDate(0, 0, -30)
		} else {
			t, err := time.Parse(time.RFC3339, in.From)
			if err != nil {
				return nil, searchAuditOutput{}, fmt.Errorf("invalid 'from' time (want RFC3339): %w", err)
			}
			q.From = t
		}
		if in.To != "" {
			t, err := time.Parse(time.RFC3339, in.To)
			if err != nil {
				return nil, searchAuditOutput{}, fmt.Errorf("invalid 'to' time (want RFC3339): %w", err)
			}
			q.To = t
		}

		res, err := client.SearchAudit(ctx, q)
		if err != nil {
			return nil, searchAuditOutput{}, err
		}
		out := searchAuditOutput{Records: res.Records, Count: len(res.Records), Status: res.Status}
		if len(res.Records) == 0 {
			out.Note = "No audit records matched. Widen the time range or relax filters. " +
				"Note this tool needs script-execution authorization and does not run under OIDC impersonation."
		}
		return text(fmt.Sprintf("Found %d audit record(s).", len(res.Records))), out, nil
	})
}
