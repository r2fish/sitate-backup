package contract

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// EventTypes は event.json の type・severity と一致する。
func TestEventTypesMatchSchema(t *testing.T) {
	b, err := os.ReadFile("schema/data/event.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Properties struct {
				Type     struct{ Const string }
				Severity struct {
					Const string
					Enum  []string
				}
			}
		} `json:"$defs"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range EventTypes {
		d, ok := doc.Defs["event_"+strings.ReplaceAll(e.Type, ".", "_")]
		if !ok || d.Properties.Type.Const != e.Type {
			t.Errorf("%s: スキーマに無い", e.Type)
			continue
		}
		got := d.Properties.Severity.Enum
		if d.Properties.Severity.Const != "" {
			got = []string{d.Properties.Severity.Const}
		}
		if strings.Join(got, ",") != strings.Join(e.Severities, ",") {
			t.Errorf("%s: severity schema=%v go=%v", e.Type, got, e.Severities)
		}
		if e.Resend != (strings.Contains(strings.Join(e.Severities, ","), "critical") || strings.HasPrefix(e.Type, "change.") || strings.HasPrefix(e.Type, "restore_session.")) {
			t.Errorf("%s: 再送の列が規則と合わない", e.Type)
		}
		seen[e.Type] = true
	}
	var extra []string
	for k := range doc.Defs {
		if strings.HasPrefix(k, "event_") && !seen[d2t(k)] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Errorf("Go に無い種類: %v", extra)
	}
	if len(EventTypes) != 41 {
		t.Errorf("種類の数 %d", len(EventTypes))
	}
}

func d2t(def string) string {
	s := strings.TrimPrefix(def, "event_")
	for _, e := range EventTypes {
		if strings.ReplaceAll(e.Type, ".", "_") == s {
			return e.Type
		}
	}
	return s
}
