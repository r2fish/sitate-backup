package acquire

// 契約試験(作業指示書 5.4 の 1・2・4): source survey の入口を、試験の中の ssh サーバー(元サーバーの代わり)に対して確かめる。
// 候補の判定は U0 の試験ベクトル(testdata/survey/rules.json の survey)と同じ結果になること。

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

var surveyWords = []string{"source", "survey"}

// surveyDoc は調査の処理の標準出力(sitate.source-survey/v1)。判定の入力のベクトルを、材料としてそのまま出す。
func surveyDoc(t *testing.T, in harness.SourceSurveyIn, scope string, prereq []string) string {
	t.Helper()
	b, _ := json.Marshal(in)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"mounts", "dirs", "clusters", "databases"} {
		if m[k] == nil {
			m[k] = []any{}
		}
	}
	if scope == contract.SurveyScopeDBOnly {
		m["mounts"], m["dirs"], m["databases"], m["databases_complete"] = []any{}, []any{}, []any{}, true
	}
	if prereq == nil {
		prereq = []string{}
	}
	m["format"], m["scope"], m["prereq_missing"] = contract.SourceSurveyFormat, scope, prereq
	out, _ := json.Marshal(m)
	if err := contract.Validate("receiver/survey.json#/$defs/source_survey", out); err != nil {
		t.Fatalf("試験の調査の出力が U0 のスキーマに合わない(試験の誤り): %v", err)
	}
	return string(out) + "\n"
}

// surveyReply は調査の処理(標準入力が SITATE_SCOPE の代入で始まる)に doc を返す。
func surveyReply(doc string) func(execRec) (string, int) {
	return func(r execRec) (string, int) {
		if strings.HasPrefix(r.Stdin, contract.EmbedVarScope+"=") {
			return doc, 0
		}
		return "", 1
	}
}

type surveyRes struct {
	HostKeyFP     string   `json:"host_key_fp"`
	PrereqMissing []string `json:"prereq_missing"`
	harness.SurveyOut
}

func normOut(o *harness.SurveyOut) {
	if o.Mounts == nil {
		o.Mounts = []harness.OutMount{}
	}
	if o.Dirs == nil {
		o.Dirs = []harness.OutDir{}
	}
	if o.Databases == nil {
		o.Databases = []harness.OutDB{}
	}
	if o.Clusters == nil {
		o.Clusters = []harness.OutCluster{}
	}
	sort.Slice(o.Mounts, func(i, j int) bool { return o.Mounts[i].Path < o.Mounts[j].Path })
	sort.Slice(o.Dirs, func(i, j int) bool { return o.Dirs[i].Path < o.Dirs[j].Path })
	sort.Slice(o.Databases, func(i, j int) bool {
		a, b := o.Databases[i], o.Databases[j]
		return a.Port < b.Port || (a.Port == b.Port && a.Name < b.Name)
	})
	sort.Slice(o.Clusters, func(i, j int) bool { return o.Clusters[i].Port < o.Clusters[j].Port })
}

// errEnvelope は誤りが封筒の error として原因コードのスキーマ(detail を含む)に合うことを確かめる。
func errEnvelope(t *testing.T, ce *contract.Error) {
	t.Helper()
	next := ce.Next
	if next == nil {
		next = []contract.Next{}
	}
	env := map[string]any{"schema": "sitate.out/v1", "command": []string{"x"}, "ok": false, "dry_run": false, "run_id": testRunID, "result": nil, "warnings": []any{},
		"error": map[string]any{"code": ce.Code, "class": ce.Class, "retryable": ce.Retryable, "detail": ce.Detail, "next": next}}
	b, _ := json.Marshal(env)
	if err := contract.Validate("cli/envelope.json", b); err != nil {
		t.Errorf("%s: 封筒のスキーマに合わない: %v (detail=%s)", ce.Code, err, ce.Detail)
	}
}

