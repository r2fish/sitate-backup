package fake

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type testClock struct{ t time.Time }

func (c *testClock) Now() (time.Time, error) { return c.t, nil }

func newStore(t *testing.T, role Role) (*ObjectStore, *testClock) {
	t.Helper()
	clk := &testClock{t: time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)}
	core := NewCore(clk)
	if err := core.CreateBucket("b", true); err != nil {
		t.Fatal(err)
	}
	return &ObjectStore{Core: core, Bucket: "b", Role: role, Params: contract.FixedRetention{}}, clk
}

func TestObjectLockAndClock(t *testing.T) {
	s, clk := newStore(t, RoleAdmin)
	ctx := context.Background()
	r, err := s.Put(ctx, "k", bytes.NewReader([]byte("x")), contract.RetentionDaily)
	if err != nil {
		t.Fatal(err)
	}
	if want := clk.t.AddDate(0, 0, contract.RetentionDailyDays); !r.RetainUntil.Equal(want) {
		t.Errorf("retain-until %v want %v", r.RetainUntil, want)
	}
	if err := s.DeleteVersion(ctx, "k", r.VersionID); err == nil {
		t.Fatal("保持期間内の削除が通った")
	}
	if err := s.Extend(ctx, "k", r.VersionID, clk.t.Add(time.Hour)); err == nil {
		t.Error("短縮が通った")
	}
	if err := s.Extend(ctx, "k", r.VersionID, r.RetainUntil.Add(24*time.Hour)); err != nil {
		t.Errorf("延長: %v", err)
	}
	clk.t = clk.t.AddDate(0, 0, 200) // 制御できる時計を進める
	if err := s.DeleteVersion(ctx, "k", r.VersionID); err != nil {
		t.Errorf("期限後の削除: %v", err)
	}
	if _, err := s.Get(ctx, "k", r.VersionID); err != contract.ErrMissing {
		t.Errorf("削除後の Get: %v", err)
	}
}

// 書き込み鍵は保持中の版を Object Lock で拒否され、期限後は版指定で削除できる(prune が使う。spec 3.2.6)。読み取り鍵は常に拒否。
func TestWriteKeyDeleteVersionRespectsLock(t *testing.T) {
	s, clk := newStore(t, RoleWrite)
	ctx := context.Background()
	r, _ := s.Put(ctx, "kk", bytes.NewReader([]byte("x")), contract.RetentionDaily)
	err := s.DeleteVersion(ctx, "kk", r.VersionID)
	if err == nil || !strings.Contains(err.Error(), "WORM") {
		t.Errorf("保持中の版の削除は Object Lock で拒否される: %v", err)
	}
	clk.t = clk.t.AddDate(0, 0, 200)
	if err := s.DeleteVersion(ctx, "kk", r.VersionID); err != nil {
		t.Errorf("期限後の削除は書き込み鍵でも通る: %v", err)
	}
	rd := &ObjectStore{Core: s.Core, Bucket: "b", Role: RoleRead, Params: contract.FixedRetention{}}
	r2, _ := s.Put(ctx, "kk2", bytes.NewReader([]byte("x")), contract.RetentionDaily)
	clk.t = clk.t.AddDate(0, 0, 200)
	if err := rd.DeleteVersion(ctx, "kk2", r2.VersionID); err == nil {
		t.Error("読み取り鍵が版指定の削除を通した")
	}
}

func TestWriteKeyUnversionedDelete(t *testing.T) {
	s, _ := newStore(t, RoleWrite)
	ctx := context.Background()
	r, _ := s.Put(ctx, "k", bytes.NewReader([]byte("x")), contract.RetentionDaily)
	if _, err := s.Core.Delete("b", "k", "", RoleWrite); err == nil {
		t.Error("既定では書き込み鍵の版指定なしの削除は拒否される")
	}
	s.Core.AllowUnversionedDelete = true
	if _, err := s.Core.Delete("b", "k", "", RoleWrite); err != nil {
		t.Errorf("KEY_ALLOWS_UNVERSIONED_DELETE の設定: %v", err)
	}
	if v, _ := s.ListVersions(ctx, "k"); len(v) != 2 || !v[0].DeleteMarker {
		t.Errorf("削除マーカー: %v", v)
	}
	if _, err := s.Get(ctx, "k", r.VersionID); err != nil {
		t.Errorf("読み出しは削除マーカーを無視して版指定で読める: %v", err)
	}
}

func TestSimTimeMissingOrBroken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "none")
	clk := FuncClock(func() (time.Time, error) {
		st, err := contract.ReadSimTime(path)
		if err != nil {
			return time.Time{}, err
		}
		return st.Now(time.Now())
	})
	core := NewCore(clk)
	if err := core.Healthy(); err == nil {
		t.Fatal("共有の値が無いのに内部エラーにならない")
	}
	s := &ObjectStore{Core: core, Bucket: "b"}
	if _, err := s.ListVersions(context.Background(), ""); err == nil {
		t.Error("ObjectStore が共有の値の無いときに通った")
	}
	if e, ok := core.Healthy().(*S3Error); !ok || e.Status != 500 {
		t.Errorf("5xx 相当でない: %v", core.Healthy())
	}
}

// StoreTime は実時刻(応答の Date)で、模擬時間(Object Lock の判定の時計)に引きずられない。
func TestStoreTimeIsRealTime(t *testing.T) {
	s, clk := newStore(t, RoleAdmin)
	clk.t = clk.t.AddDate(5, 0, 0)
	got, err := s.StoreTime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(got); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("StoreTime = %s は実時刻でない", got)
	}
}

// 読み取り鍵は Go パッケージの Put でも書けない(サーバーと同じ意味論)。
func TestReadKeyCannotPut(t *testing.T) {
	s, _ := newStore(t, RoleRead)
	if _, err := s.Put(context.Background(), "k", bytes.NewReader([]byte("x")), contract.RetentionDaily); err == nil {
		t.Error("読み取り鍵が Put できた")
	}
}

// 月次の retain-until は UTC の暦の月加算(同じ日が無ければ月末に丸める。spec 4.2)。
func TestMonthlyRetainUntilClamped(t *testing.T) {
	s, clk := newStore(t, RoleWrite)
	clk.t = time.Date(2028, 2, 29, 3, 0, 0, 0, time.UTC)
	r, err := s.Put(context.Background(), "m", bytes.NewReader([]byte("x")), contract.RetentionMonthly)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2029, 2, 28, 3, 0, 0, 0, time.UTC); !r.RetainUntil.Equal(want) {
		t.Errorf("retain-until = %s, want %s", r.RetainUntil, want)
	}
}

// 偽のアダプタの台本の ErrCode は ADAPTER_ERROR の detail.adapter_code に出る。
func TestAdapterScriptErrCode(t *testing.T) {
	a := NewAdapterRunner()
	a.Script("notify", Step{Outcome: OutcomeError, ErrCode: "WEBHOOK_REJECTED"})
	_, err := a.Run(context.Background(), contract.RunContext{}, "notify", "webhook", nil, nil)
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodeAdapterError || !strings.Contains(string(ce.Detail), "WEBHOOK_REJECTED") {
		t.Fatalf("%v", err)
	}
	if e := contract.Validate("cli/detail.json#/$defs/ADAPTER_ERROR", ce.Detail); e != nil {
		t.Errorf("detail がスキーマに合わない: %v", e)
	}
}
