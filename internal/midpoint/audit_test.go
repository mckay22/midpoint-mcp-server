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

// executeScriptResponse builds a realistic ExecuteScriptResponseType envelope
// whose data-output items are xsd:string values (how the audit script returns
// its tab-delimited records).
func executeScriptResponse(console string, itemValues ...string) []byte {
	items := make([]any, 0, len(itemValues))
	for _, v := range itemValues {
		items = append(items, map[string]any{
			"value":  map[string]any{"@type": "xsd:string", "@value": v},
			"result": map[string]any{"status": "success"},
		})
	}
	b, _ := json.Marshal(map[string]any{
		"@ns": "http://prism.evolveum.com/xml/ns/public/types-3",
		"object": map[string]any{
			"@type": "http://midpoint.evolveum.com/xml/ns/public/common/api-types-3#ExecuteScriptResponseType",
			"output": map[string]any{
				"consoleOutput": console,
				"dataOutput":    map[string]any{"item": items},
			},
			"result": map[string]any{"operation": "executeScript", "status": "success"},
		},
	})
	return b
}

func newScriptClient(t *testing.T, console string, itemValues ...string) (*Client, *string) {
	t.Helper()
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(executeScriptResponse(console, itemValues...))
	}))
	t.Cleanup(srv.Close)
	return NewClient(Config{BaseURL: srv.URL, Username: "u", Password: "p"}), &gotBody
}

// auditItem joins fields the way the script does: one tab-delimited record.
func auditItem(fields ...string) string { return strings.Join(fields, "\t") }

func TestExecuteScriptParsesResponse(t *testing.T) {
	c, _ := newScriptClient(t, "hello console\n", "a", "b")
	out, err := c.ExecuteScript(context.Background(), map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("ExecuteScript: %v", err)
	}
	if out.Status != "success" || out.ConsoleOutput != "hello console\n" {
		t.Errorf("out = %+v", out)
	}
	if len(out.Items) != 2 {
		t.Errorf("items = %d, want 2", len(out.Items))
	}
}

func TestParseAuditItems(t *testing.T) {
	c, _ := newScriptClient(t, "",
		auditItem("2026-07-01T10:00:00Z", "modifyObject", "execution", "success", "chan#user", "administrator", "Jane Doe", "modified user"),
		auditItem("2026-07-01T09:00:00Z", "addObject", "execution", "fatalError", "chan#init", "system", "Bob", "add failed"),
	)
	out, err := c.ExecuteScript(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("ExecuteScript: %v", err)
	}
	recs := parseAuditItems(out.Items)
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	r0 := recs[0]
	if r0.Timestamp != "2026-07-01T10:00:00Z" || r0.EventType != "modifyObject" || r0.EventStage != "execution" ||
		r0.Outcome != "success" || r0.Channel != "chan#user" || r0.Initiator != "administrator" ||
		r0.Target != "Jane Doe" || r0.Message != "modified user" {
		t.Errorf("record 0 = %+v", r0)
	}
}

func TestBuildAuditGroovy(t *testing.T) {
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	g := buildAuditGroovy(AuditQuery{From: from, To: to, Limit: 15})
	for _, want := range []string{
		"modelAuditService", "modelInteractionService",
		"auditService.searchObjects(", "DatatypeFactory",
		".item(AuditEventRecordType.F_TIMESTAMP).ge(cal('2026-06-01T00:00:00Z'))",
		".and()", ".le(cal('2026-06-02T00:00:00Z'))",
		".desc(AuditEventRecordType.F_TIMESTAMP)", "maxSize(15)",
	} {
		if !strings.Contains(g, want) {
			t.Errorf("groovy missing %q:\n%s", want, g)
		}
	}
	// No time bounds → no timestamp filter, but still ordered + capped.
	all := buildAuditGroovy(AuditQuery{})
	if strings.Contains(all, "F_TIMESTAMP).ge(") || strings.Contains(all, "F_TIMESTAMP).le(") {
		t.Errorf("expected no timestamp filter with no bounds:\n%s", all)
	}
	if !strings.Contains(all, "queryFor(AuditEventRecordType.class).desc(") {
		t.Errorf("expected desc ordering directly after queryFor:\n%s", all)
	}
}

func TestAuditScriptBodyShape(t *testing.T) {
	b, err := json.Marshal(auditScriptBody("CODE"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	// 4.10 requires a typed <search> seed and the execute-script action name.
	for _, want := range []string{
		`"@element":"search"`, `"type":"SystemConfigurationType"`,
		`"type":"execute-script"`, `"code":"CODE"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("body missing %q:\n%s", want, s)
		}
	}
	// The generic dynamic search form is rejected by 4.10; make sure it's gone.
	if strings.Contains(s, `"type":"execute"`) && !strings.Contains(s, `"type":"execute-script"`) {
		t.Errorf("must use execute-script, not the generic execute action:\n%s", s)
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
	c, gotBody := newScriptClient(t, "",
		auditItem("2026-07-01T10:00:00Z", "modifyObject", "execution", "success", "c", "administrator", "Jane", "m1"),
		auditItem("2026-07-01T09:00:00Z", "addObject", "execution", "success", "c", "system", "Bob", "m2"),
	)

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
