package midpoint

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	selfJSON = `{"user":{"oid":"u-self","name":"selfuser"}}`

	// One open case: work item @id 1 is the caller's and open; @id 2 belongs to
	// someone else; @id 3 is the caller's but already completed.
	caseBody = `{
		"@type":"http://midpoint.evolveum.com/xml/ns/public/common/common-3#CaseType",
		"oid":"case-1",
		"name":"Approving Superuser for Jane",
		"state":"open",
		"objectRef":{"oid":"u-jane","type":"c:UserType","targetName":"Jane Doe"},
		"targetRef":{"oid":"role-su","type":"c:RoleType","targetName":"Superuser"},
		"requestorRef":{"oid":"u-self","type":"c:UserType","targetName":"selfuser"},
		"workItem":[
			{"@id":1,"assigneeRef":{"oid":"u-self","type":"c:UserType","targetName":"selfuser"},"stageNumber":1},
			{"@id":2,"assigneeRef":{"oid":"u-other","type":"c:UserType","targetName":"Someone Else"},"stageNumber":1},
			{"@id":3,"assigneeRef":{"oid":"u-self","type":"c:UserType","targetName":"selfuser"},"stageNumber":1,
			 "output":{"outcome":"http://midpoint.evolveum.com/xml/ns/public/model/approval/outcome#approve","comment":"ok"}}
		]
	}`
)

func newCasesClient(t *testing.T) (*Client, *[]capturedRequest) {
	t.Helper()
	var reqs []capturedRequest
	serve := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			reqs = append(reqs, capturedRequest{r.Method, r.URL.Path, r.URL.RawQuery, string(b)})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/rest/self", serve(200, selfJSON))
	mux.HandleFunc("POST /ws/rest/cases/search", serve(200, `{"object":[`+caseBody+`]}`))
	mux.HandleFunc("GET /ws/rest/cases/{oid}", serve(200, `{"case":`+caseBody+`}`))
	mux.HandleFunc("POST /ws/rest/cases/{caseOid}/workItems/{wid}/complete", serve(204, ""))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewClient(Config{BaseURL: srv.URL, Username: "u", Password: "p"}), &reqs
}

func TestGetCase(t *testing.T) {
	c, reqs := newCasesClient(t)

	detail, err := c.GetCase(context.Background(), "case-1")
	if err != nil {
		t.Fatalf("GetCase: %v", err)
	}
	if detail.State != "open" || detail.Target != "Superuser" || detail.Requestor != "selfuser" || detail.Object != "Jane Doe" {
		t.Errorf("case summary = %+v", detail.CaseSummary)
	}
	if len(detail.WorkItems) != 3 {
		t.Fatalf("got %d work items, want 3 (GetCase lists all)", len(detail.WorkItems))
	}
	// The completed item's outcome URI should render short.
	if detail.WorkItems[2].Outcome != "approve" {
		t.Errorf("work item 3 outcome = %q, want approve", detail.WorkItems[2].Outcome)
	}
	if q := lastRequest(t, reqs).rawQuery; q != "options=resolveNames" {
		t.Errorf("query = %q, want options=resolveNames", q)
	}
}

func TestListMyRequests(t *testing.T) {
	c, reqs := newCasesClient(t)

	cases, err := c.ListMyRequests(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListMyRequests: %v", err)
	}
	if len(cases) != 1 || cases[0].OID != "case-1" || cases[0].Target != "Superuser" {
		t.Fatalf("cases = %+v", cases)
	}
	// The search filter must scope to the authenticated requestor.
	var sr searchRequest
	if err := json.Unmarshal([]byte(lastRequest(t, reqs).body), &sr); err != nil {
		t.Fatalf("decoding search body: %v", err)
	}
	if sr.Query.Filter == nil || !strings.Contains(sr.Query.Filter.Text, `requestorRef matches (oid = "u-self")`) {
		t.Errorf("filter = %+v, want requestorRef scoped to self", sr.Query.Filter)
	}
}

