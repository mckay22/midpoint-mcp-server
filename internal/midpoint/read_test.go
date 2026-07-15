package midpoint

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type capturedRequest struct {
	method   string
	path     string
	rawQuery string
	body     string
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return b
}

// newTestClient wires a Client to an httptest server that serves the recorded
// fixtures, and returns a pointer to the captured requests for assertions.
func newTestClient(t *testing.T) (*Client, *[]capturedRequest) {
	t.Helper()
	var reqs []capturedRequest

	serve := func(f string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			reqs = append(reqs, capturedRequest{
				method: r.Method, path: r.URL.Path, rawQuery: r.URL.RawQuery, body: string(body),
			})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fixture(t, f))
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /ws/rest/users/search", serve("users_search.json"))
	mux.HandleFunc("GET /ws/rest/users/{oid}", serve("user_get.json"))
	mux.HandleFunc("POST /ws/rest/roles/search", serve("roles_search.json"))
	mux.HandleFunc("GET /ws/rest/roles/{oid}", serve("role_get.json"))
	mux.HandleFunc("POST /ws/rest/resources/search", serve("resources_search.json"))
	mux.HandleFunc("GET /ws/rest/resources/{oid}", serve("resource_get.json"))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return NewClient(Config{BaseURL: srv.URL, Username: "u", Password: "p"}), &reqs
}

func lastRequest(t *testing.T, reqs *[]capturedRequest) capturedRequest {
	t.Helper()
	if len(*reqs) == 0 {
		t.Fatal("no request captured")
	}
	return (*reqs)[len(*reqs)-1]
}

func TestSearchUsersByQuery(t *testing.T) {
	c, reqs := newTestClient(t)

	users, err := c.SearchUsers(context.Background(), SearchOptions{Query: "doe", Limit: 5})
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("got %d users, want 2", len(users))
	}
	want0 := UserSummary{
		OID: "00000000-0000-0000-0000-000000000001", Name: "jdoe",
		FullName: "Jane Doe", EmailAddress: "jane.doe@example.com", Status: "enabled",
	}
	if users[0] != want0 {
		t.Errorf("users[0] = %+v, want %+v", users[0], want0)
	}
	// Second user exercises PolyString-object name/fullName and disabled status.
	if users[1].Name != "asmith" || users[1].FullName != "Alice Smith" || users[1].Status != "disabled" {
		t.Errorf("users[1] = %+v", users[1])
	}

	// The request must carry the OR'd contains-filter and the paging cap.
	var got searchRequest
	if err := json.Unmarshal([]byte(lastRequest(t, reqs).body), &got); err != nil {
		t.Fatalf("decoding sent body: %v", err)
	}
	wantFilter := `name contains "doe" or fullName contains "doe" or emailAddress contains "doe"`
	if got.Query.Filter == nil || got.Query.Filter.Text != wantFilter {
		t.Errorf("filter = %+v, want %q", got.Query.Filter, wantFilter)
	}
	if got.Query.Paging == nil || got.Query.Paging.MaxSize != 5 {
		t.Errorf("paging = %+v, want maxSize 5", got.Query.Paging)
	}
}

func TestSearchUsersByOID(t *testing.T) {
	c, reqs := newTestClient(t)

	users, err := c.SearchUsers(context.Background(), SearchOptions{OID: "00000000-0000-0000-0000-000000000001"})
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) != 1 || users[0].Name != "jdoe" {
		t.Fatalf("got %+v, want single jdoe", users)
	}
	// OID lookup must be a direct GET, not a search.
	req := lastRequest(t, reqs)
	if req.method != http.MethodGet || req.path != "/ws/rest/users/00000000-0000-0000-0000-000000000001" {
		t.Errorf("request = %s %s, want GET of the user", req.method, req.path)
	}
}

func TestGetUser(t *testing.T) {
	c, reqs := newTestClient(t)

	u, err := c.GetUser(context.Background(), "00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.GivenName != "Jane" || u.FamilyName != "Doe" || u.Status != "enabled" {
		t.Errorf("detail = %+v", u)
	}
	if u.AssignmentCount != 2 {
		t.Errorf("assignmentCount = %d, want 2", u.AssignmentCount)
	}
	if q := lastRequest(t, reqs).rawQuery; q != "options=resolveNames" {
		t.Errorf("query = %q, want options=resolveNames", q)
	}
}

