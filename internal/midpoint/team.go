package midpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Org relation local parts. midPoint models management through org structure: a
// user assigned to an org with the manager relation manages it; members hold the
// default relation. These local parts are used both to classify the caller's own
// parentOrgRef and (as trusted constants) in the membership query filter.
const (
	relationManager = "manager"
	relationDefault = "default"
)

// isManager reports whether the ref links the user as a manager of the org.
func (r orgRef) isManager() bool { return relationLocal(r.Relation) == relationManager }

// relationLocal returns the local part of a relation QName ("org:manager" →
// "manager"); an empty relation (the default membership) returns "".
func relationLocal(rel string) string {
	if i := strings.LastIndexAny(rel, ":#/"); i >= 0 {
		return rel[i+1:]
	}
	return rel
}

// ListMyTeam returns the caller's direct reports: the members of the orgs the
// caller manages. It is empty when the caller manages no org. Read-only, and in
// resource-server mode it runs as the caller so midPoint scopes it to what that
// manager may see.
func (c *Client) ListMyTeam(ctx context.Context, limit int) ([]UserSummary, error) {
	self, err := c.selfUser(ctx)
	if err != nil {
		return nil, err
	}
	managed := orgOIDsByRole(self.parentOrgs(), true)
	if len(managed) == 0 {
		return nil, nil
	}
	return c.orgUsers(ctx, managed, relationDefault, self.OID, limit)
}

// ListMyManagers returns the managers of the orgs the caller is a member of —
// who the caller reports to.
func (c *Client) ListMyManagers(ctx context.Context, limit int) ([]UserSummary, error) {
	self, err := c.selfUser(ctx)
	if err != nil {
		return nil, err
	}
	memberOf := orgOIDsByRole(self.parentOrgs(), false)
	if len(memberOf) == 0 {
		return nil, nil
	}
	return c.orgUsers(ctx, memberOf, relationManager, self.OID, limit)
}

// selfUser fetches the caller's full user object (GET /self), which carries the
// parentOrgRef that Self() omits.
func (c *Client) selfUser(ctx context.Context) (userJSON, error) {
	body, err := c.get(ctx, "/self", nil)
	if err != nil {
		return userJSON{}, err
	}
	var resp struct {
		User userJSON `json:"user"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return userJSON{}, fmt.Errorf("decoding /self: %w", err)
	}
	return resp.User, nil
}

// orgOIDsByRole selects the org OIDs the refs link to as manager (wantManager) or
// as non-manager member (!wantManager).
func orgOIDsByRole(refs []orgRef, wantManager bool) []string {
	var out []string
	for _, r := range refs {
		if r.OID != "" && r.isManager() == wantManager {
			out = append(out, r.OID)
		}
	}
	return out
}

// orgUsers searches for users linked to any of orgOIDs with the given relation,
// excluding excludeOID (the caller) and de-duplicating across orgs.
func (c *Client) orgUsers(ctx context.Context, orgOIDs []string, relation, excludeOID string, limit int) ([]UserSummary, error) {
	raws, err := c.searchRaw(ctx, collUsers, orgMembersFilter(orgOIDs, relation), limit)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	if excludeOID != "" {
		seen[excludeOID] = true
	}
	out := make([]UserSummary, 0, len(raws))
	for _, raw := range raws {
		var u userJSON
		if err := json.Unmarshal(raw, &u); err != nil {
			return nil, fmt.Errorf("decoding org member: %w", err)
		}
		if u.OID == "" || seen[u.OID] {
			continue
		}
		seen[u.OID] = true
		out = append(out, u.summary())
	}
	return out, nil
}

// orgMembersFilter builds a query matching users linked to any of orgOIDs with
// the given relation. The relation is a trusted constant (manager/default); OIDs
// are quoted.
func orgMembersFilter(orgOIDs []string, relation string) string {
	parts := make([]string, 0, len(orgOIDs))
	for _, oid := range orgOIDs {
		parts = append(parts, fmt.Sprintf("parentOrgRef matches (oid = %s and relation = %s)", quoteQueryString(oid), relation))
	}
	return strings.Join(parts, " or ")
}
