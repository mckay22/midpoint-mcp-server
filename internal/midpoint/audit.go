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

// AuditResult is what SearchAudit returns: the parsed records plus the script's
// overall status.
type AuditResult struct {
	Records []AuditRecord `json:"records"`
	Status  string        `json:"status,omitempty"`
}

// SearchAudit queries the audit trail.
//
// midPoint 4.10 exposes no REST audit endpoint, so this runs a Groovy script via
// executeScript that reaches the ModelAuditService and returns delimited records
// as script data output. Because it uses the execute-script action, it requires
// script-execution authorization and therefore does NOT work under
// resource-server (#proxy) impersonation, where the mapped end user lacks that
// privilege. The script reflects a non-public field (modelAuditService) because
// the scripting binding exposes no audit accessor; that internal path is the only
// one available in 4.10 and may need revisiting on a future midPoint.
func (c *Client) SearchAudit(ctx context.Context, q AuditQuery) (AuditResult, error) {
	out, err := c.ExecuteScript(ctx, auditScriptBody(buildAuditGroovy(q)))
	if err != nil {
		return AuditResult{}, err
	}
	records := refineAudit(parseAuditItems(out.Items), q)
	return AuditResult{Records: records, Status: out.Status}, nil
}

// auditScriptBody wraps a Groovy body in a two-step pipeline: a typed search that
// yields the single SystemConfigurationType object (so the following action has
// exactly one input item and runs once), then an execute-script action.
//
// midPoint 4.10 rejects the generic dynamic "search"/"execute" actions
// ("cannot be invoked dynamically" / "unknown action executor"); the search must
// be the typed <search> element and the script action must be "execute-script".
func auditScriptBody(groovy string) map[string]any {
	return map[string]any{
		"@ns": scriptingNS,
		"executeScript": map[string]any{
			"pipeline": []any{
				map[string]any{"@element": "search", "type": "SystemConfigurationType"},
				map[string]any{
					"@element":  "action",
					"type":      "execute-script",
					"parameter": []any{map[string]any{"name": "script", "value": map[string]any{"code": groovy}}},
				},
			},
		},
	}
}

// buildAuditGroovy renders the audit-search script. Timestamp bounds are applied
// server-side; the remaining filters are applied by refineAudit on the results.
// Each record is returned as one tab-delimited string (tabs/newlines in values
// are flattened to spaces first), which midPoint surfaces as a data-output item.
func buildAuditGroovy(q AuditQuery) string {
	var filter strings.Builder
	if !q.From.IsZero() {
		fmt.Fprintf(&filter, ".item(AuditEventRecordType.F_TIMESTAMP).ge(cal('%s'))", q.From.UTC().Format(time.RFC3339))
	}
	if !q.To.IsZero() {
		if filter.Len() > 0 {
			filter.WriteString(".and()")
		}
		fmt.Fprintf(&filter, ".item(AuditEventRecordType.F_TIMESTAMP).le(cal('%s'))", q.To.UTC().Format(time.RFC3339))
	}

	return fmt.Sprintf(`import com.evolveum.midpoint.xml.ns._public.common.audit_3.AuditEventRecordType
import javax.xml.datatype.DatatypeFactory
def getField
getField = { obj, name ->
    def k = obj.getClass()
    while (k != null) {
        try { def f = k.getDeclaredField(name); f.setAccessible(true); return f.get(obj) } catch (Throwable t) {}
        k = k.superclass
    }
    return null
}
def auditService = getField(getField(midpoint, 'modelInteractionService'), 'modelAuditService')
def cal = { s -> DatatypeFactory.newInstance().newXMLGregorianCalendar(s) }
def query = prismContext.queryFor(AuditEventRecordType.class)%s.desc(AuditEventRecordType.F_TIMESTAMP).maxSize(%d).build()
def records = auditService.searchObjects(query, null, midpoint.getCurrentTask(), midpoint.getCurrentResult())
return records.collect { r ->
    [r.timestamp, r.eventType, r.eventStage, r.outcome, r.channel,
     (r.initiatorRef ? (r.initiatorRef.targetName ?: r.initiatorRef.oid) : ''),
     (r.targetRef ? (r.targetRef.targetName ?: r.targetRef.oid) : ''),
     (r.message ?: '')].collect { it == null ? '' : it.toString().replace('\t', ' ').replace('\n', ' ') }.join('\t')
}
`, filter.String(), clampLimit(q.Limit))
}

// parseAuditItems decodes the tab-delimited record strings the audit script
// returns as script data-output items. Each item's value is an xsd:string,
// serialized by midPoint as {"@type":"xsd:string","@value":"f0\tf1\t..."}.
func parseAuditItems(items []json.RawMessage) []AuditRecord {
	var recs []AuditRecord
	for _, raw := range items {
		var v struct {
			Value string `json:"@value"`
		}
		if err := json.Unmarshal(raw, &v); err != nil || v.Value == "" {
			continue
		}
		f := strings.Split(v.Value, "\t")
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
