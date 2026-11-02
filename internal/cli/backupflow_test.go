package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

type kidKeys struct{ fake.KeyService }

func (kidKeys) NewSnapshotKey(context.Context, string) ([]byte, []byte, string, error) {
	return []byte("0123456789abcdef0123456789abcdef"), []byte("wrapped"), "k1", nil
}

// ingest 後の backup の流れ(spec 7.5 の 2〜5): マニフェストを保管先に書き、snapshots[] に足し、索引を確定する。
func TestBackupIngestWritesManifestAndRecordsSnapshot(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	if err := f.d.State.SwitchDef(bg, contract.SwitchInput{Normalized: mustNorm(t, f, applyDef)}); err != nil {
		t.Fatal(err)
	}
	held, _ := f.d.State.Lock(bg)
	_ = f.d.State.Update(bg, held, "repo_id", json.RawMessage(`"r-`+strings.Repeat("a", 32)+`"`))
	held.Release()
	st, _, core := fake.NewTestStore(fake.RoleWrite)
	_ = core
	var committed string
	mid := "m-" + strings.Repeat("b", 32)
	f.d.Object, f.d.Codec, f.d.KeyS = st, &harness.RefCodec{}, kidKeys{}
	f.d.Index = &fake.Index{M: map[string]contract.ChunkRef{}}
	f.d.CommitIndex = func(_ context.Context, id string, _ []string) error { committed = id; return nil }
	f.d.Store, f.d.Retention = &fake.Retention{}, contract.FixedRetention{}
	f.d.Ingest = func(context.Context, contract.Call) (IngestOut, error) {
		return IngestOut{Material: ManifestMaterial{ManifestID: mid, Targets: []TargetMaterial{{ID: "t1", Kind: "files", Status: "ok", Files: 2, Bytes: 9}}},
			Outcomes: []IngestTarget{{ID: "t1"}}}, nil
	}
	rep, err := f.d.backupIngest(bg, contract.Call{Words: []string{"backup"}, RunID: runA})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rep.Result), `"snapshot_ref":"`+mid+`"`) || !strings.Contains(string(rep.Result), `"health":"pass"`) {
		t.Fatalf("結果: %s", rep.Result)
	}
	if committed != mid {
		t.Errorf("索引を確定していない: %q", committed)
	}
	snaps, _ := f.d.loadSnapshots(bg)
	if len(snaps) != 1 || snaps[0].Ref != mid || snaps[0].Health != "pass" || !snaps[0].TargetsOK {
		t.Errorf("snapshots[]: %+v", snaps)
	}
	if raw, _ := f.d.State.Get(bg, "protected"); !strings.Contains(string(raw), "latest_healthy") {
		t.Errorf("protected: %s", raw)
	}
	if vs, _ := st.ListVersions(bg, "manifests/"); len(vs) != 1 {
		t.Errorf("manifests/ の版: %d", len(vs))
	}
}

// 全対象が failed ならマニフェストを書かず ALL_TARGETS_FAILED。
func TestBackupIngestAllFailedWritesNothing(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	st, _, _ := fake.NewTestStore(fake.RoleWrite)
	f.d.Object, f.d.Codec, f.d.KeyS, f.d.Index, f.d.Store = st, &harness.RefCodec{}, kidKeys{}, &fake.Index{}, &fake.Retention{}
	f.d.CommitIndex = func(context.Context, string, []string) error { t.Fatal("確定した"); return nil }
	f.d.Ingest = func(context.Context, contract.Call) (IngestOut, error) {
		return IngestOut{Material: ManifestMaterial{ManifestID: "m-" + strings.Repeat("c", 32), Targets: []TargetMaterial{{ID: "t1", Kind: "files", Status: "failed"}}},
			Outcomes: []IngestTarget{{ID: "t1", ErrorCode: "STORAGE_UNREACHABLE"}}}, nil
	}
	_, err := f.d.backupIngest(bg, contract.Call{Words: []string{"backup"}, RunID: runA})
	if e := toError(err); e == nil || e.Code != contract.CodeAllTargetsFailed {
		t.Fatalf("%v", err)
	}
	if vs, _ := st.ListVersions(bg, "manifests/"); len(vs) != 0 {
		t.Errorf("manifests/ に書いた: %d", len(vs))
	}
}

// 索引を開けないときは再構成して 1 度だけやり直す。
func TestBackupIngestRebuildsUnreadableIndexOnce(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	calls := 0
	f.d.Ingest = func(context.Context, contract.Call) (IngestOut, error) {
		calls++
		return IngestOut{}, ErrIndexUnreadable()
	}
	f.d.Object, f.d.Codec, f.d.KeyS, f.d.Index = nil, nil, nil, nil
	_, err := f.d.ingest(bg, contract.Call{})
	if err == nil || calls != 1 {
		t.Fatalf("部品が無いときは再構成で止まる: calls=%d err=%v", calls, err)
	}
}
