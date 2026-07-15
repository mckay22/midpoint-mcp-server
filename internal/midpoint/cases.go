package midpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const collCases = "cases"

// Work-item outcome URIs used when completing an approval work item.
const (
	outcomeApprove = "http://midpoint.evolveum.com/xml/ns/public/model/approval/outcome#approve"
	outcomeReject  = "http://midpoint.evolveum.com/xml/ns/public/model/approval/outcome#reject"
)

// CaseSummary is the compact shape of an approval case.
type CaseSummary struct {
	OID       string `json:"oid"`
	Name      string `json:"name,omitempty"`
	State     string `json:"state,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
	Object    string `json:"object,omitempty"`    // objectRef — the focus being changed
	Target    string `json:"target,omitempty"`    // targetRef — the role/resource requested
	Requestor string `json:"requestor,omitempty"` // requestorRef — who asked
}

// WorkItem is one approval work item. Context fields (Case/Object/Target/
// Requestor) are filled when listing an inbox; GetCase fills the per-item fields.
type WorkItem struct {
	CaseOID   string `json:"caseOid"`
	ID        string `json:"id"`
	Assignee  string `json:"assignee,omitempty"`
	Stage     int    `json:"stage,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
	Case      string `json:"case,omitempty"`
	Object    string `json:"object,omitempty"`
	Target    string `json:"target,omitempty"`
	Requestor string `json:"requestor,omitempty"`
}

// CaseDetail is a case plus its work items.
type CaseDetail struct {
	CaseSummary
	WorkItems []WorkItem `json:"workItems"`
}

type caseJSON struct {
	OID          string     `json:"oid"`
	Name         polyString `json:"name"`
	State        string     `json:"state"`
	Outcome      string     `json:"outcome"`
	ObjectRef    *refJSON   `json:"objectRef"`
	TargetRef    *refJSON   `json:"targetRef"`
	RequestorRef *refJSON   `json:"requestorRef"`
	WorkItem     flexSlice  `json:"workItem"`
}

type workItemJSON struct {
	ID          flexID   `json:"@id"`
	AssigneeRef *refJSON `json:"assigneeRef"`
	StageNumber int      `json:"stageNumber"`
	Output      *struct {
		Outcome string `json:"outcome"`
		Comment string `json:"comment"`
	} `json:"output"`
}

func (cj caseJSON) summary() CaseSummary {
	return CaseSummary{
		OID:       cj.OID,
		Name:      cj.Name.value(),
		State:     cj.State,
		Outcome:   shortURI(cj.Outcome),
		Object:    refName(cj.ObjectRef),
		Target:    refName(cj.TargetRef),
		Requestor: refName(cj.RequestorRef),
	}
}

func (cj caseJSON) items() []workItemJSON {
	out := make([]workItemJSON, 0, len(cj.WorkItem))
	for _, raw := range cj.WorkItem {
		var wi workItemJSON
		if err := json.Unmarshal(raw, &wi); err == nil {
			out = append(out, wi)
		}
	}
	return out
}

// GetCase returns a case and its work items by OID.
func (c *Client) GetCase(ctx context.Context, oid string) (CaseDetail, error) {
	var cj caseJSON
	if err := c.getObject(ctx, collCases, oid, true, &cj); err != nil {
		return CaseDetail{}, err
	}
	detail := CaseDetail{CaseSummary: cj.summary(), WorkItems: []WorkItem{}}
	for _, wi := range cj.items() {
		item := WorkItem{
			CaseOID:  cj.OID,
			ID:       wi.ID.s,
			Assignee: refName(wi.AssigneeRef),
			Stage:    wi.StageNumber,
		}
		if wi.Output != nil {
			item.Outcome = shortURI(wi.Output.Outcome)
		}
		detail.WorkItems = append(detail.WorkItems, item)
	}
	return detail, nil
}

// ListMyRequests returns approval cases the authenticated user initiated.
func (c *Client) ListMyRequests(ctx context.Context, limit int) ([]CaseSummary, error) {
	self, err := c.Self(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving self: %w", err)
	}
	filter := fmt.Sprintf("requestorRef matches (oid = %s)", quoteQueryString(self.OID))
	raws, err := c.searchRawOpts(ctx, collCases, filter, limit, true)
	if err != nil {
		return nil, err
	}
	out := make([]CaseSummary, 0, len(raws))
	for _, raw := range raws {
		var cj caseJSON
		if err := json.Unmarshal(raw, &cj); err != nil {
			return nil, fmt.Errorf("decoding case: %w", err)
		}
		out = append(out, cj.summary())
	}
	return out, nil
}

