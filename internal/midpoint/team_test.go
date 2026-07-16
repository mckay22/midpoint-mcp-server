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

// newTeamClient serves GET /self (with the given body) and POST /users/search
// (dispatching by the request's query-language filter).
func newTeamClient(t *testing.T, selfBody string, search func(filter string) string) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/rest/self", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, selfBody)
	})
	mux.HandleFunc("POST /ws/rest/users/search", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var sr searchRequest
		_ = json.Unmarshal(body, &sr)
		filter := ""
		if sr.Query.Filter != nil {
			filter = sr.Query.Filter.Text
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, search(filter))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewClient(Config{BaseURL: srv.URL, Username: "svc", Password: "p"})
}

// selfWithOrgs renders a /self body whose parentOrgRef carries the given refs.
func selfWithOrgs(refs ...orgRef) string {
	items := make([]map[string]string, 0, len(refs))
	for _, r := range refs {
		items = append(items, map[string]string{"oid": r.OID, "relation": r.Relation, "type": "OrgType"})
	}
	b, _ := json.Marshal(map[string]any{
		"user": map[string]any{"oid": "me", "name": "mgr", "parentOrgRef": items},
	})
	return string(b)
}

func names(us []UserSummary) []string {
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.Name
	}
	return out
}

func TestListMyTeam(t *testing.T) {
	self := selfWithOrgs(
		orgRef{OID: "org-mgd", Relation: "org:manager"},
		orgRef{OID: "org-mem", Relation: "org:default"},
	)
	c := newTeamClient(t, self, func(filter string) string {
		// Reports = members (default relation) of the managed org; the caller
		// itself may come back in the member list and must be dropped.
		if strings.Contains(filter, "org-mgd") && strings.Contains(filter, "relation = default") {
			return `{"object":[{"oid":"me","name":"mgr"},{"oid":"r1","name":"rep-one"},{"oid":"r2","name":"rep-two"}]}`
		}
		return `{"object":[]}`
	})

	team, err := c.ListMyTeam(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListMyTeam: %v", err)
	}
	got := names(team)
	if len(got) != 2 || got[0] != "rep-one" || got[1] != "rep-two" {
		t.Fatalf("team = %v, want [rep-one rep-two] (caller excluded)", got)
	}
}

func TestListMyTeamNonManager(t *testing.T) {
	self := selfWithOrgs(orgRef{OID: "org-mem", Relation: "org:default"})
	searched := false
	c := newTeamClient(t, self, func(string) string {
		searched = true
		return `{"object":[]}`
	})

	team, err := c.ListMyTeam(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListMyTeam: %v", err)
	}
	if len(team) != 0 {
		t.Errorf("non-manager team = %v, want empty", names(team))
	}
	if searched {
		t.Error("must not search for members when the caller manages no org")
	}
}

func TestListMyManagers(t *testing.T) {
	self := selfWithOrgs(
		orgRef{OID: "org-mgd", Relation: "org:manager"},
		orgRef{OID: "org-mem", Relation: "org:default"},
	)
	c := newTeamClient(t, self, func(filter string) string {
		// Managers = manager-relation links on the org the caller is a member of.
		if strings.Contains(filter, "org-mem") && strings.Contains(filter, "relation = manager") {
			return `{"object":[{"oid":"boss","name":"the-boss"}]}`
		}
		return `{"object":[]}`
	})

	mgrs, err := c.ListMyManagers(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListMyManagers: %v", err)
	}
	if got := names(mgrs); len(got) != 1 || got[0] != "the-boss" {
		t.Fatalf("managers = %v, want [the-boss]", got)
	}
}

func TestRelationLocalAndIsManager(t *testing.T) {
	cases := map[string]bool{
		"org:manager": true,
		"manager":     true,
		"http://midpoint.evolveum.com/xml/ns/public/common/org-3#manager": true,
		"org:default": false,
		"":            false,
		"member":      false,
	}
	for rel, wantMgr := range cases {
		if got := (orgRef{Relation: rel}).isManager(); got != wantMgr {
			t.Errorf("isManager(%q) = %v, want %v", rel, got, wantMgr)
		}
	}
}

func TestOrgMembersFilter(t *testing.T) {
	f := orgMembersFilter([]string{"o1", "o2"}, relationManager)
	for _, want := range []string{
		`parentOrgRef matches (oid = "o1" and relation = manager)`,
		`parentOrgRef matches (oid = "o2" and relation = manager)`,
		" or ",
	} {
		if !strings.Contains(f, want) {
			t.Errorf("filter %q missing %q", f, want)
		}
	}
}
