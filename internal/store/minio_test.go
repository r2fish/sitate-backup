package store

// 試験用の MinIO(make test-env-up NAME=<WP>。Object Lock の線上の挙動)に対する判定(作業指示書 5.3 の完了条件 1)。
// 接続先と資格情報は contract.EnvTestS3* の環境変数で受ける。無ければ飛ばす。MinIO の時計は動かさない。保持は 1 日の最小値で付け、
// 削除・短縮の拒否と記録の無い版・削除マーカーの検出を確かめる。

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
	"github.com/r2fish/sitate-backup/internal/contract/s3test"
)

// oneDay は MinIO の試験用の保持(日次 1 日。月次は定数のまま)。
type oneDay struct{}

func (oneDay) DailyDays() int     { return 1 }
func (oneDay) MonthlyMonths() int { return contract.RetentionMonthlyMonths }

type minio struct {
	S     *ObjectStore
	Admin *s3.Client
	Bkt   string
}

func newMinIO(t *testing.T) *minio {
	t.Helper()
	ep, wk := os.Getenv(contract.EnvTestS3Endpoint), os.Getenv(contract.EnvTestS3WriteAccessKey)
	if ep == "" || wk == "" {
		t.Skip("試験用の保管先の環境変数が無い(make test-env-up NAME=<WP> の後に NAME=<WP> を付けて実行)")
	}
	if os.Getenv(contract.EnvTestS3Kind) == "fake" {
		t.Skip("MinIO に対する試験(KIND=fake では飛ばす)")
	}
	bkt := os.Getenv(contract.EnvTestS3Bucket)
	s := &ObjectStore{Endpoint: ep, Bucket: bkt, AccessKey: wk, SecretKey: os.Getenv(contract.EnvTestS3WriteSecretKey),
		Prefix: fmt.Sprintf("t%d/", time.Now().UnixNano()), Params: oneDay{}}
	return &minio{S: s, Admin: s3test.NewClient(ep, os.Getenv(contract.EnvTestS3AccessKey), os.Getenv(contract.EnvTestS3SecretKey), nil), Bkt: bkt}
}

func (m *minio) retention(t *testing.T) *Retention {
	return &Retention{Store: m.S, State: &fake.StateFiles{}, Codec: &harness.RefCodec{}, KeyS: fake.KeyService{}, Clock: &fake.Clock{T: time.Now().UTC()}}
}

