package store

// 契約試験(作業指示書 3.2): 実物の ObjectStore を偽の保管先のサーバー(fake/s3server)に、Retention を偽の保管先の Go パッケージ(制御できる時計)に通す。
// 骨格は NOT_IMPLEMENTED で落ち、nil を返すだけの実装は値の比較で落ち、仕様どおりの実装は panic しない。WP は骨格を実装し、この試験は変えない。
// 試験用の MinIO に対する同じ判定は U1b の追加の試験(5.3 の完了条件 1)が持つ。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/fake/s3server"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

var ctx = context.Background()

type nowClock struct{}

func (nowClock) Now() (time.Time, error) { return time.Now().UTC(), nil }

// serverStore は実時刻の保管先(Date も Object Lock の判定も実時刻)に書き込み鍵でつながった ObjectStore を返す。
func serverStore(t *testing.T) *ObjectStore {
	t.Helper()
	s, _ := serverStoreCore(t)
	return s
}

func serverStoreCore(t *testing.T) (*ObjectStore, *fake.Core) {
	t.Helper()
	core := fake.NewCore(nowClock{})
	if err := core.CreateBucket("b", true); err != nil {
		t.Fatal(err)
	}
	srv := s3server.New(core, "ADMINKEY", "adminsecret")
	srv.Keys["WRITEKEY"] = s3server.Key{Secret: "writesecret", Role: "write"}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return &ObjectStore{Endpoint: ts.URL, Bucket: "b", AccessKey: "WRITEKEY", SecretKey: "writesecret", Params: contract.FixedRetention{}}, core
}

func near(t *testing.T, what string, got, want time.Time) {
	t.Helper()
	if d := got.Sub(want); d < -time.Minute || d > time.Minute {
		t.Errorf("%s = %s は %s の 1 分以内でない", what, got, want)
	}
}

// 保持期限は応答の Date + 定数を呼び出し側が渡さずに付ける(日次 90 日、月次 12 か月・月末丸め)。
func TestContractObjectStorePut(t *testing.T) {
	s := serverStore(t)
	r, err := s.Put(ctx, "chunks/a", strings.NewReader("hello"), contract.RetentionDaily)
	harness.Implemented(t, err)
	if err != nil || r.VersionID == "" || r.Size != 5 || r.ETag == "" {
		t.Fatalf("Put: %+v %v", r, err)
	}
	near(t, "日次の retain-until", r.RetainUntil, time.Now().UTC().AddDate(0, 0, contract.RetentionDailyDays))
	m, err := s.Put(ctx, "manifests/m", strings.NewReader("m"), contract.RetentionMonthly)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "月次の retain-until", m.RetainUntil, contract.AddMonthsUTC(time.Now().UTC(), contract.RetentionMonthlyMonths))
}

