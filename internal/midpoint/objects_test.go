package midpoint

import (
	"encoding/json"
	"testing"
)

func TestParseObjectList(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantN   int
		wantErr bool
	}{
		{
			// The real midPoint 4.10 shape: ObjectListType wrapped under the
			// top-level "object", results under its own nested "object".
			name:  "nested list (real shape)",
			body:  `{"@ns":"x","object":{"@type":"...ObjectListType","object":[{"oid":"a"},{"oid":"b"}]}}`,
			wantN: 2,
		},
		{
			name:  "nested single object collapsed",
			body:  `{"object":{"@type":"...ObjectListType","object":{"oid":"a"}}}`,
			wantN: 1,
		},
		{
			name:  "flat array (tolerated)",
			body:  `{"object":[{"oid":"a"},{"oid":"b"},{"oid":"c"}]}`,
			wantN: 3,
		},
		{
			name:  "empty list",
			body:  `{"object":{"@type":"...ObjectListType"}}`,
			wantN: 0,
		},
		{
			name:  "no object key",
			body:  `{"@ns":"x"}`,
			wantN: 0,
		},
		{
			name:  "null object",
			body:  `{"object":null}`,
			wantN: 0,
		},
		{
			name:    "malformed",
			body:    `not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseObjectList([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseObjectList: %v", err)
			}
			if len(got) != tt.wantN {
				t.Fatalf("got %d objects, want %d", len(got), tt.wantN)
			}
			// Every extracted element must carry its real fields (the regression:
			// the wrapper used to be decoded in place, yielding empty oids).
			for i, raw := range got {
				var o struct {
					OID string `json:"oid"`
				}
				if err := json.Unmarshal(raw, &o); err != nil {
					t.Fatalf("element %d: %v", i, err)
				}
				if o.OID == "" {
					t.Errorf("element %d has empty oid: %s", i, raw)
				}
			}
		})
	}
}
