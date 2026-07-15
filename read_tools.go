package main

import (
	"context"
	"fmt"

	"github.com/mckay22/midpoint-mcp-server/internal/midpoint"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerReadTools installs the M1 read-only tools on the server.
func registerReadTools(server *mcp.Server, client *midpoint.Client) {
	registerSearchUsers(server, client)
	registerGetUser(server, client)
	registerGetUserAssignments(server, client)
	registerListRoles(server, client)
	registerGetRole(server, client)
	registerListResources(server, client)
	registerGetResource(server, client)
}

// --- search_users ---

type searchUsersInput struct {
	Query string `json:"query,omitempty" jsonschema:"free-text match against name, full name, or email (substring); omit to list users"`
	OID   string `json:"oid,omitempty" jsonschema:"exact midPoint OID; when set, returns just that user"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum results, default 20, max 100"`
}

type searchUsersOutput struct {
	Users []midpoint.UserSummary `json:"users"`
	Count int                    `json:"count"`
}

func registerSearchUsers(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_users",
		Title:       "Search users",
		Description: "Find midPoint users by free-text query (name, full name, or email) or by exact OID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchUsersInput) (*mcp.CallToolResult, searchUsersOutput, error) {
		users, err := client.SearchUsers(ctx, midpoint.SearchOptions{
			Query: in.Query,
			OID:   in.OID,
			Limit: in.Limit,
		})
		if err != nil {
			return nil, searchUsersOutput{}, err
		}
		return text(fmt.Sprintf("Found %d user(s).", len(users))),
			searchUsersOutput{Users: users, Count: len(users)}, nil
	})
}

// --- get_user ---

type oidInput struct {
	OID string `json:"oid" jsonschema:"the midPoint object OID"`
}

func registerGetUser(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_user",
		Title:       "Get user",
		Description: "Fetch a single midPoint user by OID (identity attributes and status).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in oidInput) (*mcp.CallToolResult, midpoint.UserDetail, error) {
		user, err := client.GetUser(ctx, in.OID)
		if err != nil {
			return nil, midpoint.UserDetail{}, err
		}
		return text(fmt.Sprintf("%s (%s)", user.Name, user.OID)), user, nil
	})
}

// --- get_user_assignments ---

func registerGetUserAssignments(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_user_assignments",
		Title:       "Get user assignments",
		Description: "List a user's direct assignments and effective role membership (each flagged direct or inherited) — what they have and why.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in oidInput) (*mcp.CallToolResult, midpoint.UserAssignments, error) {
		res, err := client.GetUserAssignments(ctx, in.OID)
		if err != nil {
			return nil, midpoint.UserAssignments{}, err
		}
		return text(fmt.Sprintf("%s has %d direct assignment(s), %d effective membership(s).",
			res.User.Name, len(res.Assignments), len(res.Effective))), res, nil
	})
}

// --- list_roles ---

type limitInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"maximum results, default 20, max 100"`
}

type listRolesOutput struct {
	Roles []midpoint.RoleSummary `json:"roles"`
	Count int                    `json:"count"`
}

func registerListRoles(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_roles",
		Title:       "List roles",
		Description: "List midPoint roles (name, display name, description).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in limitInput) (*mcp.CallToolResult, listRolesOutput, error) {
		roles, err := client.ListRoles(ctx, in.Limit)
		if err != nil {
			return nil, listRolesOutput{}, err
		}
		return text(fmt.Sprintf("Found %d role(s).", len(roles))),
			listRolesOutput{Roles: roles, Count: len(roles)}, nil
	})
}

// --- get_role ---

func registerGetRole(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_role",
		Title:       "Get role",
		Description: "Fetch a single midPoint role by OID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in oidInput) (*mcp.CallToolResult, midpoint.RoleDetail, error) {
		role, err := client.GetRole(ctx, in.OID)
		if err != nil {
			return nil, midpoint.RoleDetail{}, err
		}
		return text(fmt.Sprintf("%s (%s)", role.Name, role.OID)), role, nil
	})
}

// --- list_resources ---

type listResourcesOutput struct {
	Resources []midpoint.ResourceSummary `json:"resources"`
	Count     int                        `json:"count"`
}

func registerListResources(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_resources",
		Title:       "List resources",
		Description: "List midPoint resources (connected systems).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in limitInput) (*mcp.CallToolResult, listResourcesOutput, error) {
		resources, err := client.ListResources(ctx, in.Limit)
		if err != nil {
			return nil, listResourcesOutput{}, err
		}
		return text(fmt.Sprintf("Found %d resource(s).", len(resources))),
			listResourcesOutput{Resources: resources, Count: len(resources)}, nil
	})
}

// --- get_resource ---

func registerGetResource(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_resource",
		Title:       "Get resource",
		Description: "Fetch a single midPoint resource by OID, including connection status where reported.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in oidInput) (*mcp.CallToolResult, midpoint.ResourceDetail, error) {
		resource, err := client.GetResource(ctx, in.OID)
		if err != nil {
			return nil, midpoint.ResourceDetail{}, err
		}
		return text(fmt.Sprintf("%s (%s)", resource.Name, resource.OID)), resource, nil
	})
}

// text builds a CallToolResult carrying a single human-readable line; the typed
// output is attached as StructuredContent by the SDK.
func text(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}
