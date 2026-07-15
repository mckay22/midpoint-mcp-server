package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mckay22/midpoint-mcp-server/internal/midpoint"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func mockMidpointReporting(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ws/rest/orgs/search", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":[{"oid":"org-1","name":"IT","displayName":"IT Department"}]}`)
	})
	mux.HandleFunc("POST /ws/rest/rpc/executeScript", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// The audit script returns each record as a tab-delimited xsd:string
		// data-output item (not console output).
		rec := "2026-07-01T10:00:00Z\tmodifyObject\texecution\tsuccess\tc\tadministrator\tJane\tmodified"
		_, _ = io.WriteString(w, `{"object":{"output":{"consoleOutput":"","dataOutput":{"item":[{"value":{"@type":"xsd:string","@value":`+jsonString(rec)+`}}]}},"result":{"status":"success"}}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// jsonString quotes s as a JSON string literal.
func jsonString(s string) string {
	out := `"`
	for _, r := range s {
		switch r {
		case '"':
			out += `\"`
		case '\\':
			out += `\\`
		case '\t':
			out += `\t`
		case '\n':
			out += `\n`
		default:
			out += string(r)
		}
	}
	return out + `"`
}

func connectReporting(t *testing.T, srv *httptest.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	client := midpoint.NewClient(midpoint.Config{BaseURL: srv.URL, Username: "u", Password: "p"})
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	registerAuditTools(server, client)

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

func TestReportingToolsRoundTrip(t *testing.T) {
	cs := connectReporting(t, mockMidpointReporting(t))

	objs := callTool(t, cs, "search_objects", map[string]any{"type": "orgs", "filter": `name = "IT"`})
	if objs["count"].(float64) != 1 {
		t.Errorf("search_objects count = %v, want 1", objs["count"])
	}

	audit := callTool(t, cs, "search_audit", map[string]any{"eventType": "modify"})
	if audit["count"].(float64) != 1 {
		t.Errorf("search_audit count = %v, want 1", audit["count"])
	}
}
