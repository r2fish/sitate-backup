package acquire

// source check の uncovered[] を U0 の試験ベクトル(testdata/survey/rules.json の check)に通す契約試験。
// 受け口の survey の応答(sitate.recv-survey/v1)・状態ファイルの source_clusters・定義の対象と source.declined をベクトルの入力から組み、
// 結果の uncovered[] がベクトルの expect(kind・path・port を並べ替えて比べる)と同じであること、actionable[] が足せる種別だけであること、
// 前回の記録(uncovered_seen)が無いので new_since_last[] が uncovered[] と同じであること、結果が source_check のスキーマに合うことを確かめる。
// 判定(JudgeCheck)は U1c の持ち物で、ここは入力と期待を通すだけ。

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

type ucItem struct {
	Kind     string  `json:"kind"`
	Path     *string `json:"path"`
	Port     *int    `json:"port"`
	TargetID *string `json:"target_id"`
}

func toUncovered(l []ucItem) []harness.Uncovered {
	r := []harness.Uncovered{}
	for _, x := range l {
		r = append(r, harness.Uncovered{Kind: x.Kind, Path: x.Path, Port: x.Port})
	}
	harness.SortUncovered(r)
	return r
}

// ruleCheckDef はベクトルの入力から定義(正規化済み)を組む。
func ruleCheckDef(in harness.CheckIn) string {
	var tg []map[string]any
	n := 0
	for _, r := range in.TargetRoots {
		n++
		tg = append(tg, map[string]any{"id": fmt.Sprintf("t%d", n), "files": r})
	}
	for _, d := range in.TargetDBs {
		n++
		m := map[string]any{"id": fmt.Sprintf("t%d", n), "postgresql": d.Name}
		if d.Port != nil {
			m["port"] = *d.Port
		}
		tg = append(tg, m)
	}
	dec := []map[string]any{}
	for i, d := range in.Declined {
		m := map[string]any{"id": fmt.Sprintf("d%d", i+1)}
		if d.Mount != nil {
			m["mount"] = *d.Mount
		}
		if d.PostgresqlCluster != nil {
			m["postgresql_cluster"] = *d.PostgresqlCluster
		}
		dec = append(dec, m)
	}
	b, _ := json.Marshal(map[string]any{"version": 1, "source": map[string]any{"host": "127.0.0.1", "targets": tg, "declined": dec}})
	return string(b)
}

// ruleSelftest は定義の対象と一致する受け口の selftest の出力(DB の対象は接続でき、読めない表は無い)。
func ruleSelftest(in harness.CheckIn) string {
	tg := []map[string]any{}
	n := 0
	for _, r := range in.TargetRoots {
		n++
		tg = append(tg, map[string]any{"id": fmt.Sprintf("t%d", n), "kind": "files", "root": r, "root_state": "ok"})
	}
	for _, d := range in.TargetDBs {
		n++
		port := 5432
		if d.Port != nil {
			port = *d.Port
		} else if len(in.SourceClusters) > 0 {
			port = in.SourceClusters[0].Port
		}
		tg = append(tg, map[string]any{"id": fmt.Sprintf("t%d", n), "kind": "postgresql", "db": d.Name, "socket_dir": "/run/postgresql", "port": port, "connect": true,
			"connect_error": nil, "server_version_num": 160000, "pg_dump_version_num": 160000, "unreadable_tables": 0, "large_objects": false})
	}
	b, _ := json.Marshal(map[string]any{"format": "sitate.recv-selftest/v1", "version": "0.1.0", "receiver_hash": checkHash,
		"authorized_keys": map[string]any{"lines": 1, "match": true}, "targets": tg})
	return string(b)
}

