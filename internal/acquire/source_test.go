package acquire

// 契約試験に置けなかった source survey・source enroll・source check の中身の試験(作業指示書 5.4 の 2・4・5)。

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// Surveyor.Judge の候補の判定を U0 の調査のベクトルに直接通す(SSH を介さない)。
func TestJudgeSurveyVectors(t *testing.T) {
	var d struct {
		Survey []struct {
			Name   string
			In     harness.SourceSurveyIn
			Expect harness.SurveyOut
		}
	}
	harness.LoadVectors(t, "survey/rules.json", &d)
	for _, v := range d.Survey {
		t.Run(v.Name, func(t *testing.T) {
			doc := surveyDoc(t, v.In, contract.SurveyScopeFull, nil)
			raw, err := (&Surveyor{}).Judge(ctx, json.RawMessage(doc), nil)
			if err != nil {
				t.Fatal(err)
			}
			var got harness.SurveyOut
			_ = json.Unmarshal(raw, &got)
			want := v.Expect
			normOut(&got)
			normOut(&want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("判定がベクトルと違う:\n got  %s\n want %s", mustJSON(got), mustJSON(want))
			}
		})
	}
}

// Judge の recv-survey の側(source check の入口と同じ判定)をベクトルに直接通す。
func TestJudgeCheckVectors(t *testing.T) {
	var d struct {
		Check []struct {
			Name   string
			In     harness.CheckIn
			Expect []harness.Uncovered
		}
	}
	harness.LoadVectors(t, "survey/rules.json", &d)
	for _, v := range d.Check {
		t.Run(v.Name, func(t *testing.T) {
			recv := map[string]any{"format": "sitate.recv-survey/v1", "mounts": v.In.Mounts, "clusters": v.In.Clusters}
			if v.In.Mounts == nil {
				recv["mounts"] = []any{}
			}
			if v.In.Clusters == nil {
				recv["clusters"] = []any{}
			}
			sc, _ := json.Marshal(v.In.SourceClusters)
			raw, err := (&Surveyor{SourceClusters: sc}).Judge(ctx, mustJSON(recv), []byte(ruleCheckDef(v.In)))
			if err != nil {
				t.Fatal(err)
			}
			var res struct{ Uncovered []ucItem }
			_ = json.Unmarshal(raw, &res)
			want := append([]harness.Uncovered{}, v.Expect...)
			harness.SortUncovered(want)
			if got := toUncovered(res.Uncovered); !reflect.DeepEqual(got, want) {
				t.Errorf("uncovered[]:\n got  %s\n want %s", mustJSON(got), mustJSON(want))
			}
		})
	}
}

// pull 接続のポートは 22 固定: source enroll も 22 以外は接続の前に ARG_INVALID(--host、unsupported_port)。省略と :22 は通る。
func TestSourceEnrollPortNot22(t *testing.T) {
	src := startSource(t, nil)
	a, _ := newSrcAcquire(enrollItems(nil))
	_, err := a.SourceEnroll(ctx, sourceCall(enrollWords, "ssh://root@127.0.0.1:2222", newAdminKey(t)))
	ce := harness.ExpectCode(t, err, contract.CodeArgInvalid)
	var d struct{ Flag, Reason string }
	if json.Unmarshal(ce.Detail, &d) != nil || d.Flag != "--host" || d.Reason != "unsupported_port" {
		t.Errorf("detail: %s", ce.Detail)
	}
	if src.Conns() != 0 || len(src.Execs()) != 0 {
		t.Error("接続した")
	}
	for _, h := range []string{"ssh://root@127.0.0.1", "ssh://root@127.0.0.1:22"} {
		if _, e := parseHostArg(h); e != nil {
			t.Errorf("%s は通る: %v", h, e)
		}
	}
}

