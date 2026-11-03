package cli

// verify・extend・prune の入口(spec 3.1.2。作業指示書 5.18)。保持の判断は store(U1b)が持ち、cli は結果の形・通知・警告への写しだけを持つ。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

type stubRet struct {
	*fake.Retention
	verify   contract.VerifyResult
	extend   map[string]contract.ExtendResult
	extErr   error
	prune    contract.PruneResult
	pruneEr  error
	calls    []string
	refs     []contract.VersionRef
	onVerify func() // 一覧を取った後、結果を返す前に起きること(並行する backup の模擬)
}

func (s *stubRet) Verify(_ context.Context, r []contract.VersionRef) (contract.VerifyResult, error) {
	s.calls, s.refs = append(s.calls, "verify"), r
	if s.onVerify != nil {
		s.onVerify()
	}
	return s.verify, nil
}

func (s *stubRet) Extend(_ context.Context, t string) (contract.ExtendResult, error) {
	s.calls = append(s.calls, "extend-"+t)
	return s.extend[t], s.extErr
}

func (s *stubRet) Prune(context.Context) (contract.PruneResult, error) {
	s.calls = append(s.calls, "prune")
	return s.prune, s.pruneEr
}

type emitErrSpy struct {
	contract.Recorder
	got  *[]emitted
	errs *[]error
}

func (s emitErrSpy) Emit(ctx context.Context, typ string, facts []byte) (contract.Event, []contract.DeliveryResult, error) {
	ev, dr, err := s.Recorder.Emit(ctx, typ, facts)
	*s.got = append(*s.got, emitted{typ, string(facts)})
	*s.errs = append(*s.errs, err)
	return ev, dr, err
}

func retEnv(t *testing.T, s *stubRet) (*Deps, *[]emitted) {
	t.Helper()
	f := newFlow(t)
	var got []emitted
	var errs []error
	f.d.Recorder = emitErrSpy{f.d.Recorder, &got, &errs}
	s.Retention = &fake.Retention{}
	f.d.Store = s
	f.d.Index = &fake.Index{M: map[string]contract.ChunkRef{}}
	t.Cleanup(func() {
		for _, e := range errs {
			if e != nil {
				t.Errorf("通知の事実がスキーマに合わない: %v", e)
			}
		}
	})
	return f.d, &got
}

func callOf2(words ...string) contract.Call { return contract.Call{Words: words, RunID: runA} }

func decode(t *testing.T, rep contract.Reply) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rep.Result, &m); err != nil {
		t.Fatalf("%v: %s", err, rep.Result)
	}
	return m
}

func typesOf(g []emitted) string {
	var s []string
	for _, e := range g {
		s = append(s, e.typ)
	}
	return strings.Join(s, ",")
}

func TestVerifyResultShapeAndNoEventWhenClean(t *testing.T) {
	s := &stubRet{verify: contract.VerifyResult{NoncurrentVersions: 2, BytesWrittenVsBucket: 1.01}}
	d, got := retEnv(t, s)
	rep, err := verifyEntry(d, bg, callOf2("verify"))
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, rep)
	for _, k := range []string{"missing", "retention_short", "retention_long", "unexpected_versions", "noncurrent_versions", "delete_markers", "bytes_written_vs_bucket", "journal_chain_ok", "sampled"} {
		if _, ok := m[k]; !ok {
			t.Errorf("verify の結果に %s が無い: %s", k, rep.Result)
		}
	}
	if m["missing"] == nil || m["unexpected_versions"] == nil {
		t.Errorf("空の配列は null にしない: %s", rep.Result)
	}
	if err := contract.Validate("cli/results.json#/$defs/verify", rep.Result); err != nil {
		t.Errorf("スキーマ: %v", err)
	}
	if len(*got) != 0 || len(rep.Warnings) != 0 {
		t.Errorf("異常が無いのに通知・警告: %v %v", *got, rep.Warnings)
	}
}

