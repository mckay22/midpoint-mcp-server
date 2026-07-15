package midpoint

import (
	"bytes"
	"encoding/json"
	"strings"
)

// flexSlice decodes a JSON value that midPoint may serialize as either a single
// object or an array of objects (its JSON output collapses single-element
// collections to a bare object). Either form is normalized to a slice of raw
// messages.
type flexSlice []json.RawMessage

func (s *flexSlice) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if trimmed[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return err
		}
		*s = arr
		return nil
	}
	*s = flexSlice{append(json.RawMessage(nil), trimmed...)}
	return nil
}

// objectList is midPoint's ObjectListType envelope for search/list responses.
type objectList struct {
	Object flexSlice `json:"object"`
}

// unwrapObject strips midPoint's single-object envelope (e.g. {"user": {...}})
// and returns the wrapped object, ignoring metadata keys like "@ns".
func unwrapObject(body []byte) (json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	for k, v := range m {
		if strings.HasPrefix(k, "@") {
			continue
		}
		return v, nil
	}
	// Not wrapped; hand back the original body.
	return body, nil
}

// refJSON is midPoint's ObjectReferenceType. targetName is populated only when
// the request uses ?options=resolveNames.
type refJSON struct {
	OID        string     `json:"oid"`
	Type       string     `json:"type"`
	Relation   string     `json:"relation"`
	TargetName polyString `json:"targetName"`
}

// activation carries a focus/assignment activation status.
type activation struct {
	AdministrativeStatus string `json:"administrativeStatus"`
	EffectiveStatus      string `json:"effectiveStatus"`
}

func (a *activation) status() string {
	if a == nil {
		return ""
	}
	if a.EffectiveStatus != "" {
		return a.EffectiveStatus
	}
	return a.AdministrativeStatus
}

// cleanType turns a midPoint reference QName like "c:OrgType" into a friendly
// kind like "Org".
func cleanType(qname string) string {
	t := qname
	if i := strings.LastIndex(t, ":"); i >= 0 {
		t = t[i+1:]
	}
	return strings.TrimSuffix(t, "Type")
}

// --- Users ---

// UserSummary is the compact user shape returned by search.
type UserSummary struct {
	OID          string `json:"oid"`
	Name         string `json:"name"`
	FullName     string `json:"fullName,omitempty"`
	EmailAddress string `json:"emailAddress,omitempty"`
	Status       string `json:"status,omitempty"`
}

// UserDetail extends UserSummary with the attributes get_user surfaces.
type UserDetail struct {
	UserSummary
	GivenName       string `json:"givenName,omitempty"`
	FamilyName      string `json:"familyName,omitempty"`
	AssignmentCount int    `json:"assignmentCount"`
}

type userJSON struct {
	OID               string      `json:"oid"`
	Name              polyString  `json:"name"`
	FullName          polyString  `json:"fullName"`
	GivenName         polyString  `json:"givenName"`
	FamilyName        polyString  `json:"familyName"`
	EmailAddress      string      `json:"emailAddress"`
	Activation        *activation `json:"activation"`
	Assignment        flexSlice   `json:"assignment"`
	RoleMembershipRef flexSlice   `json:"roleMembershipRef"`
}

func (u userJSON) summary() UserSummary {
	return UserSummary{
		OID:          u.OID,
		Name:         u.Name.value(),
		FullName:     u.FullName.value(),
		EmailAddress: u.EmailAddress,
		Status:       u.Activation.status(),
	}
}

func (u userJSON) detail() UserDetail {
	return UserDetail{
		UserSummary:     u.summary(),
		GivenName:       u.GivenName.value(),
		FamilyName:      u.FamilyName.value(),
		AssignmentCount: len(u.Assignment),
	}
}

// --- Assignments ---

// Assignment is a single directly-assigned entitlement on a user.
type Assignment struct {
	TargetOID  string `json:"targetOid,omitempty"`
	TargetName string `json:"targetName,omitempty"`
	TargetType string `json:"targetType,omitempty"`
	Relation   string `json:"relation,omitempty"`
	Status     string `json:"status,omitempty"`
	Subtype    string `json:"subtype,omitempty"`
}

