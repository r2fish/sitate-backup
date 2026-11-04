package cli

// backup の targets[] の failed の要素は、その原因コードの detail・next を持つ(spec 3.1.2。D-0172)。
// 全対象が failed の run_end にも載る(結果が null になるため)。

import (
	"encoding/json"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func failedIngest() IngestOut {
	why := contract.CodeInternal
	return IngestOut{
		Material: ManifestMaterial{Targets: []TargetMaterial{{ID: "t1", Kind: "files", Status: "ok"}, {ID: "t2", Kind: "postgresql", Status: "failed"}}},
		Outcomes: []IngestTarget{{ID: "t1"}, {ID: "t2", ErrorCode: "INTERNAL",
			Detail: json.RawMessage(`{"component":"adapter","http_status":null,"kind":"acquire","name":"postgresql","reason":"request"}`),
			Next:   []contract.Next{{Argv: []string{"sitate", "status"}, Why: &why}}}},
	}
}

func TestResultTargetsCarryDetailAndNext(t *testing.T) {
	ts := resultTargets(failedIngest())
	var jes []map[string]any
	for _, x := range ts {
		je, _ := normalizeTarget(x.(map[string]any)) // 結果へ出す形(errors[]・warnings[] を外す)にし、run_end の元を得る
		jes = append(jes, je)
	}
	b, _ := json.Marshal(map[string]any{"snapshot_ref": "m-" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "targets": ts, "checks": []any{}, "health": "incomplete",
		"uploaded_bytes": 0, "duration_s": 0, "chunk_keys_missing": 0})
	var r struct {
		Targets []struct {
			Detail map[string]any   `json:"detail"`
			Next   []map[string]any `json:"next"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(b, &r); err != nil || len(r.Targets) != 2 {
		t.Fatalf("%v %s", err, b)
	}
	if r.Targets[0].Detail != nil || len(r.Targets[0].Next) != 0 {
		t.Errorf("ok の対象は detail が null・next が空: %s", b)
	}
	if r.Targets[1].Detail["component"] != "adapter" || r.Targets[1].Detail["reason"] != "request" || len(r.Targets[1].Next) != 1 {
		t.Errorf("failed の対象に detail・next が載らない: %s", b)
	}
	// 結果のスキーマ(backup_target)に合い、run_end の元(normalizeTarget)にも残る。
	if err := contract.Validate("cli/results.json#/$defs/backup_done", b); err != nil {
		t.Errorf("結果のスキーマに合わない: %v", err)
	}
	if je := jes[1]; je["detail"] == nil || je["next"] == nil {
		t.Errorf("run_end の targets[] に detail・next が無い: %+v", je)
	}
}
