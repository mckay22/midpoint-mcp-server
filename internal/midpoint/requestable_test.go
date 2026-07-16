package midpoint

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListRequestableRolesFor(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ws/rest/roles/search", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":[{"oid":"r-held","name":"held-role"},{"oid":"r-free","name":"free-role"}]}`)
	})
	mux.HandleFunc("GET /ws/rest/users/{oid}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// The target already holds r-held (via roleMembershipRef).
		_, _ = io.WriteString(w, `{"user":{"oid":"rep","name":"rep","roleMembershipRef":[{"oid":"r-held","type":"RoleType"}]}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, Username: "u", Password: "p"})

	// For a report holding r-held, only the un-held role is actionable.
	got, err := c.ListRequestableRolesFor(context.Background(), "rep", 0)
	if err != nil {
		t.Fatalf("ListRequestableRolesFor(target): %v", err)
	}
	if len(got) != 1 || got[0].OID != "r-free" {
		t.Fatalf("for report = %+v, want only free-role", got)
	}

	// No target → the full requestable catalog, unfiltered.
	all, err := c.ListRequestableRolesFor(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("ListRequestableRolesFor(self): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("no target = %+v, want the full catalog of 2", all)
	}
}