// Membership is one effective role membership (direct or inherited).
type Membership struct {
	OID    string `json:"oid"`
	Name   string `json:"name,omitempty"`
	Type   string `json:"type,omitempty"`
	Direct bool   `json:"direct"`
}

// UserAssignments is what get_user_assignments returns: a user's direct
// assignments plus the computed effective membership, with each membership
// flagged as direct (present as an assignment) or inherited.
type UserAssignments struct {
	User        UserSummary  `json:"user"`
	Assignments []Assignment `json:"assignments"`
	Effective   []Membership `json:"effectiveMembership"`
}

type assignmentJSON struct {
	TargetRef    *refJSON    `json:"targetRef"`
	Activation   *activation `json:"activation"`
	Subtype      string      `json:"subtype"`
	Construction *struct {
		ResourceRef *refJSON `json:"resourceRef"`
	} `json:"construction"`
}

// target returns the reference an assignment points at, preferring an explicit
// targetRef and falling back to a resource construction.
func (a assignmentJSON) target() *refJSON {
	if a.TargetRef != nil {
		return a.TargetRef
	}
	if a.Construction != nil && a.Construction.ResourceRef != nil {
		return a.Construction.ResourceRef
	}
	return nil
}

// --- Roles ---

// RoleSummary is the compact role shape returned by list_roles.
type RoleSummary struct {
	OID         string `json:"oid"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
}

// RoleDetail extends RoleSummary with get_role attributes.
type RoleDetail struct {
	RoleSummary
	Identifier string `json:"identifier,omitempty"`
	RiskLevel  string `json:"riskLevel,omitempty"`
}

type roleJSON struct {
	OID         string     `json:"oid"`
	Name        polyString `json:"name"`
	DisplayName polyString `json:"displayName"`
	Description string     `json:"description"`
	Identifier  string     `json:"identifier"`
	RiskLevel   string     `json:"riskLevel"`
}

func (r roleJSON) summary() RoleSummary {
	return RoleSummary{
		OID:         r.OID,
		Name:        r.Name.value(),
		DisplayName: r.DisplayName.value(),
		Description: r.Description,
	}
}

func (r roleJSON) detail() RoleDetail {
	return RoleDetail{
		RoleSummary: r.summary(),
		Identifier:  r.Identifier,
		RiskLevel:   r.RiskLevel,
	}
}

// --- Resources ---

// ResourceSummary is the compact resource shape returned by list_resources.
type ResourceSummary struct {
	OID         string `json:"oid"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ResourceDetail extends ResourceSummary with connection status where midPoint
// reports it.
type ResourceDetail struct {
	ResourceSummary
	LifecycleState string `json:"lifecycleState,omitempty"`
	Availability   string `json:"availability,omitempty"`
	Connector      string `json:"connector,omitempty"`
}

type resourceJSON struct {
	OID              string     `json:"oid"`
	Name             polyString `json:"name"`
	Description      string     `json:"description"`
	LifecycleState   string     `json:"lifecycleState"`
	ConnectorRef     *refJSON   `json:"connectorRef"`
	OperationalState *struct {
		LastAvailabilityStatus string `json:"lastAvailabilityStatus"`
	} `json:"operationalState"`
}

func (r resourceJSON) summary() ResourceSummary {
	return ResourceSummary{
		OID:         r.OID,
		Name:        r.Name.value(),
		Description: r.Description,
	}
}

func (r resourceJSON) detail() ResourceDetail {
	d := ResourceDetail{
		ResourceSummary: r.summary(),
		LifecycleState:  r.LifecycleState,
	}
	if r.OperationalState != nil {
		d.Availability = r.OperationalState.LastAvailabilityStatus
	}
	if r.ConnectorRef != nil {
		d.Connector = r.ConnectorRef.TargetName.value()
	}
	return d
}