func seenOf(t *testing.T, items map[string]json.RawMessage) map[string][]string {
	t.Helper()
	var l []struct {
		Host string `json:"host"`
		FP   string `json:"host_key_fp"`
		At   string `json:"at"`
	}
	if raw := items["source_seen"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &l); err != nil {
			t.Fatalf("source_seen を読めない: %v", err)
		}
		for _, e := range l {
			if err := contract.Validate("data/state.json#/$defs/source_seen", mustJSON(e)); err != nil {
				t.Errorf("source_seen の要素がスキーマに合わない: %v", err)
			}
		}
	}
	m := map[string][]string{}
	for _, e := range l {
		m[e.Host] = append(m[e.Host], e.FP)
	}
	return m
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// 候補の判定: U0 の調査のベクトルを調査の処理の出力として返し、結果の dirs・mounts・databases・clusters・sizes_complete・databases_complete が判定と一致する。
func TestContractAcquireSourceSurvey(t *testing.T) {
	_, perr := shapeAcquire().SourceSurvey(ctx, contract.Call{Words: surveyWords})
	harness.Implemented(t, perr) // 親にも未実装の印を残す(契約試験の集計は親ごとに原因を見る)
	var d struct {
		Survey []struct {
			Name   string
			In     harness.SourceSurveyIn
			Expect harness.SurveyOut
		}
	}
	harness.LoadVectors(t, "survey/rules.json", &d)
	if len(d.Survey) < 6 {
		t.Fatalf("ベクトルの数: %d", len(d.Survey))
	}
	for _, v := range d.Survey {
		t.Run(v.Name, func(t *testing.T) {
			key := newAdminKey(t)
			src := startSource(t, nil)
			doc := surveyDoc(t, v.In, contract.SurveyScopeFull, []string{"psql"})
			src.Reply = surveyReply(doc)
			a, st := newSrcAcquire(map[string]json.RawMessage{})
			r, err := a.SourceSurvey(ctx, sourceCall(surveyWords, src.URL("root"), key))
			harness.Shape(t, surveyWords, r, err)
			if verr := contract.Validate("cli/results.json#/$defs/source_survey", r.Result); verr != nil {
				t.Fatalf("結果が source_survey のスキーマに合わない: %v", verr)
			}
			var got surveyRes
			harness.Result(t, r, &got)
			want := v.Expect
			normOut(&got.SurveyOut)
			normOut(&want)
			if !reflect.DeepEqual(got.SurveyOut, want) {
				t.Errorf("判定が U0 のベクトルと違う:\n got  %s\n want %s", mustJSON(got.SurveyOut), mustJSON(want))
			}
			if got.HostKeyFP != src.FP {
				t.Errorf("host_key_fp は接続で見えた指紋 %s: %s", src.FP, got.HostKeyFP)
			}
			if !reflect.DeepEqual(got.PrereqMissing, []string{"psql"}) {
				t.Errorf("prereq_missing は調査の値のとおり: %v", got.PrereqMissing)
			}
			ex := src.Execs()
			if len(ex) != 1 {
				t.Fatalf("調査の処理は 1 回だけ流す: %d 回", len(ex))
			}
			pre, _ := harness.SurveyPreamble(contract.SurveyScopeFull, contract.SurveyDuTimeoutS)
			if ex[0].User != "root" || ex[0].Cmd != "sh -s" || !strings.HasPrefix(ex[0].Stdin, pre) {
				t.Errorf("root は `sh -s` に、範囲 full と du の打ち切りを先頭に埋め込んだ処理を渡す: %+v", execRec{ex[0].User, ex[0].Cmd, firstLine(ex[0].Stdin)})
			}
			if strings.Contains(ex[0].Stdin, "OPENSSH PRIVATE KEY") {
				t.Error("処理に管理用の秘密鍵を埋め込んだ")
			}
			if s := seenOf(t, st.Items); !reflect.DeepEqual(s, map[string][]string{"127.0.0.1": {src.FP}}) {
				t.Errorf("見えた指紋を source_seen のその接続先の要素に置く: %v", s)
			}
		})
	}
}

func firstLine(s string) string { return strings.SplitN(s, "\n", 2)[0] }

// root 以外のユーザーは `sudo -n sh -s`。
func TestContractAcquireSourceSurveySudo(t *testing.T) {
	src := startSource(t, nil)
	src.Reply = surveyReply(surveyDoc(t, harness.SourceSurveyIn{DatabasesComplete: true}, contract.SurveyScopeFull, nil))
	a, _ := newSrcAcquire(map[string]json.RawMessage{})
	r, err := a.SourceSurvey(ctx, sourceCall(surveyWords, src.URL("ops"), newAdminKey(t)))
	harness.Shape(t, surveyWords, r, err)
	if ex := src.Execs(); len(ex) != 1 || ex[0].User != "ops" || ex[0].Cmd != "sudo -n sh -s" {
		t.Errorf("root 以外は `sudo -n sh -s`: %+v", ex)
	}
}

// 打ち切り(bytes が null)と読めないクラスタが結果の sizes_complete・databases_complete になることは、ベクトルの
// du-timeout-and-dir-labels と unreadable-cluster-makes-databases-incomplete が TestContractAcquireSourceSurvey で通す。

