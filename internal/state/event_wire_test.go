package state

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// M8: Webhook・アダプタへ渡すイベントは schema: sitate.event/v1 を持つ。
func TestEventJSONHasSchema(t *testing.T) {
	b := eventJSON(contract.Event{ID: "e-20261018T170000Z-aaaa-01", Type: "backup.failed", Severity: "critical", Time: time.Unix(0, 0)})
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil || m["schema"] != "sitate.event/v1" {
		t.Fatalf("%s %v", b, err)
	}
}

func TestSetRunIDMakesEventIDs(t *testing.T) {
	r := &Recorder{}
	r.SetRunID("r-20261018T170000Z-aaaa")
	if id := r.newEventID(time.Unix(0, 0)); id != "e-20261018T170000Z-aaaa-01" {
		t.Errorf("id=%s", id)
	}
	if id := r.newEventID(time.Unix(0, 0)); id != "e-20261018T170000Z-aaaa-02" {
		t.Errorf("id=%s", id)
	}
}

// L3: spec 4.8 の表で next が 2 件の種類は 2 件を出す。
func TestNextForMulti(t *testing.T) {
	n := nextFor(contract.EventType{Type: "hold.set"})
	if len(n) != 2 || n[1].Argv[2] != "hold-release" {
		t.Errorf("hold.set: %+v", n)
	}
	if n := nextFor(contract.EventType{Type: "verify.failed"}); len(n) != 2 {
		t.Errorf("verify.failed: %+v", n)
	}
	if n := nextFor(contract.EventType{Type: "backup.failed"}); len(n) != 1 {
		t.Errorf("backup.failed: %+v", n)
	}
}
