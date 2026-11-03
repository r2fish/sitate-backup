package cli

// D-0166: エントリが 0 件の対象(IngestTarget.Empty)は警告 TARGET_EMPTY(detail.target)。世代は incomplete で、
// latest_healthy(保護中の世代)と latest は前の世代のまま。backup.partial の警告に TARGET_EMPTY が載る。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func TestBackupEmptyTargetKeepsLatestHealthy(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	if err := f.d.State.SwitchDef(bg, contract.SwitchInput{Normalized: mustNorm(t, f, applyDef)}); err != nil {
		t.Fatal(err)
	}
	held, _ := f.d.State.Lock(bg)
	_ = f.d.State.Update(bg, held, "repo_id", json.RawMessage(`"r-`+strings.Repeat("a", 32)+`"`))
	held.Release()
	st, _, _ := fake.NewTestStore(fake.RoleWrite)
	f.d.Object, f.d.Codec, f.d.KeyS = st, &harness.RefCodec{}, kidKeys{}
	f.d.Index = &fake.Index{M: map[string]contract.ChunkRef{}}
	f.d.CommitIndex = func(context.Context, string, []string) error { return nil }
	f.d.Store, f.d.Retention = &fake.Retention{}, contract.FixedRetention{}
	good, empty := "m-"+strings.Repeat("b", 32), "m-"+strings.Repeat("c", 32)
	next := IngestOut{Material: ManifestMaterial{ManifestID: good, Targets: []TargetMaterial{{ID: "t1", Kind: "files", Status: "ok", Files: 2, Bytes: 9}}}, Outcomes: []IngestTarget{{ID: "t1"}}}
	f.d.Ingest = func(context.Context, contract.Call) (IngestOut, error) { return next, nil }
	if _, err := f.d.backupIngest(bg, contract.Call{Words: []string{"backup"}, RunID: runA}); err != nil {
		t.Fatal(err)
	}
	next = IngestOut{Material: ManifestMaterial{ManifestID: empty, Targets: []TargetMaterial{{ID: "t1", Kind: "files", Status: "partial"}}}, Outcomes: []IngestTarget{{ID: "t1", Empty: true}}}
	rep, err := f.d.backupIngest(bg, contract.Call{Words: []string{"backup"}, RunID: "r-20260102T030405Z-0002"})
	if err != nil {
		t.Fatal(err)
	}
	var w *contract.Warning
	for i := range rep.Warnings {
		if rep.Warnings[i].Code == contract.CodeTargetEmpty {
			w = &rep.Warnings[i]
		}
	}
	if w == nil || !strings.Contains(string(w.Detail), `"target":"t1"`) {
		t.Fatalf("TARGET_EMPTY の警告が無い: %+v", rep.Warnings)
	}
	if !strings.Contains(string(rep.Result), `"health":"incomplete"`) {
		t.Errorf("health が incomplete でない: %s", rep.Result)
	}
	if raw, _ := f.d.State.Get(bg, "protected"); !strings.Contains(string(raw), good) || strings.Contains(string(raw), empty) {
		t.Errorf("保護中の latest_healthy が前の世代のままでない: %s", raw)
	}
	snaps, _ := f.d.loadSnapshots(bg)
	if len(snaps) != 2 || snaps[1].Health != "incomplete" || snaps[1].TargetsOK {
		t.Errorf("snapshots[]: %+v", snaps)
	}
	var sawPartial bool
	for _, n := range f.rec.Notifications() {
		if n["type"] == "backup.partial" {
			sawPartial = true
		}
	}
	if !sawPartial {
		t.Error("backup.partial を通知していない")
	}
}