func TestContractObjectStoreGetList(t *testing.T) {
	s := serverStore(t)
	r, err := s.Put(ctx, "chunks/a", strings.NewReader("hello"), contract.RetentionDaily)
	harness.Implemented(t, err)
	rc, err := s.Get(ctx, "chunks/a", r.VersionID)
	if err != nil || rc == nil {
		t.Fatalf("Get: %v %v", rc, err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(b, []byte("hello")) {
		t.Errorf("Get: %q", b)
	}
	if _, err := s.Get(ctx, "chunks/a", "no-such-version"); !errors.Is(err, contract.ErrMissing) {
		t.Errorf("無い版は ErrMissing: %v", err)
	}
	v, err := s.ListVersions(ctx, "chunks/")
	if err != nil || len(v) != 1 || v[0].Key != "chunks/a" || v[0].VersionID != r.VersionID || v[0].Size != 5 || !v[0].IsLatest {
		t.Errorf("ListVersions: %+v %v", v, err)
	}
}

// 保持中の版の削除と保持の短縮は拒否され、延長は通る。
func TestContractObjectStoreLock(t *testing.T) {
	s := serverStore(t)
	r, err := s.Put(ctx, "chunks/a", strings.NewReader("x"), contract.RetentionDaily)
	harness.Implemented(t, err)
	if err := s.DeleteVersion(ctx, "chunks/a", r.VersionID); err == nil {
		t.Error("保持中の版の削除が通った")
	}
	if err := s.Extend(ctx, "chunks/a", r.VersionID, r.RetainUntil.Add(-24*time.Hour)); err == nil {
		t.Error("保持の短縮が通った")
	}
	if err := s.Extend(ctx, "chunks/a", r.VersionID, r.RetainUntil.Add(24*time.Hour)); err != nil {
		t.Errorf("延長: %v", err)
	}
}

func TestContractObjectStoreBucketConfigStoreTime(t *testing.T) {
	s := serverStore(t)
	cfg, err := s.BucketConfig(ctx)
	harness.Implemented(t, err)
	if err != nil || !cfg.Versioning || cfg.LockMode != "COMPLIANCE" {
		t.Errorf("BucketConfig: %+v %v", cfg, err)
	}
	now, err := s.StoreTime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "StoreTime(直前の応答の Date)", now, time.Now().UTC())
}

// ---- Retention(偽の保管先の Go パッケージ。時計を進める) ----

func newRetention(t *testing.T) (*Retention, *fake.ObjectStore, *fake.ManualClock) {
	t.Helper()
	st, clk, _ := fake.NewTestStore(fake.RoleWrite)
	return &Retention{Store: st, State: &fake.StateFiles{}, Codec: &harness.RefCodec{}, KeyS: fake.KeyService{}, Clock: &fake.Clock{T: fake.StartTime}}, st, clk
}

func putRef(t *testing.T, st *fake.ObjectStore, key string) contract.VersionRef {
	t.Helper()
	r, err := st.Put(ctx, key, strings.NewReader("data-"+key), contract.RetentionDaily)
	if err != nil {
		t.Fatal(err)
	}
	return contract.VersionRef{Key: key, VersionID: r.VersionID, Size: r.Size, ETag: r.ETag, RetainUntil: r.RetainUntil}
}

func TestContractRetentionVerify(t *testing.T) {
	r, st, _ := newRetention(t)
	ok := putRef(t, st, "chunks/ok")
	short := putRef(t, st, "chunks/short")
	short.RetainUntil = short.RetainUntil.Add(10 * 24 * time.Hour) // 記録した値が実際より長い = 保管先の保持が短い
	gone := contract.VersionRef{Key: "chunks/gone", VersionID: "v-none"}
	_ = putRef(t, st, "chunks/unrecorded")
	res, err := r.Verify(ctx, []contract.VersionRef{ok, short, gone})
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Missing) != 1 || res.Missing[0].Key != "chunks/gone" {
		t.Errorf("Missing: %+v", res.Missing)
	}
	if len(res.RetentionShort) != 1 || res.RetentionShort[0].Key != "chunks/short" || !res.RetentionShort[0].Actual.Before(res.RetentionShort[0].Recorded) {
		t.Errorf("RetentionShort: %+v", res.RetentionShort)
	}
	if len(res.UnexpectedVersions) != 1 || res.UnexpectedVersions[0].Key != "chunks/unrecorded" {
		t.Errorf("UnexpectedVersions: %+v", res.UnexpectedVersions)
	}
	if res.BytesWrittenVsBucket <= 1.0 {
		t.Errorf("記録に無い版の分だけ保管先が多い(比 > 1): %v", res.BytesWrittenVsBucket)
	}
}

func TestContractRetentionMissingRefs(t *testing.T) {
	r, st, _ := newRetention(t)
	ok := putRef(t, st, "chunks/ok")
	gone := contract.VersionRef{Key: "chunks/gone", VersionID: "v-none"}
	m, err := r.MissingRefs(ctx, []contract.VersionRef{ok, gone})
	harness.Implemented(t, err)
	if err != nil || len(m) != 1 || m[0].Key != "chunks/gone" {
		t.Errorf("MissingRefs: %+v %v", m, err)
	}
}

func TestContractRetentionExtendTarget(t *testing.T) {
	r, _, _ := newRetention(t)
	_, err := r.Extend(ctx, "C")
	harness.Implemented(t, err)
	if err == nil {
		t.Error("目標 A・B 以外の延長が成功した")
	}
}

