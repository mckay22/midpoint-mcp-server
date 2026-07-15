package midpoint

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// executeScriptResponse builds a realistic ExecuteScriptResponseType envelope.
func executeScriptResponse(console string) []byte {
	b, _ := json.Marshal(map[string]any{
		"@ns": "http://prism.evolveum.com/xml/ns/public/types-3",
		"object": map[string]any{
			"@type": "http://midpoint.evolveum.com/xml/ns/public/common/api-types-3#ExecuteScriptResponseType",
			"output": map[string]any{
				"consoleOutput": console,
				"dataOutput": map[string]any{
					"item": []any{map[string]any{"value": map[string]any{"oid": "sysconfig"}, "result": map[string]any{"status": "success"}}},
				},
			},
			"result": map[string]any{"operation": "executeScript", "status": "success"},
		},
	})
	return b
}

func newScriptClient(t *testing.T, console string) (*Client, *string) {
	t.Helper()
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(executeScriptResponse(console))
	}))
	t.Cleanup(srv.Close)
	return NewClient(Config{BaseURL: srv.URL, Username: "u", Password: "p"}), &gotBody
}

func auditLine(fields ...string) string { return auditLinePrefix + strings.Join(fields, "\t") }

func TestExecuteScriptParsesResponse(t *testing.T) {
	c, _ := newScriptClient(t, "hello console\n")
	out, err := c.ExecuteScript(context.Background(), map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("ExecuteScript: %v", err)
	}
	if out.Status != "success" || out.ConsoleOutput != "hello console\n" {
		t.Errorf("out = %+v", out)
	}
	if len(out.Items) != 1 {
		t.Errorf("items = %d, want 1", len(out.Items))
	}
}

func TestParseAuditConsole(t *testing.T) {
	console := "pipeline noise line\n" +
		auditLine("2026-07-01T10:00:00Z", "modifyObject", "execution", "success", "chan#user", "administrator", "Jane Doe", "modified user") + "\n" +
		auditLine("2026-07-01T09:00:00Z", "addObject", "execution", "fatalError", "chan#init", "system", "Bob", "add failed") + "\n" +
		"trailing noise"

	recs := parseAuditConsole(console)
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	r0 := recs[0]
	if r0.EventType != "modifyObject" || r0.Outcome != "success" || r0.Initiator != "administrator" ||
		r0.Target != "Jane Doe" || r0.Message != "modified user" {
		t.Errorf("record 0 = %+v", r0)
	}
}

func TestBuildAuditGroovy(t *testing.T) {
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	g := buildAuditGroovy(AuditQuery{From: from, Limit: 15})
	for _, want := range []string{"AuditEventRecordType.F_TIMESTAMP).ge(", "2026-06-01T00:00:00Z", "maxSize(15)", "searchContainers", auditLinePrefix} {
		if !strings.Contains(g, want) {
			t.Errorf("groovy missing %q:\n%s", want, g)
		}
	}
	// No time bounds → match all.
	if all := buildAuditGroovy(AuditQuery{}); !strings.Contains(all, ".all()") {
		t.Errorf("expected .all() with no bounds:\n%s", all)
	}
}

func TestRefineAudit(t *testing.T) {
	recs := []AuditRecord{
		{EventType: "modifyObject", Outcome: "success", Initiator: "administrator"},
		{EventType: "addObject", Outcome: "fatalError", Initiator: "system"},
	}
	if got := refineAudit(recs, AuditQuery{EventType: "modify"}); len(got) != 1 || got[0].EventType != "modifyObject" {
		t.Errorf("eventType filter = %+v", got)
	}
	if got := refineAudit(recs, AuditQuery{Outcome: "fatal"}); len(got) != 1 || got[0].EventType != "addObject" {
		t.Errorf("outcome filter = %+v", got)
	}
	if got := refineAudit(recs, AuditQuery{Initiator: "ADMIN"}); len(got) != 1 {
		t.Errorf("initiator filter (case-insensitive) = %+v", got)
	}
	if got := refineAudit(recs, AuditQuery{}); len(got) != 2 {
		t.Errorf("no filter should keep all, got %d", len(got))
	}
}

func TestSearchAuditEndToEnd(t *testing.T) {
	console := auditLine("2026-07-01T10:00:00Z", "modifyObject", "execution", "success", "c", "administrator", "Jane", "m1") + "\n" +
		auditLine("2026-07-01T09:00:00Z", "addObject", "execution", "success", "c", "system", "Bob", "m2") + "\n"
	c, gotBody := newScriptClient(t, console)

	res, err := c.SearchAudit(context.Background(), AuditQuery{EventType: "modify", Limit: 50})
	if err != nil {
		t.Fatalf("SearchAudit: %v", err)
	}
	if len(res.Records) != 1 || res.Records[0].EventType != "modifyObject" {
		t.Fatalf("records = %+v", res.Records)
	}
	if res.Status != "success" {
		t.Errorf("status = %q", res.Status)
	}
	// The request must target the executeScript RPC with a scripting body.
	if !strings.Contains(*gotBody, scriptingNS) || !strings.Contains(*gotBody, "executeScript") {
		t.Errorf("executeScript body = %s", *gotBody)
	}
}