// runRuleCheck はベクトルの入力から元サーバーと状態を組んで source check を実行し、封筒とスキーマを確かめて返す。
func runRuleCheck(t *testing.T, in harness.CheckIn) contract.Reply {
	t.Helper()
	v := struct{ In harness.CheckIn }{in}
	recv := map[string]any{"format": "sitate.recv-survey/v1", "mounts": v.In.Mounts, "clusters": v.In.Clusters}
	if recv["mounts"] == nil {
		recv["mounts"] = []harness.RecvMount{}
	}
	if recv["clusters"] == nil {
		recv["clusters"] = []harness.RecvCluster{}
	}
	rb, _ := json.Marshal(recv)
	if err := contract.Validate("receiver/survey.json#/$defs/recv_survey", rb); err != nil {
		t.Fatalf("ベクトルの受け口の survey が recv-survey のスキーマに合わない: %v", err)
	}
	self := ruleSelftest(v.In)
	src := startSource22(t, func(r execRec) (string, int) {
		switch {
		case strings.Contains(r.Cmd, "selftest"):
			return self, 0
		case strings.Contains(r.Cmd, "survey"):
			return string(rb), 0
		}
		return "{}", 0
	})
	sc, _ := json.Marshal(map[string]any{"host": "127.0.0.1", "clusters": v.In.SourceClusters})
	if v.In.SourceClusters == nil {
		sc, _ = json.Marshal(map[string]any{"host": "127.0.0.1", "clusters": []any{}})
	}
	def := ruleCheckDef(v.In)
	items := map[string]json.RawMessage{"sealed": sealedItem(true), "host_keys": hostKeysItem("127.0.0.1", src.FP),
		"receiver_hash": json.RawMessage(`"` + checkHash + `"`), "source_clusters": sc}
	a, _ := newSrcAcquire(items)
	a.State = &srcState{StateFiles: &fake.StateFiles{Def: []byte(def), Items: items}, cand: []byte(def)}
	a.Secrets = &fake.SecretStore{M: map[string]json.RawMessage{contract.SourcePullKeyRef: pullKeySecret(t, newAdminKey(t))}}
	r, err := a.SourceCheck(ctx, contract.Call{Words: []string{"source", "check"}, RunID: testRunID})
	harness.Shape(t, []string{"source", "check"}, r, err)
	if verr := contract.Validate("cli/results.json#/$defs/source_check", r.Result); verr != nil {
		t.Fatalf("結果が source_check のスキーマに合わない: %v", verr)
	}
	return r
}

func TestContractAcquireSourceCheckUncoveredVectors(t *testing.T) {
	_, perr := shapeAcquire().SourceCheck(ctx, contract.Call{Words: []string{"source", "check"}})
	harness.Implemented(t, perr)
	var d struct {
		Check []struct {
			Name   string
			In     harness.CheckIn
			Expect []harness.Uncovered
		}
	}
	harness.LoadVectors(t, "survey/rules.json", &d)
	if len(d.Check) < 10 {
		t.Fatalf("ベクトルの数: %d", len(d.Check))
	}
	for _, v := range d.Check {
		t.Run(v.Name, func(t *testing.T) {
			r := runRuleCheck(t, v.In)
			var res struct {
				Uncovered, Actionable, NewSinceLast []ucItem
			}
			var raw map[string]json.RawMessage
			_ = json.Unmarshal(r.Result, &raw)
			_ = json.Unmarshal(raw["uncovered"], &res.Uncovered)
			_ = json.Unmarshal(raw["actionable"], &res.Actionable)
			_ = json.Unmarshal(raw["new_since_last"], &res.NewSinceLast)
			want := append([]harness.Uncovered{}, v.Expect...)
			harness.SortUncovered(want)
			if got := toUncovered(res.Uncovered); !reflect.DeepEqual(got, want) {
				gb, _ := json.Marshal(got)
				wb, _ := json.Marshal(want)
				t.Errorf("uncovered[] が U0 のベクトルと違う:\n got  %s\n want %s", gb, wb)
			}
			var act []harness.Uncovered
			for _, u := range v.Expect {
				switch u.Kind {
				case "mount", "mount_parent", "db", "db_cluster":
					act = append(act, u)
				}
			}
			if act == nil {
				act = []harness.Uncovered{}
			}
			harness.SortUncovered(act)
			if got := toUncovered(res.Actionable); !reflect.DeepEqual(got, act) {
				gb, _ := json.Marshal(got)
				wb, _ := json.Marshal(act)
				t.Errorf("actionable[] は uncovered[] のうち足せる種別だけ:\n got  %s\n want %s", gb, wb)
			}
			if got := toUncovered(res.NewSinceLast); !reflect.DeepEqual(got, want) {
				t.Errorf("前回の記録が無いので new_since_last[] は uncovered[] と同じ: %+v", res.NewSinceLast)
			}
		})
	}
}
