package midpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// scriptingNS is midPoint's bulk-action / scripting namespace.
const scriptingNS = "http://midpoint.evolveum.com/xml/ns/public/model/scripting-3"

// wellKnownSystemConfigOID always exists; it seeds the pipeline so the execute
// action runs exactly once.
const wellKnownSystemConfigOID = "00000000-0000-0000-0000-000000000001"

// ScriptOutput is the parsed result of an executeScript call.
type ScriptOutput struct {
	ConsoleOutput string
	Items         []json.RawMessage
	Status        string
}

// ExecuteScript runs a bulk-action / scripting request via
// POST /ws/rest/rpc/executeScript and parses the ExecuteScriptResponseType.
// It requires the REST user to hold script-execution authorization.
func (c *Client) ExecuteScript(ctx context.Context, body any) (ScriptOutput, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return ScriptOutput{}, err
	}
	resp, err := c.post(ctx, "/rpc/executeScript", nil, b)
	if err != nil {
		return ScriptOutput{}, err
	}

	var env struct {
		Object struct {
			Output struct {
				ConsoleOutput string `json:"consoleOutput"`
				DataOutput    struct {
					Item flexSlice `json:"item"`
				} `json:"dataOutput"`
			} `json:"output"`
			Result struct {
				Status string `json:"status"`
			} `json:"result"`
		} `json:"object"`
	}
	if err := json.Unmarshal(resp, &env); err != nil {
		return ScriptOutput{}, fmt.Errorf("decoding executeScript response: %w", err)
	}

	out := ScriptOutput{
		ConsoleOutput: env.Object.Output.ConsoleOutput,
		Status:        env.Object.Result.Status,
	}
	for _, raw := range env.Object.Output.DataOutput.Item {
		var item struct {
			Value json.RawMessage `json:"value"`
		}
		if json.Unmarshal(raw, &item) == nil && item.Value != nil {
			out.Items = append(out.Items, item.Value)
		}
	}
	return out, nil
}

// AuditQuery parameterizes SearchAudit. From/To bound the timestamp (server-side,
// recommended for performance); the remaining fields refine results.
type AuditQuery struct {
	From      time.Time
	To        time.Time
	EventType string
	Outcome   string
	Initiator string
	Target    string
	Channel   string
	Limit     int
}