func TestVerifyMissingAndShortRaiseVerifyFailed(t *testing.T) {
	s := &stubRet{verify: contract.VerifyResult{
		Missing:        []contract.VersionRef{{Key: "chunks/ab/x", VersionID: "v1"}},
		RetentionShort: []contract.RetentionMismatch{{Key: "k", VersionID: "v2", Recorded: fake.StartTime.Add(time.Hour), Actual: fake.StartTime}},
		DeleteMarkers:  1,
	}}
	d, got := retEnv(t, s)
	rep, err := verifyEntry(d, bg, callOf2("verify"))
	if err != nil {
		t.Fatal(err)
	}
	if typesOf(*got) != "verify.failed" || !strings.Contains((*got)[0].facts, `"missing_count":1`) || !strings.Contains((*got)[0].facts, `"retention_short_count":1`) {
		t.Fatalf("通知: %+v", *got)
	}
	codes := ""
	for _, w := range rep.Warnings {
		codes += string(w.Code) + ","
	}
	if !strings.Contains(codes, "DELETE_MARKER_FOUND") {
		t.Errorf("削除マーカーの警告が無い: %s", codes)
	}
	if err := contract.Validate("cli/results.json#/$defs/verify", rep.Result); err != nil {
		t.Errorf("スキーマ: %v", err)
	}
}

func TestVerifyUnexpectedVersionIsErrorWithDetail(t *testing.T) {
	s := &stubRet{verify: contract.VerifyResult{UnexpectedVersions: []contract.VersionRef{{Key: "repo.json", VersionID: "vx"}}}}
	d, got := retEnv(t, s)
	_, err := verifyEntry(d, bg, callOf2("verify"))
	ce := toError(err)
	if ce == nil || ce.Code != contract.CodeUnexpectedVersion {
		t.Fatalf("UNEXPECTED_VERSION でない: %v", err)
	}
	if typesOf(*got) != "storage.unexpected_version" {
		t.Errorf("通知: %s", typesOf(*got))
	}
}

func TestVerifyRetentionLongAndSizeDriftWarn(t *testing.T) {
	s := &stubRet{verify: contract.VerifyResult{
		RetentionLong:        []contract.RetentionMismatch{{Key: "k", VersionID: "v", Recorded: fake.StartTime, Actual: fake.StartTime.Add(48 * time.Hour)}},
		BytesWrittenVsBucket: 1 + contract.SizeDriftRatio + 0.2,
	}}
	d, got := retEnv(t, s)
	rep, err := verifyEntry(d, bg, callOf2("verify"))
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, w := range rep.Warnings {
		codes = append(codes, string(w.Code))
	}
	if !strings.Contains(strings.Join(codes, ","), "RETENTION_LONG") || !strings.Contains(strings.Join(codes, ","), "STORAGE_SIZE_DRIFT") {
		t.Errorf("警告: %v", codes)
	}
	if !strings.Contains(typesOf(*got), "storage.retention_long") {
		t.Errorf("通知: %s", typesOf(*got))
	}
}