func TestListWorkItems(t *testing.T) {
	c, reqs := newCasesClient(t)

	items, err := c.ListWorkItems(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
	// Only work item @id 1 qualifies: assigned to self and still open.
	if len(items) != 1 {
		t.Fatalf("got %d work items, want 1 (self + open only): %+v", len(items), items)
	}
	got := items[0]
	if got.ID != "1" || got.CaseOID != "case-1" || got.Target != "Superuser" || got.Requestor != "selfuser" {
		t.Errorf("work item = %+v", got)
	}

	var sr searchRequest
	if err := json.Unmarshal([]byte(lastRequest(t, reqs).body), &sr); err != nil {
		t.Fatalf("decoding search body: %v", err)
	}
	text := ""
	if sr.Query.Filter != nil {
		text = sr.Query.Filter.Text
	}
	if !strings.Contains(text, `state = "open"`) || !strings.Contains(text, `workItem/assigneeRef matches (oid = "u-self")`) {
		t.Errorf("filter = %q, want open + assignee scoped to self", text)
	}
}

func TestPlanRequestRole(t *testing.T) {
	c := NewClient(Config{})
	p, err := c.PlanRequestRole("u-jane", "role-su")
	if err != nil {
		t.Fatalf("PlanRequestRole: %v", err)
	}
	if p.Method != http.MethodPatch || p.Path != "/users/u-jane" {
		t.Errorf("method/path = %s %s", p.Method, p.Path)
	}
	assertJSONBody(t, p.Body, `{"objectModification":{"itemDelta":[{"modificationType":"add","path":"assignment","value":{"targetRef":{"oid":"role-su","type":"RoleType"}}}]}}`)
}

func TestPlanCompleteWorkItem(t *testing.T) {
	c := NewClient(Config{})

	approve, err := c.PlanCompleteWorkItem("case-1", "1", true, "looks good")
	if err != nil {
		t.Fatalf("PlanCompleteWorkItem approve: %v", err)
	}
	if approve.Method != http.MethodPost || approve.Path != "/cases/case-1/workItems/1/complete" {
		t.Errorf("method/path = %s %s", approve.Method, approve.Path)
	}
	assertJSONBody(t, approve.Body, `{"output":{"@type":"c:AbstractWorkItemOutputType","outcome":"`+outcomeApprove+`","comment":"looks good"}}`)

	reject, err := c.PlanCompleteWorkItem("case-1", "1", false, "")
	if err != nil {
		t.Fatalf("PlanCompleteWorkItem reject: %v", err)
	}
	assertJSONBody(t, reject.Body, `{"output":{"@type":"c:AbstractWorkItemOutputType","outcome":"`+outcomeReject+`"}}`)
}

func TestPlanCompleteWorkItemValidates(t *testing.T) {
	c := NewClient(Config{})
	if _, err := c.PlanCompleteWorkItem("", "1", true, ""); err == nil {
		t.Error("expected error for empty case oid")
	}
	if _, err := c.PlanCompleteWorkItem("case-1", "", true, ""); err == nil {
		t.Error("expected error for empty work item id")
	}
}

func TestFindRequestCase(t *testing.T) {
	c, reqs := newCasesClient(t)
	oid := c.FindRequestCase(context.Background(), "u-jane", "role-su")
	if oid != "case-1" {
		t.Errorf("FindRequestCase = %q, want case-1", oid)
	}
	var sr searchRequest
	_ = json.Unmarshal([]byte(lastRequest(t, reqs).body), &sr)
	if sr.Query.Filter == nil ||
		!strings.Contains(sr.Query.Filter.Text, `objectRef matches (oid = "u-jane")`) ||
		!strings.Contains(sr.Query.Filter.Text, `targetRef matches (oid = "role-su")`) {
		t.Errorf("filter = %+v", sr.Query.Filter)
	}
}
