package midpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// REST collection names.
const (
	collUsers     = "users"
	collRoles     = "roles"
	collResources = "resources"
)

// Result-size bounds shared by search and list operations.
const (
	defaultLimit = 20
	maxLimit     = 100
)

// SearchOptions parameterizes SearchUsers. An OID short-circuits to a direct
// get; otherwise Query is matched (substring) against name, full name, and
// email.
type SearchOptions struct {
	Query string
	OID   string
	Limit int
}

// SearchUsers finds users by free-text query, or returns a single user when
// OID is set.
func (c *Client) SearchUsers(ctx context.Context, opts SearchOptions) ([]UserSummary, error) {
	if oid := strings.TrimSpace(opts.OID); oid != "" {
		u, err := c.GetUser(ctx, oid)
		if err != nil {
			return nil, err
		}
		return []UserSummary{u.UserSummary}, nil
	}

	raws, err := c.searchRaw(ctx, collUsers, userFilter(opts.Query), opts.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]UserSummary, 0, len(raws))
	for _, raw := range raws {
		var u userJSON
		if err := json.Unmarshal(raw, &u); err != nil {
			return nil, fmt.Errorf("decoding user: %w", err)
		}
		out = append(out, u.summary())
	}
	return out, nil
}

// GetUser returns a single user by OID, with reference names resolved.
func (c *Client) GetUser(ctx context.Context, oid string) (UserDetail, error) {
	var u userJSON
	if err := c.getObject(ctx, collUsers, oid, true, &u); err != nil {
		return UserDetail{}, err
	}
	return u.detail(), nil
}

// GetUserAssignments returns a user's direct assignments plus the computed
// effective membership, marking each membership direct or inherited.
func (c *Client) GetUserAssignments(ctx context.Context, oid string) (UserAssignments, error) {
	var u userJSON
	if err := c.getObject(ctx, collUsers, oid, true, &u); err != nil {
		return UserAssignments{}, err
	}

	// Initialize slices so an assignment-less user still marshals its arrays as
	// [] (not null), which the MCP output-schema validation requires.
	result := UserAssignments{
		User:        u.summary(),
		Assignments: []Assignment{},
		Effective:   []Membership{},
	}
	direct := make(map[string]bool)

	for _, raw := range u.Assignment {
		var a assignmentJSON
		if err := json.Unmarshal(raw, &a); err != nil {
			return UserAssignments{}, fmt.Errorf("decoding assignment: %w", err)
		}
		entry := Assignment{Status: a.Activation.status(), Subtype: a.Subtype}
		if t := a.target(); t != nil {
			entry.TargetOID = t.OID
			entry.TargetName = t.TargetName.value()
			entry.TargetType = cleanType(t.Type)
			entry.Relation = t.Relation
			if t.OID != "" {
				direct[t.OID] = true
			}
		}
		result.Assignments = append(result.Assignments, entry)
	}

	for _, raw := range u.RoleMembershipRef {
		var m refJSON
		if err := json.Unmarshal(raw, &m); err != nil {
			return UserAssignments{}, fmt.Errorf("decoding membership: %w", err)
		}
		result.Effective = append(result.Effective, Membership{
			OID:    m.OID,
			Name:   m.TargetName.value(),
			Type:   cleanType(m.Type),
			Direct: direct[m.OID],
		})
	}
	return result, nil
}

// ListRoles returns up to limit roles.
func (c *Client) ListRoles(ctx context.Context, limit int) ([]RoleSummary, error) {
	return c.listRoles(ctx, "", limit)
}

// ListRequestableRoles returns up to limit roles marked requestable (offered in
// midPoint's request catalog). It runs as the calling identity, so in
// resource-server mode midPoint returns only the roles that user is authorized to
// see — the requestable-and-visible set they can self-request.
func (c *Client) ListRequestableRoles(ctx context.Context, limit int) ([]RoleSummary, error) {
	return c.listRoles(ctx, "requestable = true", limit)
}

// listRoles searches the role collection with an optional text filter (empty =
// all) and returns compact summaries.
func (c *Client) listRoles(ctx context.Context, filter string, limit int) ([]RoleSummary, error) {
	raws, err := c.searchRaw(ctx, collRoles, filter, limit)
	if err != nil {
		return nil, err
	}
	out := make([]RoleSummary, 0, len(raws))
	for _, raw := range raws {
		var r roleJSON
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("decoding role: %w", err)
		}
		out = append(out, r.summary())
	}
	return out, nil
}