// apply の中の enroll(WriteState: false)は、host_keys・受け口の記録を書かずに登録の結果を返し、提案の指紋と違えば SOURCE_HOSTKEY_MISMATCH。
func TestEnrollerApplyDoesNotWriteState(t *testing.T) {
	run := func(proposalFP func(*enrollWorld) string) (contract.EnrollResult, error, *fake.StateFiles, *enrollWorld) {
		w := newEnrollWorld(t)
		st := &fake.StateFiles{Def: []byte(testDef), Items: enrollItems(nil)}
		sec := &fake.SecretStore{M: map[string]json.RawMessage{}}
		e := &SourceEnroller{SSH: routeSSH{}, Secrets: sec, State: &srcState{StateFiles: st, cand: []byte(testDef)}, From: "203.0.113.9"}
		in := contract.EnrollInput{Kind: "source", Section: json.RawMessage(testDef), HostKeyFP: proposalFP(w), WriteState: false,
			Host: w.src.URL("root"), AdminRef: "admin", Admin: contract.NewSecret([]byte(newAdminKey(t)))}
		r, err := e.Enroll(ctx, in)
		return r, err, st, w
	}
	r, err, st, w := run(func(w *enrollWorld) string { return w.src.FP })
	if err != nil {
		t.Fatal(err)
	}
	if r.ReceiverHash != shaOf(longestQuoted(w.setup)) || w.setup == "" {
		t.Errorf("receiver_hash: %q", r.ReceiverHash)
	}
	var sc struct {
		Host     string
		Clusters []stateCluster
	}
	if json.Unmarshal(r.SourceClusters, &sc) != nil || sc.Host != "127.0.0.1" || len(sc.Clusters) != 1 || sc.Clusters[0].Major != 16 {
		t.Errorf("source_clusters: %s", r.SourceClusters)
	}
	for _, k := range []string{"host_keys", "source_clusters", "receiver_hash", "source_seen"} {
		if len(st.Items[k]) != 0 {
			t.Errorf("WriteState が偽なのに %s を書いた: %s", k, st.Items[k])
		}
	}
	_, err, _, w2 := run(func(*enrollWorld) string { return fpA })
	ce := harness.ExpectCode(t, err, contract.CodeSourceHostkeyMismatch)
	var d map[string]any
	_ = json.Unmarshal(ce.Detail, &d)
	if d["expected_fp"] != fpA || d["seen_fp"] != w2.src.FP {
		t.Errorf("expected_fp は提案の指紋: %s", ce.Detail)
	}
	if len(w2.src.Execs()) != 0 {
		t.Error("提案の指紋と違えば以降を流さない")
	}
}

// DB の対象の自己診断の項目: peer の行が無い・権限不足・道具の版・RECEIVER_TARGETS_MISMATCH の next(seal の前後)。
func TestSelftestItemsErrors(t *testing.T) {
	def, _ := parseCheckDef([]byte(`{"source":{"host":"h","targets":[{"id":"t1","postgresql":"app"}]}}`))
	pg := func(mod func(*selfTarget)) selfDoc {
		d := selfDoc{ReceiverHash: "h"}
		d.AuthorizedKeys.Lines, d.AuthorizedKeys.Match = 1, true
		ver, one := 160000, 160000
		tg := selfTarget{ID: "t1", Kind: "postgresql", DB: "app", SocketDir: "/run/postgresql", Port: 5432, Connect: true, ServerVersionNum: &ver, PgDumpVersionNum: &one,
			UnreadableTables: intp(0), LargeObjects: new(bool)}
		mod(&tg)
		d.Targets = []selfTarget{tg}
		return d
	}
	cases := []struct {
		name         string
		mod          func(*selfTarget)
		reason, lack string
	}{
		{"peer の行が無い", func(t *selfTarget) { t.Connect = false; e := "auth"; t.ConnectError = &e }, "db_connect", ""},
		{"CONNECT が無い", func(t *selfTarget) { t.Connect = false; e := "connect_privilege"; t.ConnectError = &e }, "db_privileges", "connect"},
		{"読めない表", func(t *selfTarget) { t.UnreadableTables = intp(3) }, "db_privileges", "tables"},
		{"pg_dump が古い", func(t *selfTarget) { v := 150000; t.PgDumpVersionNum = &v }, "db_tools", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := itemsError(evalSelftest(pg(c.mod), "h", def), false)
			if e == nil || e.Code != contract.CodeSourceSetupFailed {
				t.Fatalf("SOURCE_SETUP_FAILED を期待: %v", e)
			}
			var d struct {
				Step, Reason string
				TargetID     string   `json:"target_id"`
				Lacks        []string `json:"lacks"`
			}
			_ = json.Unmarshal(e.Detail, &d)
			if d.Step != "selftest" || d.Reason != c.reason || d.TargetID != "t1" || (c.lack != "" && (len(d.Lacks) != 1 || d.Lacks[0] != c.lack)) {
				t.Errorf("detail: %s", e.Detail)
			}
		})
	}
	mm := pg(func(t *selfTarget) { t.DB = "other" })
	for sealed, want := range map[bool]string{false: "enroll", true: "source-change"} {
		e := itemsError(evalSelftest(mm, "h", def), sealed)
		if e == nil || e.Code != contract.CodeReceiverTargetsMismatch || len(e.Next) != 1 || !strings.Contains(strings.Join(e.Next[0].Argv, " "), want) {
			t.Errorf("sealed=%v の next は %s: %+v", sealed, want, e)
		}
	}
	lo := pg(func(t *selfTarget) { b := true; t.LargeObjects = &b })
	if items := evalSelftest(lo, "h", def); len(items) == 0 {
		t.Error("項目が無い")
	}
}