// seal の後に固定値と照らす接続先で違えば、処理を流さずに SOURCE_HOSTKEY_MISMATCH を返し、記録しない。
func TestContractAcquireSourceSurveyHostKey(t *testing.T) {
	_, perr := shapeAcquire().SourceSurvey(ctx, contract.Call{Words: surveyWords})
	harness.Implemented(t, perr) // 親にも未実装の印を残す(契約試験の集計は親ごとに原因を見る)
	doc := surveyDoc(t, harness.SourceSurveyIn{DatabasesComplete: true}, contract.SurveyScopeFull, nil)
	run := func(t *testing.T, items func(src *fakeSource) map[string]json.RawMessage) (*fakeSource, contract.Reply, error, map[string]json.RawMessage) {
		src := startSource(t, surveyReply(doc))
		it := items(src)
		a, st := newSrcAcquire(it)
		r, err := a.SourceSurvey(ctx, sourceCall(surveyWords, src.URL("root"), newAdminKey(t)))
		return src, r, err, st.Items
	}
	t.Run("seal の後の固定値と違えば止める", func(t *testing.T) {
		src, _, err, items := run(t, func(*fakeSource) map[string]json.RawMessage {
			return map[string]json.RawMessage{"sealed": sealedItem(true), "host_keys": hostKeysItem("127.0.0.1", fpA), "source_seen": seenItem("127.0.0.1", fpB)}
		})
		ce := harness.ExpectCode(t, err, contract.CodeSourceHostkeyMismatch)
		errEnvelope(t, ce)
		dm := map[string]any{}
		_ = json.Unmarshal(ce.Detail, &dm)
		if dm["expected_fp"] != fpA || dm["seen_fp"] != src.FP {
			t.Errorf("expected_fp は固定値、seen_fp は見えた指紋: %s", ce.Detail)
		}
		if len(src.Execs()) != 0 {
			t.Errorf("違えば調査の処理を流さない: %d 回", len(src.Execs()))
		}
		if s := seenOf(t, items); !reflect.DeepEqual(s, map[string][]string{"127.0.0.1": {fpB}}) {
			t.Errorf("違えば記録しない(source_seen は元のまま): %v", s)
		}
	})
	t.Run("seal の後の固定値と同じなら通って記録する", func(t *testing.T) {
		src, r, err, items := run(t, func(src *fakeSource) map[string]json.RawMessage {
			return map[string]json.RawMessage{"sealed": sealedItem(true), "host_keys": hostKeysItem("127.0.0.1", src.FP)}
		})
		harness.Shape(t, surveyWords, r, err)
		if s := seenOf(t, items); !reflect.DeepEqual(s, map[string][]string{"127.0.0.1": {src.FP}}) {
			t.Errorf("source_seen: %v", s)
		}
	})
	t.Run("固定値の無い接続先(元サーバーの移転先)は照らさない", func(t *testing.T) {
		src, r, err, _ := run(t, func(*fakeSource) map[string]json.RawMessage {
			return map[string]json.RawMessage{"sealed": sealedItem(true), "host_keys": hostKeysItem("old.example.invalid", fpA)}
		})
		harness.Shape(t, surveyWords, r, err)
		if len(src.Execs()) != 1 {
			t.Errorf("調査の処理を流す: %d 回", len(src.Execs()))
		}
	})
	t.Run("seal の前は固定値と違っても照らさず、同じ接続先の記録を置き換え、他の接続先の記録は残す", func(t *testing.T) {
		src, r, err, items := run(t, func(*fakeSource) map[string]json.RawMessage {
			return map[string]json.RawMessage{"sealed": sealedItem(false), "host_keys": hostKeysItem("127.0.0.1", fpA), "source_seen": seenItem("127.0.0.1", fpB, "other.example.invalid", fpC)}
		})
		harness.Shape(t, surveyWords, r, err)
		want := map[string][]string{"127.0.0.1": {src.FP}, "other.example.invalid": {fpC}}
		if s := seenOf(t, items); !reflect.DeepEqual(s, want) {
			t.Errorf("source_seen: %v want %v", s, want)
		}
	})
}

// 最初の接続の失敗: 認証が通らない(SOURCE_SURVEY_FAILED connect/auth)、資格情報が鍵として読めない(key_unparsable)、届かない(STORAGE_UNREACHABLE, target source)。
func TestContractAcquireSourceSurveyFailures(t *testing.T) {
	_, perr := shapeAcquire().SourceSurvey(ctx, contract.Call{Words: surveyWords})
	harness.Implemented(t, perr) // 親にも未実装の印を残す(契約試験の集計は親ごとに原因を見る)
	step := surveyStep
	t.Run("認証が通らない", func(t *testing.T) {
		src := startSource(t, nil)
		src.Reject = true
		a, st := newSrcAcquire(map[string]json.RawMessage{})
		_, err := a.SourceSurvey(ctx, sourceCall(surveyWords, src.URL("root"), newAdminKey(t)))
		step(t, harness.ExpectCode(t, err, contract.CodeSourceSurveyFailed), "connect", "auth")
		if len(src.Execs()) != 0 || len(st.Items["source_seen"]) != 0 {
			t.Error("失敗で何も流さず、記録しない")
		}
	})
	t.Run("資格情報が鍵として読めない", func(t *testing.T) {
		src := startSource(t, nil)
		a, st := newSrcAcquire(map[string]json.RawMessage{})
		_, err := a.SourceSurvey(ctx, sourceCall(surveyWords, src.URL("root"), "this is not a private key"))
		step(t, harness.ExpectCode(t, err, contract.CodeSourceSurveyFailed), "connect", "key_unparsable")
		if len(src.Execs()) != 0 || len(st.Items["source_seen"]) != 0 {
			t.Error("失敗で何も流さず、記録しない")
		}
	})
	t.Run("届かない", func(t *testing.T) {
		src := startSource(t, nil)
		url := src.URL("root")
		src.ln.Close()
		a, _ := newSrcAcquire(map[string]json.RawMessage{})
		_, err := a.SourceSurvey(ctx, sourceCall(surveyWords, url, newAdminKey(t)))
		ce := harness.ExpectCode(t, err, contract.CodeStorageUnreachable)
		var d struct{ Target string }
		_ = json.Unmarshal(ce.Detail, &d)
		if d.Target != "source" {
			t.Errorf("detail.target は source: %s", ce.Detail)
		}
		errEnvelope(t, ce)
	})
}