// 保持期限つきの書き込み(retain-until の明示と Content-MD5)、保持中の版の削除と短縮の拒否、延長、版指定の読み出し。
func TestMinIOPutLockExtend(t *testing.T) {
	m := newMinIO(t)
	r, err := m.S.Put(ctx, "chunks/a", strings.NewReader("hello"), contract.RetentionDaily)
	harness.Implemented(t, err)
	if err != nil || r.VersionID == "" || r.Size != 5 {
		t.Fatalf("Put: %+v %v", r, err)
	}
	until, err := m.S.RetainUntil(ctx, "chunks/a", r.VersionID)
	if err != nil || until.Sub(r.RetainUntil).Abs() > 2*time.Second {
		t.Errorf("明示した retain-until が採られていない: 保管先 %v 応答 %v %v", until, r.RetainUntil, err)
	}
	now, _ := m.S.StoreTime(ctx)
	if d := r.RetainUntil.Sub(now.Add(24 * time.Hour)); d.Abs() > 3*time.Second {
		t.Errorf("retain-until は応答の Date + 1 日: %v", r.RetainUntil)
	}
	harness.ExpectCode(t, m.S.DeleteVersion(ctx, "chunks/a", r.VersionID), contract.CodeStorageDenied)
	harness.ExpectCode(t, m.S.Extend(ctx, "chunks/a", r.VersionID, r.RetainUntil.Add(-time.Hour)), contract.CodeStorageDenied)
	longer := r.RetainUntil.Add(48 * time.Hour)
	if err := m.S.Extend(ctx, "chunks/a", r.VersionID, longer); err != nil {
		t.Fatalf("延長: %v", err)
	}
	if u, _ := m.S.RetainUntil(ctx, "chunks/a", r.VersionID); u.Sub(longer).Abs() > 2*time.Second {
		t.Errorf("延長後の retain-until = %v 期待 %v", u, longer)
	}
	rc, err := m.S.Get(ctx, "chunks/a", r.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "hello" {
		t.Errorf("Get: %q", b)
	}
	if _, err := m.S.Get(ctx, "chunks/a", "no-such-version"); err == nil {
		t.Error("無い版の読み出しが成功した")
	}
	cfg, err := m.S.BucketConfig(ctx)
	if err != nil || !cfg.Versioning || cfg.LockMode == "" {
		t.Errorf("BucketConfig: %+v %v", cfg, err)
	}
}

// 記録の無い版は UNEXPECTED_VERSION の対象(同じキーの別の版)、削除マーカー(版指定なしの削除)は数に出る。retention_short は記録より短い保持。
func TestMinIOVerifyUnexpectedAndDeleteMarker(t *testing.T) {
	m := newMinIO(t)
	rt := m.retention(t)
	a, err := m.S.Put(ctx, "chunks/a", strings.NewReader("one"), contract.RetentionDaily)
	if err != nil {
		t.Fatal(err)
	}
	rogue, err := m.S.Put(ctx, "chunks/a", strings.NewReader("garbage"), contract.RetentionDaily) // 記録の無い版(書き込み鍵だけを持つ者が積んだ版)
	if err != nil {
		t.Fatal(err)
	}
	short, err := m.S.Put(ctx, "chunks/s", strings.NewReader("s"), contract.RetentionDaily)
	if err != nil {
		t.Fatal(err)
	}
	recA := contract.VersionRef{Key: "chunks/a", VersionID: a.VersionID, Size: a.Size, RetainUntil: a.RetainUntil}
	recS := contract.VersionRef{Key: "chunks/s", VersionID: short.VersionID, Size: short.Size, RetainUntil: short.RetainUntil.Add(10 * 24 * time.Hour)}
	if _, err := m.Admin.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(m.Bkt), Key: aws.String(m.S.Prefix + "chunks/a")}); err != nil {
		t.Fatalf("管理用鍵の版指定なしの削除: %v", err)
	}
	res, err := rt.Verify(ctx, []contract.VersionRef{recA, recS})
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.UnexpectedVersions) != 1 || res.UnexpectedVersions[0].VersionID != rogue.VersionID {
		t.Errorf("記録の無い版: %+v", res.UnexpectedVersions)
	}
	if res.DeleteMarkers != 1 {
		t.Errorf("削除マーカーの数 = %d", res.DeleteMarkers)
	}
	if len(res.Missing) != 0 {
		t.Errorf("削除マーカーが付いても記録した版は在る(読み出しは版指定): %+v", res.Missing)
	}
	if len(res.RetentionShort) != 1 || res.RetentionShort[0].Key != "chunks/s" {
		t.Errorf("記録より短い retain-until: %+v", res.RetentionShort)
	}
	if res.NoncurrentVersions < 2 || res.BytesWrittenVsBucket <= 1 {
		t.Errorf("非現行の版 %d 比 %v", res.NoncurrentVersions, res.BytesWrittenVsBucket)
	}
}

