package main

import (
	"context"
	"fmt"

	"github.com/mckay22/midpoint-mcp-server/internal/midpoint"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerWriteTools installs the M2 write tools. When allowWrites is false,
// every tool returns a dry-run preview instead of calling midPoint.
func registerWriteTools(server *mcp.Server, client *midpoint.Client, allowWrites bool) {
	registerCreateUser(server, client, allowWrites)
	registerSetUserEnabled(server, client, allowWrites, true)
	registerSetUserEnabled(server, client, allowWrites, false)
	registerAssignRole(server, client, allowWrites)
	registerUnassignRole(server, client, allowWrites)
	registerRecomputeUser(server, client, allowWrites)
}

// writeOutput is the structured result of a write tool: the planned request plus
// whether it was applied or previewed.
type writeOutput struct {
	Applied  bool   `json:"applied"`
	DryRun   bool   `json:"dryRun"`
	Summary  string `json:"summary"`
	Method   string `json:"method"`
	Endpoint string `json:"endpoint"`
	Body     any    `json:"body,omitempty"`
	Result   string `json:"result,omitempty"`
}

// previewWrite renders a plan as a dry-run preview (used when the write gate is
// closed).
func previewWrite(plan midpoint.Plan) (*mcp.CallToolResult, writeOutput) {
	out := writeOutput{
		DryRun:   true,
		Summary:  plan.Summary,
		Method:   plan.Method,
		Endpoint: plan.Endpoint(),
		Body:     plan.Body,
	}
	return text(fmt.Sprintf("DRY RUN — writes disabled. Would %s via %s %s.\nSet %s=true to apply.",
		plan.Summary, plan.Method, plan.Endpoint(), midpoint.EnvAllowWrites)), out
}

// runWrite applies the gate: preview when writes are disabled, otherwise apply.
func runWrite(ctx context.Context, allowWrites bool, client *midpoint.Client, plan midpoint.Plan) (*mcp.CallToolResult, writeOutput, error) {
	if !allowWrites {
		res, out := previewWrite(plan)
		return res, out, nil
	}

	res, err := client.Apply(ctx, plan)
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
	if res.OID != "" {
		out.Result = "oid=" + res.OID
	} else {
		out.Result = fmt.Sprintf("status=%d", res.StatusCode)
	}
	return text(fmt.Sprintf("Applied: %s (%s).", plan.Summary, out.Result)), out, nil
}

// --- create_user ---

type createUserInput struct {
	Name         string `json:"name" jsonschema:"the user's login name (required)"`
	FullName     string `json:"fullName,omitempty" jsonschema:"display/full name"`
	GivenName    string `json:"givenName,omitempty" jsonschema:"given (first) name"`
	FamilyName   string `json:"familyName,omitempty" jsonschema:"family (last) name"`
	EmailAddress string `json:"emailAddress,omitempty" jsonschema:"email address"`
}

func registerCreateUser(server *mcp.Server, client *midpoint.Client, allowWrites bool) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_user",
		Title:       "Create user",
		Description: "Create a new midPoint user. Requires the write gate; otherwise returns a dry-run preview.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createUserInput) (*mcp.CallToolResult, writeOutput, error) {
		plan, err := client.PlanCreateUser(midpoint.UserSpec{
			Name:         in.Name,
			FullName:     in.FullName,
			GivenName:    in.GivenName,
			FamilyName:   in.FamilyName,
			EmailAddress: in.EmailAddress,
		})
		if err != nil {
			return nil, writeOutput{}, err
		}
		return runWrite(ctx, allowWrites, client, plan)
	})
}

// --- enable_user / disable_user ---

func registerSetUserEnabled(server *mcp.Server, client *midpoint.Client, allowWrites, enable bool) {
	name, title, desc := "disable_user", "Disable user", "Disable a midPoint user (activation → disabled). Requires the write gate; otherwise a dry-run preview."
	if enable {
		name, title, desc = "enable_user", "Enable user", "Enable a midPoint user (activation → enabled). Requires the write gate; otherwise a dry-run preview."
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        name,
		Title:       title,
		Description: desc,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in oidInput) (*mcp.CallToolResult, writeOutput, error) {
		plan, err := client.PlanSetUserEnabled(in.OID, enable)
		if err != nil {
			return nil, writeOutput{}, err
		}
		return runWrite(ctx, allowWrites, client, plan)
	})
}

// --- assign_role / unassign_role ---

type roleAssignmentInput struct {
	UserOID string `json:"userOid" jsonschema:"OID of the user"`
	RoleOID string `json:"roleOid" jsonschema:"OID of the role"`
}

func registerAssignRole(server *mcp.Server, client *midpoint.Client, allowWrites bool) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "assign_role",
		Title:       "Assign role",
		Description: "Assign a role to a user. Requires the write gate; otherwise a dry-run preview.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in roleAssignmentInput) (*mcp.CallToolResult, writeOutput, error) {
		plan, err := client.PlanAssignRole(in.UserOID, in.RoleOID)
		if err != nil {
			return nil, writeOutput{}, err
		}
		return runWrite(ctx, allowWrites, client, plan)
	})
}

func registerUnassignRole(server *mcp.Server, client *midpoint.Client, allowWrites bool) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "unassign_role",
		Title:       "Unassign role",
		Description: "Remove a user's assignment to a role. Requires the write gate; otherwise a dry-run preview.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in roleAssignmentInput) (*mcp.CallToolResult, writeOutput, error) {
		// Resolves the assignment id via a read even in dry-run, so the preview is accurate.
		plan, err := client.PlanUnassignRole(ctx, in.UserOID, in.RoleOID)
		if err != nil {
			return nil, writeOutput{}, err
		}
		return runWrite(ctx, allowWrites, client, plan)
	})
}

// --- recompute_user ---

func registerRecomputeUser(server *mcp.Server, client *midpoint.Client, allowWrites bool) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recompute_user",
		Title:       "Recompute user",
		Description: "Recompute (reconcile) a user so midPoint re-evaluates policies and propagates changes. Requires the write gate; otherwise a dry-run preview.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in oidInput) (*mcp.CallToolResult, writeOutput, error) {
		plan, err := client.PlanRecomputeUser(in.OID)
		if err != nil {
			return nil, writeOutput{}, err
		}
		return runWrite(ctx, allowWrites, client, plan)
	})
}