func surveyStep(t *testing.T, ce *contract.Error, wantStep, wantReason string) {
	t.Helper()
	var d struct{ Step, Reason string }
	_ = json.Unmarshal(ce.Detail, &d)
	if d.Step != wantStep || d.Reason != wantReason {
		t.Errorf("detail は step=%s reason=%s: %s", wantStep, wantReason, ce.Detail)
	}
	errEnvelope(t, ce)
}

// 調査を始められない失敗のうち、元サーバーの側の合図で決まるもの(contract.SurveyExitUnsupportedOS・sudo の終了状態。判定の場所は contract/source.go)。
func TestContractAcquireSourceSurveySudoOS(t *testing.T) {
	_, perr := shapeAcquire().SourceSurvey(ctx, contract.Call{Words: surveyWords})
	harness.Implemented(t, perr) // 親にも未実装の印を残す(契約試験の集計は親ごとに原因を見る)
	step := surveyStep
	for _, c := range []struct {
		name, user string
		exit       int
		step, why  string
	}{
		{"sudo がパスワードを求める", "ops", 1, "sudo", "sudo_unavailable"},
		{"sudo が無い", "ops", 127, "sudo", "sudo_unavailable"},
		{"OS が Linux でない(root)", "root", contract.SurveyExitUnsupportedOS, "prereq", "unsupported_os"},
		{"OS が Linux でない(sudo 経由)", "ops", contract.SurveyExitUnsupportedOS, "prereq", "unsupported_os"},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := startSource(t, func(execRec) (string, int) { return "", c.exit })
			a, st := newSrcAcquire(map[string]json.RawMessage{})
			_, err := a.SourceSurvey(ctx, sourceCall(surveyWords, src.URL(c.user), newAdminKey(t)))
			step(t, harness.ExpectCode(t, err, contract.CodeSourceSurveyFailed), c.step, c.why)
			if len(st.Items["source_seen"]) != 0 {
				t.Error("失敗は記録しない")
			}
		})
	}
	t.Run("root の終了 1 は sudo の失敗にしない", func(t *testing.T) {
		src := startSource(t, func(execRec) (string, int) { return "", 1 })
		a, _ := newSrcAcquire(map[string]json.RawMessage{})
		_, err := a.SourceSurvey(ctx, sourceCall(surveyWords, src.URL("root"), newAdminKey(t)))
		harness.Implemented(t, err)
		var ce *contract.Error
		if errors.As(err, &ce) && ce.Code == contract.CodeSourceSurveyFailed {
			t.Errorf("root は sudo を使わない: %s", ce.Detail)
		}
	})
}

// pull 接続のポートは 22 固定(spec 3.3)。22 以外は接続の前に ARG_INVALID(--host、unsupported_port)で止める。省略は 22。
func TestContractAcquireSourcePortNot22(t *testing.T) {
	_, perr := shapeAcquire().SourceSurvey(ctx, contract.Call{Words: surveyWords})
	harness.Implemented(t, perr)
	src := startSource(t, nil)
	a, _ := newSrcAcquire(map[string]json.RawMessage{})
	_, err := a.SourceSurvey(ctx, sourceCall(surveyWords, "ssh://root@127.0.0.1:2222", newAdminKey(t)))
	ce := harness.ExpectCode(t, err, contract.CodeArgInvalid)
	var d struct{ Flag, Reason string }
	if json.Unmarshal(ce.Detail, &d) != nil || d.Flag != "--host" || d.Reason != "unsupported_port" {
		t.Errorf("detail: %s", ce.Detail)
	}
	if src.Conns() != 0 {
		t.Error("接続した")
	}
}