func TestGetUserAssignments(t *testing.T) {
	c, _ := newTestClient(t)

	res, err := c.GetUserAssignments(context.Background(), "00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("GetUserAssignments: %v", err)
	}

	if len(res.Assignments) != 2 {
		t.Fatalf("got %d assignments, want 2", len(res.Assignments))
	}
	role := res.Assignments[0]
	if role.TargetName != "Superuser" || role.TargetType != "Role" || role.Relation != "org:default" || role.Status != "enabled" {
		t.Errorf("assignment[0] = %+v", role)
	}
	// Second assignment comes from a resource construction, not a targetRef.
	acct := res.Assignments[1]
	if acct.TargetName != "OpenLDAP" || acct.TargetType != "Resource" || acct.Subtype != "account" {
		t.Errorf("assignment[1] = %+v", acct)
	}

	// Effective membership: direct flag must reflect presence as a direct assignment.
	direct := map[string]bool{}
	for _, m := range res.Effective {
		direct[m.Name] = m.Direct
	}
	if len(res.Effective) != 3 {
		t.Fatalf("got %d memberships, want 3", len(res.Effective))
	}
	if !direct["Superuser"] {
		t.Error("Superuser should be direct")
	}
	if direct["End User"] || direct["IT Department"] {
		t.Error("End User and IT Department should be inherited (not direct)")
	}
}

func TestListRoles(t *testing.T) {
	c, _ := newTestClient(t)

	roles, err := c.ListRoles(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("got %d roles, want 2", len(roles))
	}
	if roles[0].DisplayName != "Superuser" || roles[0].Description != "All privileges" {
		t.Errorf("roles[0] = %+v", roles[0])
	}
}

func TestListRequestableRoles(t *testing.T) {
	c, reqs := newTestClient(t)

	roles, err := c.ListRequestableRoles(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListRequestableRoles: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("got %d roles, want 2", len(roles))
	}
	// The request must carry the requestable filter (that's the whole point).
	if body := lastRequest(t, reqs).body; !strings.Contains(body, "requestable = true") {
		t.Errorf("search body missing requestable filter: %s", body)
	}
}

func TestGetRole(t *testing.T) {
	c, _ := newTestClient(t)

	r, err := c.GetRole(context.Background(), "11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	if r.Identifier != "superuser" || r.RiskLevel != "high" || r.DisplayName != "Superuser" {
		t.Errorf("role = %+v", r)
	}
}

func TestListResourcesSingleObjectEnvelope(t *testing.T) {
	c, _ := newTestClient(t)

	// The fixture wraps a single object (not an array) to exercise flexSlice.
	resources, err := c.ListResources(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources) != 1 || resources[0].Name != "OpenLDAP" {
		t.Fatalf("got %+v, want single OpenLDAP", resources)
	}
}

func TestGetResource(t *testing.T) {
	c, _ := newTestClient(t)

	r, err := c.GetResource(context.Background(), "22222222-2222-2222-2222-222222222222")
	if err != nil {
		t.Fatalf("GetResource: %v", err)
	}
	if r.Availability != "up" || r.Connector != "ICF LDAP Connector" || r.LifecycleState != "active" {
		t.Errorf("resource = %+v", r)
	}
}

func TestUserFilterEscapesInjection(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "doe", `name contains "doe" or fullName contains "doe" or emailAddress contains "doe"`},
		{
			"quote injection is escaped",
			`x" or name startsWith "`,
			`name contains "x\" or name startsWith \"" or fullName contains "x\" or name startsWith \"" or emailAddress contains "x\" or name startsWith \""`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := userFilter(tt.in); got != tt.want {
				t.Errorf("userFilter(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestClampLimit(t *testing.T) {
	tests := []struct{ in, want int }{
		{0, defaultLimit}, {-5, defaultLimit}, {10, 10}, {maxLimit, maxLimit}, {maxLimit + 1, maxLimit}, {1000, maxLimit},
	}
	for _, tt := range tests {
		if got := clampLimit(tt.in); got != tt.want {
			t.Errorf("clampLimit(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
