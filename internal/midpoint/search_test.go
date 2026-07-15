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

func TestSearchObjects(t *testing.T) {
	var lastBody string
	mux := http.NewServeMux()
	serve := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			lastBody = string(b)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		}
	}
	mux.HandleFunc("POST /ws/rest/orgs/search", serve(`{"object":[{"oid":"org-1","name":"IT","displayName":"IT Department","description":"tech"}]}`))
	mux.HandleFunc("POST /ws/rest/shadows/search", serve(`{"object":[{"oid":"sh-1","name":"uid=jdoe,ou=people"}]}`))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, Username: "u", Password: "p"})

	orgs, err := c.SearchObjects(context.Background(), "orgs", `name = "IT"`, 10)
	if err != nil {
		t.Fatalf("SearchObjects(orgs): %v", err)
	}
	if len(orgs) != 1 {
		t.Fatalf("got %d orgs, want 1", len(orgs))
	}
	want := ObjectSummary{OID: "org-1", Kind: "Org", Name: "IT", DisplayName: "IT Department", Description: "tech"}
	if orgs[0] != want {
		t.Errorf("org = %+v, want %+v", orgs[0], want)
	}
	// The caller's raw filter must be forwarded verbatim.
	var sr searchRequest
	_ = json.Unmarshal([]byte(lastBody), &sr)
	if sr.Query.Filter == nil || sr.Query.Filter.Text != `name = "IT"` {
		t.Errorf("filter forwarded = %+v", sr.Query.Filter)
	}

	// Different type routes to a different collection and kind.
	shadows, err := c.SearchObjects(context.Background(), "shadows", "", 5)
	if err != nil {
		t.Fatalf("SearchObjects(shadows): %v", err)
	}
	if len(shadows) != 1 || shadows[0].Kind != "Shadow" || shadows[0].Name != "uid=jdoe,ou=people" {
		t.Errorf("shadow = %+v", shadows)
	}

	// Case-insensitive type; unknown type errors with guidance.
	if _, err := c.SearchObjects(context.Background(), "ORGS", "", 1); err != nil {
		t.Errorf("uppercase type should be accepted: %v", err)
	}
	_, err = c.SearchObjects(context.Background(), "widgets", "", 1)
	if err == nil || !strings.Contains(err.Error(), "unknown object type") {
		t.Errorf("expected unknown-type error, got %v", err)
	}
}
