package main

import (
	"context"
	"fmt"

	"github.com/mckay22/midpoint-mcp-server/internal/midpoint"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerTeamTools installs the M6 manager/team read tools. They are read-only
// (outside the write gate) and, in resource-server mode, run as the caller so
// midPoint scopes results to what that manager may see.
func registerTeamTools(server *mcp.Server, client *midpoint.Client) {
	registerListMyTeam(server, client)
	registerListMyManagers(server, client)
}

type teamOutput struct {
	Users []midpoint.UserSummary `json:"users"`
	Count int                    `json:"count"`
}

func registerListMyTeam(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "list_my_team",
		Title: "List my team",
		Description: "List the authenticated user's direct reports: the members of the orgs they manage " +
			"(empty if they manage none). Use the returned OIDs with get_user_assignments to review a report's " +
			"access, or request_role to request access for them.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in limitInput) (*mcp.CallToolResult, teamOutput, error) {
		users, err := client.ListMyTeam(ctx, in.Limit)
		if err != nil {
			return nil, teamOutput{}, err
		}
		return text(fmt.Sprintf("You manage %d direct report(s).", len(users))),
			teamOutput{Users: users, Count: len(users)}, nil
	})
}

func registerListMyManagers(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_my_managers",
		Title:       "List my managers",
		Description: "List who the authenticated user reports to: the managers of the orgs they are a member of.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in limitInput) (*mcp.CallToolResult, teamOutput, error) {
		users, err := client.ListMyManagers(ctx, in.Limit)
		if err != nil {
			return nil, teamOutput{}, err
		}
		return text(fmt.Sprintf("You report to %d manager(s).", len(users))),
			teamOutput{Users: users, Count: len(users)}, nil
	})
}