func TestExtendRunsBothTargetsAndReportsFailure(t *testing.T) {
	m1, m2 := fake.StartTime.Add(24*time.Hour), fake.StartTime.Add(48*time.Hour)
	s := &stubRet{extend: map[string]contract.ExtendResult{
		"A": {Extended: 3, MinRetainUntil: m2},
		"B": {Extended: 2, MinRetainUntil: m1, Failed: []contract.ExtendFailure{{Key: "chunks/ab/y", VersionID: "v9", Code: contract.CodeStorageUnreachable}}},
	}}
	d, got := retEnv(t, s)
	rep, err := extendEntry(d, bg, callOf2("extend"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.calls, ",") != "extend-A,extend-B" {
		t.Fatalf("呼び出し: %v", s.calls)
	}
	m := decode(t, rep)
	if m["extended"].(float64) != 5 || m["min_retain_until"] != m1.UTC().Format(time.RFC3339) || len(m["failed"].([]any)) != 1 {
		t.Errorf("結果: %s", rep.Result)
	}
	if err := contract.Validate("cli/results.json#/$defs/extend", rep.Result); err != nil {
		t.Errorf("スキーマ: %v", err)
	}
	if typesOf(*got) != "retention.extension_failed" {
		t.Errorf("通知: %s", typesOf(*got))
	}
}

func TestExtendNothingToExtendHasNullMin(t *testing.T) {
	s := &stubRet{extend: map[string]contract.ExtendResult{}}
	d, got := retEnv(t, s)
	rep, err := extendEntry(d, bg, callOf2("extend"))
	if err != nil {
		t.Fatal(err)
	}
	if m := decode(t, rep); m["min_retain_until"] != nil || m["failed"] == nil {
		t.Errorf("%s", rep.Result)
	}
	if len(*got) != 0 {
		t.Errorf("通知: %v", *got)
	}
}

func TestPruneResultHeldAndNotSealed(t *testing.T) {
	s := &stubRet{prune: contract.PruneResult{Held: true}}
	d, _ := retEnv(t, s)
	rep, err := pruneEntry(d, bg, callOf2("prune"))
	if err != nil {
		t.Fatal(err)
	}
	if m := decode(t, rep); m["held"] != true || m["deleted"].(float64) != 0 {
		t.Errorf("held: %s", rep.Result)
	}
	if err := contract.Validate("cli/results.json#/$defs/prune", rep.Result); err != nil {
		t.Errorf("スキーマ: %v", err)
	}
	// seal の前: 何もせず警告 NOT_SEALED(エラーにしない)
	s2 := &stubRet{pruneEr: newErr(contract.CodeNotSealed, nil)}
	d2, _ := retEnv(t, s2)
	rep, err = pruneEntry(d2, bg, callOf2("prune"))
	if err != nil || len(rep.Warnings) != 1 || rep.Warnings[0].Code != contract.CodeNotSealed {
		t.Fatalf("NOT_SEALED は警告: %v %+v", err, rep.Warnings)
	}
	if m := decode(t, rep); m["deleted"].(float64) != 0 || m["held"] != false {
		t.Errorf("%s", rep.Result)
	}
	// ほかの誤りは握りつぶさない
	s3 := &stubRet{pruneEr: newErr(contract.CodeTimeSkew, nil)}
	d3, _ := retEnv(t, s3)
	if _, err = pruneEntry(d3, bg, callOf2("prune")); toError(err) == nil || toError(err).Code != contract.CodeTimeSkew {
		t.Errorf("TIME_SKEW を返さない: %v", err)
	}
}

func TestExtendPruneDryRunNotImplementedAndNilStore(t *testing.T) {
	s := &stubRet{}
	d, _ := retEnv(t, s)
	for _, e := range []entry{extendEntry, pruneEntry} {
		c := callOf2("x")
		c.DryRun = true
		if _, err := e(d, bg, c); err == nil || len(s.calls) != 0 {
			t.Errorf("--dry-run は実行しない: %v %v", err, s.calls)
		}
	}
	d.Store = nil
	for _, e := range []entry{verifyEntry, extendEntry, pruneEntry} {
		if _, err := e(d, bg, callOf2("x")); err == nil {
			t.Errorf("部品が nil なら NOT_IMPLEMENTED")
		}
	}
}

func TestRoutesRegistered(t *testing.T) {
	for _, w := range []string{"verify", "extend", "prune"} {
		if _, ok := routeOwn[w]; !ok {
			t.Errorf("%s が routeOwn に無い", w)
		}
	}
}

// 連鎖の確かめ(4.2 の (3)〜(5)): 素直な連鎖は ok、項目を抜くと gap、項目の prev_hash を食い違わせる(キーの entry_hash と合わない)と prev_mismatch。
func TestJournalChainKinds(t *testing.T) {
	f := newFlow(t)
	for i := 0; i < 4; i++ {
		if err := f.d.recordItem(bg, "hold", runA, map[string]any{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := f.d.journalChain(bg)
	if err != nil || !st.ok {
		t.Fatalf("素直な連鎖: %+v %v", st, err)
	}
	dir := filepath.Join(f.d.StateDir, "journal")
	names, _ := filepath.Glob(filepath.Join(dir, "*"))
	sort.Strings(names)
	if len(names) < 4 {
		t.Fatalf("項目が %d 件: %v", len(names), names)
	}
	// 2 番目の項目のキーの entry_hash を書き換える(本体は同じ): 3 番目の prev_hash が合わなくなる。
	base := filepath.Base(names[1])
	fake := base[:13] + strings.Repeat("0", 64)
	if err := os.Rename(names[1], filepath.Join(dir, fake)); err != nil {
		t.Fatal(err)
	}
	st, err = f.d.journalChain(bg)
	if err != nil || st.ok || st.kind != "prev_mismatch" {
		t.Errorf("prev_mismatch: %+v %v", st, err)
	}
	if err := os.Rename(filepath.Join(dir, fake), names[1]); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(names[2]); err != nil { // 3 番目を抜く
		t.Fatal(err)
	}
	st, err = f.d.journalChain(bg)
	if err != nil || st.ok || st.kind != "gap" {
		t.Errorf("gap: %+v %v", st, err)
	}
}

// 欠け・保持不足が無かった verify は snapshots[] の refs_verified を true にし、journal/ に refs_verified を書く。欠けがあれば変えない。
func TestVerifyMarksRefsVerifiedOnlyWhenClean(t *testing.T) {
	s := &stubRet{}
	d, _ := retEnv(t, s)
	f := newFlow(t)
	d.Recorder, d.State = f.d.Recorder, f.d.State
	h, _ := d.State.Lock(bg)
	_ = d.State.Update(bg, h, "snapshots", json.RawMessage(`[{"ref":"m-`+strings.Repeat("a", 32)+`","version_id":"v1","refs_verified":null,"health":"pass"}]`))
	h.Release()
	s.verify = contract.VerifyResult{Missing: []contract.VersionRef{{Key: "k", VersionID: "v"}}}
	if _, err := verifyEntry(d, bg, callOf2("verify")); err != nil {
		t.Fatal(err)
	}
	if raw, _ := d.State.Get(bg, "snapshots"); strings.Contains(string(raw), `"refs_verified":true`) {
		t.Fatalf("欠けがあるのに確認済みにした: %s", raw)
	}
	s.verify = contract.VerifyResult{}
	if _, err := verifyEntry(d, bg, callOf2("verify")); err != nil {
		t.Fatal(err)
	}
	raw, _ := d.State.Get(bg, "snapshots")
	if !strings.Contains(string(raw), `"refs_verified":true`) || !strings.Contains(string(raw), `"health":"pass"`) {
		t.Errorf("snapshots: %s", raw)
	}
	items, _ := d.journal(bg)
	got := false
	for _, it := range items {
		got = got || it.Kind == "refs_verified"
	}
	if !got {
		t.Errorf("journal/ に refs_verified が無い")
	}
}

// verify が一覧を取った後に並行する backup が足した世代は、記録を失わず、確認済みにもしない。
func TestVerifyMarkKeepsConcurrentSnapshot(t *testing.T) {
	s := &stubRet{}
	d, _ := retEnv(t, s)
	f := newFlow(t)
	d.Recorder, d.State = f.d.Recorder, f.d.State
	refA, refB := "m-"+strings.Repeat("a", 32), "m-"+strings.Repeat("b", 32)
	h, _ := d.State.Lock(bg)
	_ = d.State.Update(bg, h, "snapshots", json.RawMessage(`[{"ref":"`+refA+`","version_id":"v1","stored_at":"2026-11-01T00:00:00Z","refs_verified":null}]`))
	h.Release()
	s.onVerify = func() {
		h, _ := d.State.Lock(bg)
		raw, _ := d.State.Get(bg, "snapshots")
		var l []json.RawMessage
		_ = json.Unmarshal(raw, &l)
		l = append(l, json.RawMessage(`{"ref":"`+refB+`","version_id":"v2","stored_at":"2026-11-01T01:00:00Z","refs_verified":null}`))
		nb, _ := json.Marshal(l)
		_ = d.State.Update(bg, h, "snapshots", nb)
		h.Release()
	}
	if _, err := verifyEntry(d, bg, callOf2("verify")); err != nil {
		t.Fatal(err)
	}
	raw, _ := d.State.Get(bg, "snapshots")
	var got []struct {
		Ref string `json:"ref"`
		RV  *bool  `json:"refs_verified"`
	}
	_ = json.Unmarshal(raw, &got)
	if len(got) != 2 || got[0].Ref != refA || got[0].RV == nil || !*got[0].RV {
		t.Fatalf("先の世代は確認済み、後の世代の記録は残る: %s", raw)
	}
	if got[1].Ref != refB || got[1].RV != nil {
		t.Errorf("一覧の後の世代を確認済みにした: %s", raw)
	}
}