// ListWorkItems returns the authenticated user's approval inbox: open work items
// assigned to them and not yet completed.
func (c *Client) ListWorkItems(ctx context.Context, limit int) ([]WorkItem, error) {
	self, err := c.Self(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving self: %w", err)
	}
	filter := fmt.Sprintf(`state = "open" and workItem/assigneeRef matches (oid = %s)`, quoteQueryString(self.OID))
	raws, err := c.searchRawOpts(ctx, collCases, filter, limit, true)
	if err != nil {
		return nil, err
	}

	out := []WorkItem{}
	for _, raw := range raws {
		var cj caseJSON
		if err := json.Unmarshal(raw, &cj); err != nil {
			return nil, fmt.Errorf("decoding case: %w", err)
		}
		s := cj.summary()
		for _, wi := range cj.items() {
			// Only the caller's still-open work items belong in the inbox.
			if wi.Output != nil || wi.AssigneeRef == nil || wi.AssigneeRef.OID != self.OID {
				continue
			}
			out = append(out, WorkItem{
				CaseOID:   cj.OID,
				ID:        wi.ID.s,
				Stage:     wi.StageNumber,
				Assignee:  refName(wi.AssigneeRef),
				Case:      s.Name,
				Object:    s.Object,
				Target:    s.Target,
				Requestor: s.Requestor,
			})
		}
	}
	return out, nil
}

// FindRequestCase best-effort finds the newest open case requesting roleOID for
// userOID, used to surface the case created by request_role. Returns "" if none
// is found or on any error.
func (c *Client) FindRequestCase(ctx context.Context, userOID, roleOID string) string {
	filter := fmt.Sprintf(`objectRef matches (oid = %s) and targetRef matches (oid = %s) and state = "open"`,
		quoteQueryString(userOID), quoteQueryString(roleOID))
	raws, err := c.searchRaw(ctx, collCases, filter, 5)
	if err != nil || len(raws) == 0 {
		return ""
	}
	var cj caseJSON
	if json.Unmarshal(raws[0], &cj) != nil {
		return ""
	}
	return cj.OID
}

// PlanRequestRole builds a self-service role request: an assignment-add delta on
// the target user. Whether midPoint executes it directly or routes it through an
// approval case is decided by policy, not by this call. The requester identity
// is the authenticated principal (midPoint sets requestorRef); userOID is only
// the subject whose access is requested.
func (c *Client) PlanRequestRole(userOID, roleOID string) (Plan, error) {
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
		Summary: fmt.Sprintf("Request role %s for user %s (subject to approval policy)", roleOID, userOID),
		Body:    modifyBody(itemDelta{ModificationType: "add", Path: "assignment", Value: value}),
	}, nil
}

// PlanCompleteWorkItem builds a plan to approve or reject a work item.
func (c *Client) PlanCompleteWorkItem(caseOID, workItemID string, approve bool, comment string) (Plan, error) {
	if err := requireOID(caseOID); err != nil {
		return Plan{}, fmt.Errorf("case %w", err)
	}
	if strings.TrimSpace(workItemID) == "" {
		return Plan{}, fmt.Errorf("workItemId is required")
	}
	outcome, verb := outcomeReject, "Reject"
	if approve {
		outcome, verb = outcomeApprove, "Approve"
	}
	output := map[string]any{"@type": "c:AbstractWorkItemOutputType", "outcome": outcome}
	if strings.TrimSpace(comment) != "" {
		output["comment"] = comment
	}
	return Plan{
		Method: http.MethodPost,
		Path: fmt.Sprintf("/%s/%s/workItems/%s/complete",
			collCases, url.PathEscape(caseOID), url.PathEscape(workItemID)),
		Summary: fmt.Sprintf("%s work item %s in case %s", verb, workItemID, caseOID),
		Body:    map[string]any{"output": output},
	}, nil
}

// refName returns a reference's resolved display name, or "" if absent.
func refName(r *refJSON) string {
	if r == nil {
		return ""
	}
	return r.TargetName.value()
}

// shortURI returns the fragment after the last '#' or '/', so an outcome URI
// renders as "approve"/"reject".
func shortURI(s string) string {
	if s == "" {
		return ""
	}
	if i := strings.LastIndexAny(s, "#/"); i >= 0 {
		return s[i+1:]
	}
	return s
}