// 目標 B の延長は保管先の MinIO に対しても今 + 90 日にし、短縮しない。
func TestMinIOExtendTargetB(t *testing.T) {
	m := newMinIO(t)
	rt := m.retention(t)
	r, err := m.S.Put(ctx, "keyring/kr-0123456789abcdef0123456789abcdef", strings.NewReader("k"), contract.RetentionDaily)
	if err != nil {
		t.Fatal(err)
	}
	now, _ := m.S.StoreTime(ctx)
	rec := map[string]any{"key": "keyring/kr-0123456789abcdef0123456789abcdef", "version_id": r.VersionID, "size": r.Size, "etag": r.ETag,
		"retain_until": r.RetainUntil.Format(time.RFC3339), "written_at": now.Format(time.RFC3339)}
	rt.State = &fake.StateFiles{Items: map[string]json.RawMessage{}}
	setItem(rt.State.(*fake.StateFiles), "objects", []any{rec})
	res, err := rt.Extend(ctx, "B")
	harness.Implemented(t, err)
	if err != nil || res.Extended != 1 || len(res.Failed) != 0 {
		t.Fatalf("Extend B: %+v %v", res, err)
	}
	u, _ := m.S.RetainUntil(ctx, "keyring/kr-0123456789abcdef0123456789abcdef", r.VersionID)
	if want := now.AddDate(0, 0, contract.RetentionDailyDays); u.Sub(want).Abs() > 5*time.Second {
		t.Errorf("共有物は今 + 90 日: %v 期待 %v", u, want)
	}
	res, err = rt.Extend(ctx, "B") // 2 回目は記録が目標以上なので要求を送らない
	if err != nil || res.Extended != 0 {
		t.Errorf("2 回目の延長: %+v %v", res, err)
	}
}

func (m *minio) storeAs(ak, sk string) *ObjectStore {
	return &ObjectStore{Endpoint: m.S.Endpoint, Bucket: m.S.Bucket, AccessKey: ak, SecretKey: sk, Prefix: m.S.Prefix, Params: m.S.Params}
}

// bucket check の試し方(H5): 書き込み鍵は put と読み取りが通り、短縮・版指定なしの削除・リーガルホールドは拒否される。読み取り鍵は put が拒否され、
// 保持の読み取りができなくても誤りでなく拒否(false)になる。管理用鍵は版指定なしの削除とリーガルホールドが通り(リーガルホールドは外して終える)、削除マーカーが残る。
func TestMinIOProbeTry(t *testing.T) {
	m := newMinIO(t)
	read := m.storeAs(os.Getenv(contract.EnvTestS3ReadAccessKey), os.Getenv(contract.EnvTestS3ReadSecretKey))
	admin := m.storeAs(os.Getenv(contract.EnvTestS3AccessKey), os.Getenv(contract.EnvTestS3SecretKey))
	_, err := m.Admin.PutObjectLockConfiguration(ctx, &s3.PutObjectLockConfigurationInput{Bucket: aws.String(m.Bkt), ObjectLockConfiguration: &types.ObjectLockConfiguration{
		ObjectLockEnabled: types.ObjectLockEnabledEnabled,
		Rule:              &types.ObjectLockRule{DefaultRetention: &types.DefaultRetention{Mode: types.ObjectLockRetentionModeCompliance, Days: aws.Int32(1)}}}}) // probe/ の既定保持(init-storage の設定に当たる)
	if err != nil {
		t.Fatal(err)
	}
	p := &StoreProbe{Keys: map[string]contract.ObjectStore{"write": m.S, "read": read, "admin": admin}}
	v, err := p.PutProbe(ctx, "probe/x", []byte("p"))
	harness.Implemented(t, err)
	if err != nil || v == "" {
		t.Fatalf("PutProbe: %q %v", v, err)
	}
	want := []struct {
		role, op string
		allowed  bool
	}{
		{"write", "put", true}, {"write", "list_versions", true}, {"write", "get_version", true}, {"write", "get_retention", true}, {"write", "get_bucket_config", true},
		{"write", "retention_shorten", false}, {"write", "unversioned_delete", false}, {"write", "legal_hold", false},
		{"read", "put", false}, {"read", "list_versions", true}, {"read", "get_version", true}, {"read", "retention_shorten", false},
		{"admin", "legal_hold", true}, {"admin", "unversioned_delete", true},
	}
	for _, w := range want {
		if got, err := p.Try(ctx, w.role, w.op); err != nil || got != w.allowed {
			t.Errorf("Try(%s, %s) = %v %v, 期待 %v", w.role, w.op, got, err, w.allowed)
		}
	}
	vs, _ := m.S.ListVersions(ctx, "probe/")
	markers := 0
	for _, x := range vs {
		if x.DeleteMarker {
			markers++
		}
	}
	if markers != 1 {
		t.Errorf("削除マーカー: %d", markers)
	}
}
