package cli

// verify・extend・prune の入口(spec 3.1.2。作業指示書 5.18)。保持の判断は store(U1b)が持ち、cli は結果の形・通知・警告への写しだけを持つ。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

type stubRet struct {
	*fake.Retention
	verify  contract.VerifyResult
	extend  map[string]contract.ExtendResult
	extErr  error
	prune   contract.PruneResult
	pruneEr error
	calls   []string
	refs    []contract.VersionRef
}

func (s *stubRet) Verify(_ context.Context, r []contract.VersionRef) (contract.VerifyResult, error) {
	s.calls, s.refs = append(s.calls, "verify"), r
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
