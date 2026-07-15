package midpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Plan describes a pending write exactly as it would be sent to midPoint. The
// tool layer either previews it (write gate off) or Applies it (gate on). Body
// is a plain Go value (map/struct) so it both marshals to the request body and
// renders cleanly in a dry-run preview.
type Plan struct {
	Method  string
	Path    string // relative to /ws/rest, e.g. "/users/<oid>"
	Query   url.Values
	Summary string
	Body    any
}

// Endpoint returns the full display path (including /ws/rest and any query) for
// previews and logs.
func (p Plan) Endpoint() string {
	e := restPrefix + p.Path
	if len(p.Query) > 0 {
		e += "?" + p.Query.Encode()
	}
	return e
}

// ApplyResult reports the outcome of an applied Plan.
type ApplyResult struct {
	StatusCode int
	OID        string // new object oid, parsed from the Location header on create
	Location   string
}

// Apply executes a Plan against midPoint. It is only reached once the tool layer
// has confirmed the write gate is open.
func (c *Client) Apply(ctx context.Context, p Plan) (ApplyResult, error) {
	var body []byte
	if p.Body != nil {
		b, err := json.Marshal(p.Body)
		if err != nil {
			return ApplyResult{}, fmt.Errorf("marshaling request body: %w", err)
		}
		body = b
	}

	resp, err := c.doFull(ctx, p.Method, p.Path, p.Query, body)
	if err != nil {
		return ApplyResult{}, err
	}

	res := ApplyResult{StatusCode: resp.StatusCode}
	if loc := resp.Header.Get("Location"); loc != "" {
		res.Location = loc
		res.OID = oidFromLocation(loc)
	}
	return res, nil
}

// UserSpec is the input to a create_user plan. Name is required.
type UserSpec struct {
	Name         string
	FullName     string
	GivenName    string
	FamilyName   string
	EmailAddress string
}

// PlanCreateUser builds a plan to create a user (POST /ws/rest/users).
func (c *Client) PlanCreateUser(spec UserSpec) (Plan, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return Plan{}, fmt.Errorf("name is required")
	}
	user := map[string]any{"name": name}
	for k, v := range map[string]string{
		"fullName":     spec.FullName,
		"givenName":    spec.GivenName,
		"familyName":   spec.FamilyName,
		"emailAddress": spec.EmailAddress,
	} {
		if strings.TrimSpace(v) != "" {
			user[k] = v
		}
	}
	return Plan{
		Method:  http.MethodPost,
		Path:    "/" + collUsers,
		Summary: fmt.Sprintf("Create user %q", name),
		Body:    map[string]any{"user": user},
	}, nil
}

// PlanSetUserEnabled builds a plan to enable or disable a user by replacing
// activation/administrativeStatus.
func (c *Client) PlanSetUserEnabled(oid string, enabled bool) (Plan, error) {
	if err := requireOID(oid); err != nil {
		return Plan{}, err
	}
	status, verb := "disabled", "Disable"
	if enabled {
		status, verb = "enabled", "Enable"
	}
	return Plan{
		Method:  http.MethodPatch,
		Path:    userPath(oid),
		Summary: fmt.Sprintf("%s user %s", verb, oid),
		Body:    modifyBody(itemDelta{ModificationType: "replace", Path: "activation/administrativeStatus", Value: status}),
	}, nil
}

// PlanAssignRole builds a plan to add a role assignment to a user.
func (c *Client) PlanAssignRole(userOID, roleOID string) (Plan, error) {
	if err := requireOID(userOID); err != nil {
		return Plan{}, fmt.Errorf("user %w", err)
	}
	if err := requireOID(roleOID); err != nil {
		return Plan{}, fmt.Errorf("role %w", err)
	}
	value := map[string]any{"targetRef": map[string]any{"oid": roleOID, "type": "RoleType"}}
	return Plan{
		Method:  http.MethodPatch,
		Path:    userPath(userOID),
		Summary: fmt.Sprintf("Assign role %s to user %s", roleOID, userOID),
		Body:    modifyBody(itemDelta{ModificationType: "add", Path: "assignment", Value: value}),
	}, nil
}

// PlanUnassignRole builds a plan to remove a user's assignment(s) to a role. It
// reads the user (a read, always permitted) to resolve the assignment container
// id(s) so the delete targets the exact value(s).
func (c *Client) PlanUnassignRole(ctx context.Context, userOID, roleOID string) (Plan, error) {
	if err := requireOID(userOID); err != nil {
		return Plan{}, fmt.Errorf("user %w", err)
	}
	if err := requireOID(roleOID); err != nil {
		return Plan{}, fmt.Errorf("role %w", err)
	}

	ids, err := c.assignmentIDsForTarget(ctx, userOID, roleOID)
	if err != nil {
		return Plan{}, err
	}
	if len(ids) == 0 {
		return Plan{}, fmt.Errorf("user %s has no direct assignment to %s", userOID, roleOID)
	}

	deltas := make([]itemDelta, 0, len(ids))
	for _, id := range ids {
		deltas = append(deltas, itemDelta{ModificationType: "delete", Path: fmt.Sprintf("assignment[%s]", id)})
	}
	return Plan{
		Method:  http.MethodPatch,
		Path:    userPath(userOID),
		Summary: fmt.Sprintf("Unassign role %s from user %s", roleOID, userOID),
		Body:    modifyBody(deltas...),
	}, nil
}

// PlanRecomputeUser builds a plan that recomputes a user via an empty
// modification with the reconcile execute option.
func (c *Client) PlanRecomputeUser(oid string) (Plan, error) {
	if err := requireOID(oid); err != nil {
		return Plan{}, err
	}
	return Plan{
		Method:  http.MethodPatch,
		Path:    userPath(oid),
		Query:   url.Values{"options": {"reconcile"}},
		Summary: fmt.Sprintf("Recompute (reconcile) user %s", oid),
		Body:    map[string]any{"objectModification": map[string]any{}},
	}, nil
}

// assignmentIDsForTarget returns the container ids of the user's assignments
// whose target is targetOID.
func (c *Client) assignmentIDsForTarget(ctx context.Context, userOID, targetOID string) ([]string, error) {
	var u userJSON
	if err := c.getObject(ctx, collUsers, userOID, false, &u); err != nil {
		return nil, err
	}
	var ids []string
	for _, raw := range u.Assignment {
		var a assignmentJSON
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, fmt.Errorf("decoding assignment: %w", err)
		}
		if t := a.target(); t != nil && t.OID == targetOID && a.ID.s != "" {
			ids = append(ids, a.ID.s)
		}
	}
	return ids, nil
}

// itemDelta is one modification within an ObjectModificationType.
type itemDelta struct {
	ModificationType string `json:"modificationType"`
	Path             string `json:"path"`
	Value            any    `json:"value,omitempty"`
}

// modifyBody wraps deltas in midPoint's {"objectModification":{"itemDelta":[...]}}.
func modifyBody(deltas ...itemDelta) map[string]any {
	return map[string]any{"objectModification": map[string]any{"itemDelta": deltas}}
}

func userPath(oid string) string { return "/" + collUsers + "/" + url.PathEscape(oid) }

func requireOID(oid string) error {
	if strings.TrimSpace(oid) == "" {
		return fmt.Errorf("oid is required")
	}
	return nil
}

// oidFromLocation extracts the last path segment (the new oid) from a Location
// header URL.
func oidFromLocation(loc string) string {
	loc = strings.TrimRight(loc, "/")
	if i := strings.LastIndex(loc, "/"); i >= 0 {
		return loc[i+1:]
	}
	return loc
}
