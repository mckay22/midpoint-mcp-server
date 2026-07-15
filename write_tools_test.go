package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mckay22/midpoint-mcp-server/internal/midpoint"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type recordedReq struct {
	method string
	path   string
	body   string
}

// mockMidpointWrite records every request and answers write endpoints. Its GET
// returns a user assigned to role-1 (container @id 5) so unassign_role can
// resolve the id.
func mockMidpointWrite(t *testing.T) (*httptest.Server, *[]recordedReq) {
	t.Helper()
	var reqs []recordedReq
	record := func(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			reqs = append(reqs, recordedReq{r.Method, r.URL.Path, string(b)})
			next(w, r, string(b))
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /ws/rest/users", record(func(w http.ResponseWriter, r *http.Request, _ string) {
		w.Header().Set("Location", "http://"+r.Host+"/ws/rest/users/created-oid")
		w.WriteHeader(http.StatusCreated)
	}))
	mux.HandleFunc("GET /ws/rest/users/{oid}", record(func(w http.ResponseWriter, _ *http.Request, _ string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"user":{"oid":"user-1","name":"jack","assignment":[{"@id":5,"targetRef":{"oid":"role-1","type":"c:RoleType"}}]}}`)
	}))
	mux.HandleFunc("PATCH /ws/rest/users/{oid}", record(func(w http.ResponseWriter, _ *http.Request, _ string) {
		w.WriteHeader(http.StatusOK)
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &reqs
}

func connectWithWrites(t *testing.T, srv *httptest.Server, allowWrites bool) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	client := midpoint.NewClient(midpoint.Config{BaseURL: srv.URL, Username: "u", Password: "p"})
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	registerWriteTools(server, client, allowWrites)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	mc := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := mc.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool(%s) tool error: %v", name, res.Content)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	return m
}

// allWriteCalls returns each write tool with valid arguments.
func allWriteCalls() []struct {
	tool string
	args map[string]any
} {
	return []struct {
		tool string
		args map[string]any
	}{
		{"create_user", map[string]any{"name": "jack"}},
		{"disable_user", map[string]any{"oid": "user-1"}},
		{"enable_user", map[string]any{"oid": "user-1"}},
		{"assign_role", map[string]any{"userOid": "user-1", "roleOid": "role-1"}},
		{"unassign_role", map[string]any{"userOid": "user-1", "roleOid": "role-1"}},
		{"recompute_user", map[string]any{"oid": "user-1"}},
	}
}

func TestWriteGateOffPreviewsOnly(t *testing.T) {
	srv, reqs := mockMidpointWrite(t)
	cs := connectWithWrites(t, srv, false) // gate OFF

	for _, c := range allWriteCalls() {
		out := callTool(t, cs, c.tool, c.args)
		if out["dryRun"] != true {
			t.Errorf("%s: dryRun = %v, want true", c.tool, out["dryRun"])
		}
		if out["applied"] != false {
			t.Errorf("%s: applied = %v, want false", c.tool, out["applied"])
		}
		// The preview still shows what would be sent.
		if out["endpoint"] == "" || out["method"] == "" {
			t.Errorf("%s: preview missing method/endpoint: %v", c.tool, out)
		}
	}

	// The only permitted request is the read GET (unassign resolving the id).
	for _, r := range *reqs {
		if r.method != http.MethodGet {
			t.Errorf("write gate off, but a %s %s request was made", r.method, r.path)
		}
	}
}

func TestWriteGateOnApplies(t *testing.T) {
	srv, reqs := mockMidpointWrite(t)
	cs := connectWithWrites(t, srv, true) // gate ON

	// disable_user → PATCH with the replace-disabled delta.
	out := callTool(t, cs, "disable_user", map[string]any{"oid": "user-1"})
	if out["applied"] != true || out["dryRun"] != false {
		t.Errorf("disable_user: applied=%v dryRun=%v", out["applied"], out["dryRun"])
	}
	patch := findReq(*reqs, http.MethodPatch, "/ws/rest/users/user-1")
	if patch == nil {
		t.Fatal("disable_user did not PATCH midPoint")
	}
	if !strings.Contains(patch.body, "activation/administrativeStatus") || !strings.Contains(patch.body, `"disabled"`) {
		t.Errorf("disable delta body = %s", patch.body)
	}

	// create_user → POST, and the new oid is parsed from Location.
	out = callTool(t, cs, "create_user", map[string]any{"name": "jack"})
	if out["applied"] != true {
		t.Errorf("create_user applied = %v", out["applied"])
	}
	if got, _ := out["result"].(string); got != "oid=created-oid" {
		t.Errorf("create_user result = %q, want oid=created-oid", got)
	}
	if findReq(*reqs, http.MethodPost, "/ws/rest/users") == nil {
		t.Error("create_user did not POST midPoint")
	}
}

func findReq(reqs []recordedReq, method, path string) *recordedReq {
	for i := range reqs {
		if reqs[i].method == method && reqs[i].path == path {
			return &reqs[i]
		}
	}
	return nil
}