// 取り消された文脈を受けた実行は保管先へ書かずに RUN_ABORTED(spec 3.1.1 の規則 11。Lease の偽物は使わない)。
func TestContractRetentionAborted(t *testing.T) {
	r, st, clk := newRetention(t)
	ref := putRef(t, st, "chunks/a")
	clk.Advance(200 * 24 * time.Hour) // 期限切れ(取り消されていなければ prune が消せる)
	c, cancel := context.WithCancel(ctx)
	cancel()
	_, err := r.Prune(c)
	harness.ExpectCode(t, err, contract.CodeRunAborted)
	_, err = r.Extend(c, "A")
	harness.ExpectCode(t, err, contract.CodeRunAborted)
	if v, _ := st.ListVersions(ctx, "chunks/"); len(v) != 1 || v[0].VersionID != ref.VersionID {
		t.Errorf("取り消された実行が保管先を変えた: %+v", v)
	}
}

func TestContractRetentionHoldSet(t *testing.T) {
	r, _, _, sf := newRetentionState(t, nil)
	code := contract.CodeRestoreMismatch
	t1, err := r.HoldSet(ctx, contract.HoldInput{Reason: "restore_failed", Code: &code, Refs: []string{"m-44444444444444444444444444444444"}})
	harness.Implemented(t, err)
	if err != nil || t1.IsZero() {
		t.Fatalf("since: %v %v", t1, err)
	}
	raw, _ := sf.Get(ctx, "hold")
	if verr := contract.Validate("data/state.json#/$defs/hold", raw); verr != nil {
		t.Fatalf("state.json の hold に合わない: %v\n%s", verr, raw)
	}
	var h struct {
		Active bool       `json:"active"`
		Since  *time.Time `json:"since"`
		Auto   bool       `json:"auto"`
		Reason string     `json:"reason"`
		Causes []struct {
			Reason string   `json:"reason"`
			Code   string   `json:"code"`
			Refs   []string `json:"refs"`
		} `json:"causes"`
	}
	_ = json.Unmarshal(raw, &h)
	if !h.Active || h.Since == nil || !h.Since.Equal(t1) || h.Reason != "restore_failed" || !h.Auto || len(h.Causes) != 1 || h.Causes[0].Code != string(code) || len(h.Causes[0].Refs) != 1 {
		t.Errorf("hold の中身: %s", raw)
	}
	// 停止中に別の原因が当たったら since を変えず causes[] に足す。手動の停止は auto でない原因として足る。
	t2, err := r.HoldSet(ctx, contract.HoldInput{Reason: "manual"})
	if err != nil || !t2.Equal(t1) {
		t.Errorf("繰り返しで since が変わった: %v %v %v", t1, t2, err)
	}
	raw, _ = sf.Get(ctx, "hold")
	h.Causes = nil
	_ = json.Unmarshal(raw, &h)
	if len(h.Causes) != 2 || h.Causes[1].Reason != "manual" || h.Reason != "restore_failed" {
		t.Errorf("原因が足されていない: %s", raw)
	}
	if _, err := r.HoldSet(ctx, contract.HoldInput{Reason: "no_such_reason"}); err == nil {
		t.Error("理由の列挙に無い値が成功した")
	}
}

