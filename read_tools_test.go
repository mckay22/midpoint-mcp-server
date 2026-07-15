package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mckay22/midpoint-mcp-server/internal/midpoint"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mockMidpoint returns minimal but valid midPoint REST responses. The user has
// no assignments, so get_user_assignments must still marshal its arrays as []
// (not null) and pass the SDK's output-schema validation.
func mockMidpoint(t *testing.T) *httptest.Server {
	t.Helper()
	json := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ws/rest/users/search", json(`{"object":[{"oid":"oid-1","name":"jdoe","fullName":"Jane Doe","emailAddress":"j@x.com"}]}`))
	mux.HandleFunc("GET /ws/rest/users/{oid}", json(`{"user":{"oid":"oid-1","name":"jdoe","givenName":"Jane","familyName":"Doe","fullName":"Jane Doe","emailAddress":"j@x.com","activation":{"effectiveStatus":"enabled"}}}`))
	mux.HandleFunc("POST /ws/rest/roles/search", json(`{"object":[{"oid":"role-1","name":"Superuser","description":"all"}]}`))
	mux.HandleFunc("GET /ws/rest/roles/{oid}", json(`{"role":{"oid":"role-1","name":"Superuser"}}`))
	mux.HandleFunc("POST /ws/rest/resources/search", json(`{"object":[{"oid":"res-1","name":"LDAP"}]}`))
	mux.HandleFunc("GET /ws/rest/resources/{oid}", json(`{"resource":{"oid":"res-1","name":"LDAP","operationalState":{"lastAvailabilityStatus":"up"}}}`))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestReadToolsRoundTrip(t *testing.T) {
	ctx := context.Background()
	srv := mockMidpoint(t)
	client := midpoint.NewClient(midpoint.Config{BaseURL: srv.URL, Username: "u", Password: "p"})

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	registerReadTools(server, client)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	mc := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := mc.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	calls := []struct {
		tool string
		args map[string]any
	}{
		{"search_users", map[string]any{"query": "doe"}},
		{"get_user", map[string]any{"oid": "oid-1"}},
		{"get_user_assignments", map[string]any{"oid": "oid-1"}},
		{"list_roles", map[string]any{}},
		{"get_role", map[string]any{"oid": "role-1"}},
		{"list_resources", map[string]any{}},
		{"get_resource", map[string]any{"oid": "res-1"}},
	}

	for _, c := range calls {
		t.Run(c.tool, func(t *testing.T) {
			// A non-nil error or IsError here means input/output schema validation
			// (or the call itself) failed.
			res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: c.tool, Arguments: c.args})
			if err != nil {
				t.Fatalf("CallTool(%s): %v", c.tool, err)
			}
			if res.IsError {
				t.Fatalf("CallTool(%s) returned tool error: %v", c.tool, res.Content)
			}
			if res.StructuredContent == nil {
				t.Errorf("CallTool(%s): no structured content", c.tool)
			}
		})
	}
}

// TestReadToolsRegistered confirms every M1 tool is advertised to clients.
func TestReadToolsRegistered(t *testing.T) {
	ctx := context.Background()
	srv := mockMidpoint(t)
	client := midpoint.NewClient(midpoint.Config{BaseURL: srv.URL, Username: "u", Password: "p"})

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	registerReadTools(server, client)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	mc := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := mc.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	want := map[string]bool{
		"search_users": false, "get_user": false, "get_user_assignments": false,
		"list_roles": false, "get_role": false, "list_resources": false, "get_resource": false,
	}
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("listing tools: %v", err)
		}
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %q not registered", name)
		}
	}
}
