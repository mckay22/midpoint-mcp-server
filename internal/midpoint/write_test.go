package midpoint

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// assertJSONBody marshals got and compares it structurally to wantJSON.
func assertJSONBody(t *testing.T, got any, wantJSON string) {
	t.Helper()
	gotBytes, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshaling body: %v", err)
	}
	var gotAny, wantAny any
	if err := json.Unmarshal(gotBytes, &gotAny); err != nil {
		t.Fatalf("unmarshaling got: %v", err)
	}
	if err := json.Unmarshal([]byte(wantJSON), &wantAny); err != nil {
		t.Fatalf("bad wantJSON: %v", err)
	}
	if !reflect.DeepEqual(gotAny, wantAny) {
		t.Errorf("body mismatch\n got: %s\nwant: %s", gotBytes, wantJSON)
	}
}

func TestPlanCreateUser(t *testing.T) {
	c := NewClient(Config{})
	p, err := c.PlanCreateUser(UserSpec{Name: "jack", FullName: "Jack Sparrow", EmailAddress: "j@x.com"})
	if err != nil {
		t.Fatalf("PlanCreateUser: %v", err)
	}
	if p.Method != http.MethodPost || p.Path != "/users" {
		t.Errorf("method/path = %s %s, want POST /users", p.Method, p.Path)
	}
	assertJSONBody(t, p.Body, `{"user":{"name":"jack","fullName":"Jack Sparrow","emailAddress":"j@x.com"}}`)
}

func TestPlanCreateUserRequiresName(t *testing.T) {
	c := NewClient(Config{})
	if _, err := c.PlanCreateUser(UserSpec{FullName: "no name"}); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestPlanSetUserEnabled(t *testing.T) {
	c := NewClient(Config{})
	for _, tc := range []struct {
		enable bool
		want   string
	}{{true, "enabled"}, {false, "disabled"}} {
		p, err := c.PlanSetUserEnabled("oid-1", tc.enable)
		if err != nil {
			t.Fatalf("PlanSetUserEnabled(%v): %v", tc.enable, err)
		}
		if p.Method != http.MethodPatch || p.Path != "/users/oid-1" {
			t.Errorf("method/path = %s %s", p.Method, p.Path)
		}
		assertJSONBody(t, p.Body, `{"objectModification":{"itemDelta":[{"modificationType":"replace","path":"activation/administrativeStatus","value":"`+tc.want+`"}]}}`)
	}
}

func TestPlanAssignRole(t *testing.T) {
	c := NewClient(Config{})
	p, err := c.PlanAssignRole("user-1", "role-1")
	if err != nil {
		t.Fatalf("PlanAssignRole: %v", err)
	}
	if p.Path != "/users/user-1" {
		t.Errorf("path = %s", p.Path)
	}
	assertJSONBody(t, p.Body, `{"objectModification":{"itemDelta":[{"modificationType":"add","path":"assignment","value":{"targetRef":{"oid":"role-1","type":"RoleType"}}}]}}`)
}

func TestPlanAssignRoleValidates(t *testing.T) {
	c := NewClient(Config{})
	if _, err := c.PlanAssignRole("", "role-1"); err == nil {
		t.Error("expected error for empty user oid")
	}
	if _, err := c.PlanAssignRole("user-1", ""); err == nil {
		t.Error("expected error for empty role oid")
	}
}

func TestPlanRecomputeUser(t *testing.T) {
	c := NewClient(Config{})
	p, err := c.PlanRecomputeUser("user-1")
	if err != nil {
		t.Fatalf("PlanRecomputeUser: %v", err)
	}
	if p.Query.Get("options") != "reconcile" {
		t.Errorf("options = %q, want reconcile", p.Query.Get("options"))
	}
	if got := p.Endpoint(); got != "/ws/rest/users/user-1?options=reconcile" {
		t.Errorf("endpoint = %q", got)
	}
	assertJSONBody(t, p.Body, `{"objectModification":{}}`)
}

func TestPlanUnassignRole(t *testing.T) {
	c, _ := newTestClient(t) // serves user_get.json (assignment @id 1 → role 1111..., @id 2 → resource)
	p, err := c.PlanUnassignRole(context.Background(),
		"00000000-0000-0000-0000-000000000001", "11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("PlanUnassignRole: %v", err)
	}
	assertJSONBody(t, p.Body, `{"objectModification":{"itemDelta":[{"modificationType":"delete","path":"assignment[1]"}]}}`)
}

func TestPlanUnassignRoleNoMatch(t *testing.T) {
	c, _ := newTestClient(t)
	_, err := c.PlanUnassignRole(context.Background(),
		"00000000-0000-0000-0000-000000000001", "no-such-role")
	if err == nil {
		t.Fatal("expected error when the user has no assignment to the role")
	}
}

func TestApplyCreateParsesLocationOID(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotMethod, gotPath, gotBody = r.Method, r.URL.Path, string(b)
		w.Header().Set("Location", srv0(r)+"/ws/rest/users/new-oid-123")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Username: "u", Password: "p"})
	plan, _ := c.PlanCreateUser(UserSpec{Name: "jack"})
	res, err := c.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.OID != "new-oid-123" {
		t.Errorf("OID = %q, want new-oid-123", res.OID)
	}
	if gotMethod != http.MethodPost || gotPath != "/ws/rest/users" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	assertJSONBody(t, json.RawMessage(gotBody), `{"user":{"name":"jack"}}`)
}

// srv0 builds an absolute base for the Location header from the request.
func srv0(r *http.Request) string { return "http://" + r.Host }

func TestOIDFromLocation(t *testing.T) {
	tests := map[string]string{
		"http://h/midpoint/ws/rest/users/abc-123": "abc-123",
		"http://h/ws/rest/users/xyz/":             "xyz",
		"just-an-oid":                             "just-an-oid",
	}
	for in, want := range tests {
		if got := oidFromLocation(in); got != want {
			t.Errorf("oidFromLocation(%q) = %q, want %q", in, got, want)
		}
	}
}