// GetRole returns a single role by OID.
func (c *Client) GetRole(ctx context.Context, oid string) (RoleDetail, error) {
	var r roleJSON
	if err := c.getObject(ctx, collRoles, oid, false, &r); err != nil {
		return RoleDetail{}, err
	}
	return r.detail(), nil
}

// ListResources returns up to limit connected resources.
func (c *Client) ListResources(ctx context.Context, limit int) ([]ResourceSummary, error) {
	raws, err := c.searchRaw(ctx, collResources, "", limit)
	if err != nil {
		return nil, err
	}
	out := make([]ResourceSummary, 0, len(raws))
	for _, raw := range raws {
		var r resourceJSON
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("decoding resource: %w", err)
		}
		out = append(out, r.summary())
	}
	return out, nil
}

// GetResource returns a single resource by OID, resolving the connector name.
func (c *Client) GetResource(ctx context.Context, oid string) (ResourceDetail, error) {
	var r resourceJSON
	if err := c.getObject(ctx, collResources, oid, true, &r); err != nil {
		return ResourceDetail{}, err
	}
	return r.detail(), nil
}

// getObject fetches one object by OID and decodes it (after unwrapping the
// type envelope) into dst.
func (c *Client) getObject(ctx context.Context, collection, oid string, resolveNames bool, dst any) error {
	if strings.TrimSpace(oid) == "" {
		return fmt.Errorf("oid is required")
	}
	var query url.Values
	if resolveNames {
		query = url.Values{"options": {"resolveNames"}}
	}
	body, err := c.get(ctx, "/"+collection+"/"+url.PathEscape(oid), query)
	if err != nil {
		return err
	}
	obj, err := unwrapObject(body)
	if err != nil {
		return fmt.Errorf("decoding %s response: %w", collection, err)
	}
	if err := json.Unmarshal(obj, dst); err != nil {
		return fmt.Errorf("decoding %s: %w", collection, err)
	}
	return nil
}

// searchRaw POSTs a text-filter search (empty filter = match all) with a paging
// cap and returns the raw matched objects.
func (c *Client) searchRaw(ctx context.Context, collection, filterText string, limit int) ([]json.RawMessage, error) {
	return c.searchRawOpts(ctx, collection, filterText, limit, false)
}

// searchRawOpts is searchRaw with an optional resolveNames request so returned
// references carry their targetName.
func (c *Client) searchRawOpts(ctx context.Context, collection, filterText string, limit int, resolveNames bool) ([]json.RawMessage, error) {
	req := searchRequest{}
	req.Query.Paging = &searchPaging{MaxSize: clampLimit(limit)}
	if filterText != "" {
		req.Query.Filter = &searchFilter{Text: filterText}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	var query url.Values
	if resolveNames {
		query = url.Values{"options": {"resolveNames"}}
	}
	respBody, err := c.post(ctx, "/"+collection+"/search", query, body)
	if err != nil {
		return nil, err
	}
	objects, err := parseObjectList(respBody)
	if err != nil {
		return nil, fmt.Errorf("decoding %s search response: %w", collection, err)
	}
	return objects, nil
}

// searchRequest models the REST search body: {"query":{"filter":{"text":...},"paging":{...}}}.
type searchRequest struct {
	Query searchQuery `json:"query"`
}

type searchQuery struct {
	Filter *searchFilter `json:"filter,omitempty"`
	Paging *searchPaging `json:"paging,omitempty"`
}

type searchFilter struct {
	Text string `json:"text"`
}

type searchPaging struct {
	MaxSize int `json:"maxSize,omitempty"`
	Offset  int `json:"offset,omitempty"`
}

// userFilter builds a midPoint query-language filter matching the query against
// name, full name, or email. An empty query matches all users.
func userFilter(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	v := quoteQueryString(q)
	return fmt.Sprintf("name contains %[1]s or fullName contains %[1]s or emailAddress contains %[1]s", v)
}

// quoteQueryString renders s as a midPoint query-language string literal,
// escaping backslashes and quotes so a caller-supplied value can't break out of
// the literal or inject query syntax.
func quoteQueryString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func clampLimit(n int) int {
	switch {
	case n <= 0:
		return defaultLimit
	case n > maxLimit:
		return maxLimit
	default:
		return n
	}
}