// 保持区分: 現地(timezone)の暦月の最初の健全な世代が monthly、同じ月の 2 つ目や不健全な世代は daily、月をまたぐ実行は created_at の月に数える。
func TestContractRetentionClass(t *testing.T) {
	r, _, _ := newRetention(t)
	tokyo := "Asia/Tokyo"
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	class := func(in contract.ClassInput) contract.RetentionClass {
		t.Helper()
		c, err := r.Class(in)
		harness.Implemented(t, err)
		if err != nil {
			t.Fatalf("Class: %v", err)
		}
		return c
	}
	prev := []contract.SnapshotClassRef{{CreatedAt: at("2026-10-02T03:00:00Z"), RetentionClass: contract.RetentionMonthly}}
	// 月の最初の健全な世代は monthly。
	if c := class(contract.ClassInput{CreatedAt: at("2026-11-02T03:00:00Z"), Healthy: true, Timezone: tokyo, Snapshots: prev}); c != contract.RetentionMonthly {
		t.Errorf("11 月の最初の健全な世代: %q", c)
	}
	// 同じ月に monthly があれば daily。
	same := append(prev, contract.SnapshotClassRef{CreatedAt: at("2026-11-02T03:00:00Z"), RetentionClass: contract.RetentionMonthly})
	if c := class(contract.ClassInput{CreatedAt: at("2026-11-03T03:00:00Z"), Healthy: true, Timezone: tokyo, Snapshots: same}); c != contract.RetentionDaily {
		t.Errorf("同じ月の 2 つ目: %q", c)
	}
	// 不健全な世代は daily で、月次を占めない(次の健全な世代が monthly)。
	if c := class(contract.ClassInput{CreatedAt: at("2026-11-02T03:00:00Z"), Healthy: false, Timezone: tokyo, Snapshots: prev}); c != contract.RetentionDaily {
		t.Errorf("不健全な世代: %q", c)
	}
	// 暦月は timezone の現地: UTC の 10-31T16:00 は東京の 11-01 01:00(11 月)。UTC の月なら 10 月で、10 月に monthly があるので daily になる。
	if c := class(contract.ClassInput{CreatedAt: at("2026-10-31T16:00:00Z"), Healthy: true, Timezone: tokyo, Snapshots: prev}); c != contract.RetentionMonthly {
		t.Errorf("現地の暦月で数える: %q", c)
	}
	if c := class(contract.ClassInput{CreatedAt: at("2026-10-31T16:00:00Z"), Healthy: true, Timezone: "UTC", Snapshots: prev}); c != contract.RetentionDaily {
		t.Errorf("UTC では 10 月: %q", c)
	}
	if _, err := r.Class(contract.ClassInput{CreatedAt: at("2026-11-02T03:00:00Z"), Healthy: true, Timezone: "No/Such_Zone"}); err == nil {
		t.Error("読めない timezone が成功した")
	}
}

func TestContractStoreProbe(t *testing.T) {
	adm, _, core := fake.NewTestStore(fake.RoleAdmin)
	wr := &fake.ObjectStore{Core: core, Bucket: "b", Role: fake.RoleWrite, Params: contract.FixedRetention{}}
	p := &StoreProbe{Keys: map[string]contract.ObjectStore{"admin": adm, "write": wr}}
	v, err := p.PutProbe(ctx, "probe/x", []byte("p"))
	harness.Implemented(t, err)
	if err != nil || v == "" {
		t.Fatalf("PutProbe: %q %v", v, err)
	}
	if ok, err := p.Try(ctx, "write", "put"); err != nil || !ok {
		t.Errorf("書き込み鍵の put は許される: %v %v", ok, err)
	}
	if ok, err := p.Try(ctx, "write", "extend_retention"); err != nil || !ok {
		t.Errorf("書き込み鍵の延長は許される: %v %v", ok, err)
	}
	if ok, err := p.Try(ctx, "write", "retention_shorten"); err != nil || ok {
		t.Errorf("書き込み鍵の短縮は拒否される: %v %v", ok, err)
	}
}

// `<p>`(プレフィクス)は ObjectStore の実物だけが付け、一覧は外して返す(contract/storage.go の注記)。
// 呼び出し側は `<p>` を除いた名前を渡す。付け忘れ(素のキーに書く)も二重(`sitate/sitate/`)も落ちる。
func TestContractObjectStorePrefixOwner(t *testing.T) {
	s, core := serverStoreCore(t)
	s.Prefix = "sitate/"
	r, err := s.Put(ctx, "repo.json", strings.NewReader("{}"), contract.RetentionDaily)
	harness.Implemented(t, err)
	if err != nil {
		return
	}
	raw, err := core.List("b", "")
	if err != nil || len(raw) != 1 || raw[0].Key != "sitate/repo.json" {
		t.Fatalf("保管先の実際のキーは <p> 付きの sitate/repo.json 1 件のはず: %+v %v", raw, err)
	}
	if rc, err := s.Get(ctx, "repo.json", r.VersionID); err != nil {
		t.Errorf("素の名前で読み出せない: %v", err)
	} else {
		rc.Close()
	}
	v, err := s.ListVersions(ctx, "")
	if err != nil || len(v) != 1 || v[0].Key != "repo.json" {
		t.Errorf("一覧の Key は <p> を外した repo.json: %+v %v", v, err)
	}
	// <p> の外のキーは一覧に出ない。
	if v, _ := s.ListVersions(ctx, "keyring/"); len(v) != 0 {
		t.Errorf("無い接頭辞の一覧が空でない: %+v", v)
	}
}