// 受け口の selftest がラージオブジェクトありと返した DB の対象は、uncovered[] に kind: large_object で出る。
func TestSourceCheckReportsLargeObjects(t *testing.T) {
	def := `{"version":1,"source":{"host":"127.0.0.1","targets":[{"id":"t1","postgresql":"app"}]}}`
	self := mustJSON(map[string]any{"format": "sitate.recv-selftest/v1", "version": "0.1.0", "receiver_hash": checkHash,
		"authorized_keys": map[string]any{"lines": 1, "match": true},
		"targets": []any{map[string]any{"id": "t1", "kind": "postgresql", "db": "app", "socket_dir": "/run/postgresql", "port": 5432, "connect": true, "connect_error": nil,
			"server_version_num": 160000, "pg_dump_version_num": 160000, "unreadable_tables": 0, "large_objects": true}}})
	rs := mustJSON(map[string]any{"format": "sitate.recv-survey/v1", "mounts": []any{}, "clusters": []any{map[string]any{"port": 5432, "names": []string{"postgres", "app"}}}})
	src := startSource22(t, func(r execRec) (string, int) {
		if strings.Contains(r.Cmd, "selftest") {
			return string(self), 0
		}
		return string(rs), 0
	})
	sc := mustJSON(map[string]any{"host": "127.0.0.1", "clusters": []any{map[string]any{"socket_dir": "/run/postgresql", "port": 5432, "major": 16, "raw_paths": []string{"/var/lib/postgresql/16/main"}}}})
	items := map[string]json.RawMessage{"sealed": sealedItem(true), "host_keys": hostKeysItem("127.0.0.1", src.FP), "receiver_hash": json.RawMessage(`"` + checkHash + `"`), "source_clusters": sc}
	a, _ := newSrcAcquire(items)
	a.State = &srcState{StateFiles: &fake.StateFiles{Def: []byte(def), Items: items}, cand: []byte(def)}
	a.Secrets = &fake.SecretStore{M: map[string]json.RawMessage{contract.SourcePullKeyRef: pullKeySecret(t, newAdminKey(t))}}
	r, err := a.SourceCheck(ctx, contract.Call{Words: []string{"source", "check"}, RunID: testRunID})
	harness.Shape(t, []string{"source", "check"}, r, err)
	if verr := contract.Validate("cli/results.json#/$defs/source_check", r.Result); verr != nil {
		t.Fatal(verr)
	}
	var res struct{ Uncovered []ucItem }
	_ = json.Unmarshal(r.Result, &res)
	if len(res.Uncovered) != 1 || res.Uncovered[0].Kind != "large_object" || res.Uncovered[0].TargetID == nil || *res.Uncovered[0].TargetID != "t1" {
		t.Errorf("uncovered[]: %+v", res.Uncovered)
	}
}
