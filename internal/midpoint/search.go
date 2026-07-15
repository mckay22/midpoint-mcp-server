package midpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// searchTarget maps a friendly object-type name to its REST collection and a
// display kind. These are the top-level types an ad-hoc report draws on.
type searchTarget struct {
	collection string
	kind       string
}

var searchTargets = map[string]searchTarget{
	"users":     {collUsers, "User"},
	"roles":     {collRoles, "Role"},
	"resources": {collResources, "Resource"},
	"orgs":      {"orgs", "Org"},
	"services":  {"services", "Service"},
	"shadows":   {"shadows", "Shadow"},
}

// SearchObjectTypes lists the accepted object-type names (sorted).
func SearchObjectTypes() []string {
	out := make([]string, 0, len(searchTargets))
	for k := range searchTargets {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ObjectSummary is the compact, type-agnostic result of SearchObjects.
type ObjectSummary struct {
	OID         string `json:"oid"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
}

type genericObjectJSON struct {
	OID         string     `json:"oid"`
	Name        polyString `json:"name"`
	DisplayName polyString `json:"displayName"`
	Description string     `json:"description"`
}

// SearchObjects runs a filtered search over one object type using a raw midPoint
// query-language filter (empty filter = match all), returning compact summaries.
// It powers ad-hoc reports (orphaned accounts, unused roles, etc.); the assistant
// composes the filter. It is read-only and, in resource-server mode, executes as
// the mapped user so midPoint enforces that user's authorizations.
func (c *Client) SearchObjects(ctx context.Context, objectType, filter string, limit int) ([]ObjectSummary, error) {
	target, ok := searchTargets[strings.ToLower(strings.TrimSpace(objectType))]
	if !ok {
		return nil, fmt.Errorf("unknown object type %q; use one of: %s", objectType, strings.Join(SearchObjectTypes(), ", "))
	}

	raws, err := c.searchRaw(ctx, target.collection, strings.TrimSpace(filter), limit)
	if err != nil {
		return nil, err
	}
	out := make([]ObjectSummary, 0, len(raws))
	for _, raw := range raws {
		var o genericObjectJSON
		if err := json.Unmarshal(raw, &o); err != nil {
			return nil, fmt.Errorf("decoding %s: %w", target.kind, err)
		}
		out = append(out, ObjectSummary{
			OID:         o.OID,
			Kind:        target.kind,
			Name:        o.Name.value(),
			DisplayName: o.DisplayName.value(),
			Description: o.Description,
		})
	}
	return out, nil
}