// AuditRecord is one decoded audit-trail entry.
type AuditRecord struct {
	Timestamp  string `json:"timestamp"`
	EventType  string `json:"eventType"`
	EventStage string `json:"eventStage,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	Channel    string `json:"channel,omitempty"`
	Initiator  string `json:"initiator,omitempty"`
	Target     string `json:"target,omitempty"`
	Message    string `json:"message,omitempty"`
}

// AuditResult is what SearchAudit returns: the parsed records plus the raw
// script status/console, which callers surface for diagnosis since this path is
// experimental (see SearchAudit).
type AuditResult struct {
	Records []AuditRecord `json:"records"`
	Status  string        `json:"status,omitempty"`
	Console string        `json:"console,omitempty"`
}

// SearchAudit queries the audit trail.
//
// EXPERIMENTAL: midPoint 4.10 exposes no REST audit endpoint, so this runs a
// Groovy script via executeScript that searches audit containers and prints
// delimited records. It requires script-execution authorization and therefore
// does NOT work under resource-server (#proxy) impersonation. The embedded
// script and pipeline are a best-effort against the documented scripting API and
// may need tuning for a specific midPoint version; the raw console output is
// returned to aid that.
func (c *Client) SearchAudit(ctx context.Context, q AuditQuery) (AuditResult, error) {
	out, err := c.ExecuteScript(ctx, auditScriptBody(buildAuditGroovy(q)))
	if err != nil {
		return AuditResult{}, err
	}
	records := refineAudit(parseAuditConsole(out.ConsoleOutput), q)
	return AuditResult{Records: records, Status: out.Status, Console: out.ConsoleOutput}, nil
}

// auditScriptBody wraps a Groovy body in a pipeline seeded by the system
// configuration object (so execute runs once).
func auditScriptBody(groovy string) map[string]any {
	return map[string]any{
		"@ns": scriptingNS,
		"executeScript": map[string]any{
			"pipeline": []any{
				map[string]any{
					"@element":  "action",
					"type":      "search",
					"parameter": []any{map[string]any{"name": "type", "value": "SystemConfigurationType"}},
				},
				map[string]any{
					"@element":  "action",
					"type":      "execute",
					"parameter": []any{map[string]any{"name": "script", "value": map[string]any{"code": groovy}}},
				},
			},
			"options": map[string]any{"continueOnAnyError": "true"},
		},
	}
}

// auditLinePrefix marks a record line in the script console output.
const auditLinePrefix = "AUDITREC\t"

// buildAuditGroovy renders the audit-search script. Timestamp bounds are applied
// server-side; other filters are applied by refineAudit on the results.
func buildAuditGroovy(q AuditQuery) string {
	var filter strings.Builder
	if !q.From.IsZero() {
		fmt.Fprintf(&filter, ".item(AuditEventRecordType.F_TIMESTAMP).ge(XmlTypeConverter.createXMLGregorianCalendar('%s'))", q.From.UTC().Format(time.RFC3339))
	}
	if !q.To.IsZero() {
		if !q.From.IsZero() {
			filter.WriteString(".and()")
		}
		fmt.Fprintf(&filter, ".item(AuditEventRecordType.F_TIMESTAMP).le(XmlTypeConverter.createXMLGregorianCalendar('%s'))", q.To.UTC().Format(time.RFC3339))
	}
	if filter.Len() == 0 {
		filter.WriteString(".all()")
	}

	// The record line joins fields with tabs; sanitize tabs/newlines in values.
	return fmt.Sprintf(`import com.evolveum.midpoint.xml.ns._public.common.audit_3.AuditEventRecordType
import com.evolveum.midpoint.util.XmlTypeConverter
def query = prismContext.queryFor(AuditEventRecordType.class)%s.maxSize(%d).build()
def records = midpoint.searchContainers(AuditEventRecordType.class, query, null)
records.each { r ->
    def parts = [r.timestamp, r.eventType, r.eventStage, r.outcome, r.channel,
        (r.initiatorRef ? (r.initiatorRef.targetName ?: r.initiatorRef.oid) : ''),
        (r.targetRef ? (r.targetRef.targetName ?: r.targetRef.oid) : ''),
        (r.message ?: '')]
    log.info('%s' + parts.collect { it == null ? '' : it.toString().replace('\t', ' ').replace('\n', ' ') }.join('\t'))
}
`, filter.String(), clampLimit(q.Limit), auditLinePrefix)
}

// parseAuditConsole extracts record lines emitted by the audit script.
func parseAuditConsole(console string) []AuditRecord {
	var recs []AuditRecord
	for _, line := range strings.Split(console, "\n") {
		line = strings.TrimRight(line, "\r")
		rest, ok := strings.CutPrefix(line, auditLinePrefix)
		if !ok {
			continue
		}
		f := strings.Split(rest, "\t")
		at := func(i int) string {
			if i < len(f) {
				return f[i]
			}
			return ""
		}
		recs = append(recs, AuditRecord{
			Timestamp: at(0), EventType: at(1), EventStage: at(2), Outcome: at(3),
			Channel: at(4), Initiator: at(5), Target: at(6), Message: at(7),
		})
	}
	return recs
}

// refineAudit applies the non-timestamp filters client-side (case-insensitive
// substring match) since they are not part of the server-side query.
func refineAudit(records []AuditRecord, q AuditQuery) []AuditRecord {
	match := func(field, want string) bool {
		return want == "" || strings.Contains(strings.ToLower(field), strings.ToLower(want))
	}
	out := make([]AuditRecord, 0, len(records))
	for _, r := range records {
		if match(r.EventType, q.EventType) && match(r.Outcome, q.Outcome) &&
			match(r.Initiator, q.Initiator) && match(r.Target, q.Target) && match(r.Channel, q.Channel) {
			out = append(out, r)
		}
	}
	return out
}
