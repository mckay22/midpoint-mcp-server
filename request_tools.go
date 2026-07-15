package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/mckay22/midpoint-mcp-server/internal/midpoint"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerRequestTools installs the M3 requests & approvals tools. The mutating
// ones (request_role, approve/reject) respect the write gate.
func registerRequestTools(server *mcp.Server, client *midpoint.Client, allowWrites bool) {
	registerRequestRole(server, client, allowWrites)
	registerListMyRequests(server, client)
	registerListWorkItems(server, client)
	registerGetCase(server, client)
	registerCompleteWorkItem(server, client, allowWrites, true)
	registerCompleteWorkItem(server, client, allowWrites, false)
}

// --- request_role ---

type requestRoleInput struct {
	RoleOID string `json:"roleOid" jsonschema:"OID of the role to request"`
	UserOID string `json:"userOid,omitempty" jsonschema:"OID of the user the role is for; defaults to the authenticated user (self-service)"`
}

func registerRequestRole(server *mcp.Server, client *midpoint.Client, allowWrites bool) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "request_role",
		Title:       "Request role",
		Description: "Request a role (self-service). Submits an assignment-add delta; midPoint policy may route it through an approval case instead of executing. The requester is always the authenticated user. Respects the write gate.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in requestRoleInput) (*mcp.CallToolResult, writeOutput, error) {
		target := strings.TrimSpace(in.UserOID)
		if target == "" {
			self, err := client.Self(ctx)
			if err != nil {
				return nil, writeOutput{}, fmt.Errorf("resolving self: %w", err)
			}
			target = self.OID
		}

		plan, err := client.PlanRequestRole(target, in.RoleOID)
		if err != nil {
			return nil, writeOutput{}, err
		}
		if !allowWrites {
			res, out := previewWrite(plan)
			return res, out, nil
		}

		applied, err := client.Apply(ctx, plan)
		if err != nil {
			return nil, writeOutput{}, err
		}
		out := writeOutput{
			Applied:  true,
			Summary:  plan.Summary,
			Method:   plan.Method,
			Endpoint: plan.Endpoint(),
			Body:     plan.Body,
		}
		// Best-effort: surface the approval case, if policy created one. This is
		// robust to whether midPoint signals approval via status code.
		if caseOID := client.FindRequestCase(ctx, target, in.RoleOID); caseOID != "" {
			out.Result = "pending approval; caseOid=" + caseOID
			return text(fmt.Sprintf("Requested role %s for %s — pending approval (case %s).", in.RoleOID, target, caseOID)), out, nil
		}
		out.Result = fmt.Sprintf("status=%d (no approval case found; likely executed directly)", applied.StatusCode)
		return text(fmt.Sprintf("Requested role %s for %s — %s.", in.RoleOID, target, out.Result)), out, nil
	})
}

// --- list_my_requests ---

type listMyRequestsOutput struct {
	Requests []midpoint.CaseSummary `json:"requests"`
	Count    int                    `json:"count"`
}

func registerListMyRequests(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_my_requests",
		Title:       "List my requests",
		Description: "List approval cases the authenticated user initiated.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in limitInput) (*mcp.CallToolResult, listMyRequestsOutput, error) {
		cases, err := client.ListMyRequests(ctx, in.Limit)
		if err != nil {
			return nil, listMyRequestsOutput{}, err
		}
		return text(fmt.Sprintf("Found %d request(s).", len(cases))),
			listMyRequestsOutput{Requests: cases, Count: len(cases)}, nil
	})
}

// --- list_work_items ---

type listWorkItemsOutput struct {
	WorkItems []midpoint.WorkItem `json:"workItems"`
	Count     int                 `json:"count"`
}

func registerListWorkItems(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_work_items",
		Title:       "List work items",
		Description: "List the authenticated user's approval inbox: open work items assigned to them.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in limitInput) (*mcp.CallToolResult, listWorkItemsOutput, error) {
		items, err := client.ListWorkItems(ctx, in.Limit)
		if err != nil {
			return nil, listWorkItemsOutput{}, err
		}
		return text(fmt.Sprintf("Found %d work item(s) in your inbox.", len(items))),
			listWorkItemsOutput{WorkItems: items, Count: len(items)}, nil
	})
}

// --- get_case ---

func registerGetCase(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_case",
		Title:       "Get case",
		Description: "Fetch an approval case by OID, including its work items.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in oidInput) (*mcp.CallToolResult, midpoint.CaseDetail, error) {
		c, err := client.GetCase(ctx, in.OID)
		if err != nil {
			return nil, midpoint.CaseDetail{}, err
		}
		return text(fmt.Sprintf("Case %s: state=%s, %d work item(s).", c.OID, c.State, len(c.WorkItems))), c, nil
	})
}

// --- approve_work_item / reject_work_item ---

type workItemInput struct {
	CaseOID    string `json:"caseOid" jsonschema:"OID of the case"`
	WorkItemID string `json:"workItemId" jsonschema:"id of the work item within the case"`
	Comment    string `json:"comment,omitempty" jsonschema:"optional decision comment"`
}

func registerCompleteWorkItem(server *mcp.Server, client *midpoint.Client, allowWrites, approve bool) {
	name, title, desc := "reject_work_item", "Reject work item", "Reject an approval work item. Respects the write gate."
	if approve {
		name, title, desc = "approve_work_item", "Approve work item", "Approve an approval work item. Respects the write gate."
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        name,
		Title:       title,
		Description: desc,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in workItemInput) (*mcp.CallToolResult, writeOutput, error) {
		plan, err := client.PlanCompleteWorkItem(in.CaseOID, in.WorkItemID, approve, in.Comment)
		if err != nil {
			return nil, writeOutput{}, err
		}
		return runWrite(ctx, allowWrites, client, plan)
	})
}
