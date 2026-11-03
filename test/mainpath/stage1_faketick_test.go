//go:build mainpath

package mainpath

// 作業指示書 5.18 の完了条件 6 の後半: 偽の保管先のサーバー(make test-env-up NAME=<WP> KIND=fake)を起動したまま、試験ビルドの核の
// test-tick --days 3 を回すと、保持を 1 日に短くして書いた版が、結線した実物の Retention.Prune で消える(サーバーを再起動しない)。
// 偽のサーバーと試験の側の時計は、test-tick が書き換える共有の値(SITATE_SIM_TIME_FILE)を読むたびに読む。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/s3test"
)

// simFile は共有の値を読むたびに読む時計とオフセット。
type simFile string

func (p simFile) Offset() (time.Duration, error) {
	s, err := contract.ReadSimTime(string(p))
	if err != nil {
		return 0, err
	}
	return s.Offset()
}

func TestStage1FakeTickPrune(t *testing.T) {
	e := s3envRaw(t)
	path := os.Getenv(contract.EnvSimTimeFile)
	if os.Getenv(contract.EnvTestS3Kind) != "fake" || path == "" {
		t.Skip("偽の保管先のサーバーが無い(make test-env-up NAME=<WP> KIND=fake の後に NAME=<WP> を付けて実行)")
	}
	prepareFakeBucket(t, e)
	now := time.Now()
	if err := contract.WriteSimTime(path, now, now); err != nil { // 共有の値を今に置く
		t.Fatal(err)
	}
	w := newWorldOn(t, e)
	off := simFile(path)
	w.put.Offset, w.ret.Offset = off, off
	w.ret.Clock = simClock{off}
	w.put.Clock = simClock{off}
	g1 := w.backup("")
	if err := os.WriteFile(filepath.Join(w.src, "added-for-gen2.txt"), []byte("gen2 "+time.Now().String()), 0o644); err != nil {
		t.Fatal(err)
	}
	g2 := w.backup(g1.MID)
	before, err := w.put.ListVersions(w.ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	pr0, err := w.ret.Prune(w.ctx)
	if err != nil || pr0.Deleted != 0 {
		t.Fatalf("時間を進める前の Prune: %+v %v", pr0, err)
	}
	tickDays3(t, e2eRoot())
	pr, err := w.ret.Prune(w.ctx)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	after, _ := w.put.ListVersions(w.ctx, "")
	t.Logf("Prune: %+v、版 %d → %d(月次の世代 %s、日次の世代 %s)", pr, len(before), len(after), g1.MID, g2.MID)
	if pr.Deleted == 0 || len(after) >= len(before) {
		t.Errorf("3 日進めた後、保持 1 日の版が消えていない: %+v、版 %d → %d", pr, len(before), len(after))
	}
	have := map[string]bool{}
	for _, v := range after {
		have[v.Key] = true
	}
	// 2 世代目は日次(保持 1 日)で消え、1 世代目はその月の最初の健全な世代 = 月次(12 か月 + 7 日)で残る。
	if have["manifests/"+g2.MID] {
		t.Errorf("保持 1 日の日次の世代のマニフェストが残っている")
	}
	if !have["manifests/"+g1.MID] {
		t.Errorf("月次の世代のマニフェストが消えた")
	}
	if raw, _ := w.st.Get(w.ctx, "snapshots"); strings.Contains(string(raw), g2.MID) || !strings.Contains(string(raw), g1.MID) {
		t.Errorf("消した世代を記録から外していない: %s", raw)
	}
}

// tickDays3 は試験ビルドの核の test-tick --days 3 を回す(共有の値を 3 日進める。サーバーは再起動しない)。
func tickDays3(t *testing.T, root string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "sitate")
	build := exec.Command("go", "build", "-tags", "sitate_testbuild", "-o", bin, "./cmd/sitate")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("試験ビルド: %v\n%s", err, out)
	}
	tick := exec.Command(bin, "test-tick", "--days", "3")
	tick.Env = append(os.Environ(), "HOME="+t.TempDir())
	out, err := tick.CombinedOutput()
	if err != nil {
		t.Fatalf("test-tick --days 3: %v\n%s", err, out)
	}
	t.Logf("test-tick --days 3: %s", out)
}

type simClock struct{ off simFile }

func (c simClock) Now(context.Context) (time.Time, error) {
	d, err := c.off.Offset()
	return time.Now().UTC().Add(d), err
}

func prepareFakeBucket(t *testing.T, e envS3) {
	t.Helper()
	ctx := context.Background()
	c := s3test.NewClient(e.ep, os.Getenv(contract.EnvTestS3AccessKey), os.Getenv(contract.EnvTestS3SecretKey), nil)
	if _, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(e.bucket), ObjectLockEnabledForBucket: aws.Bool(true)}); err != nil && s3test.ErrCode(err) != "BucketAlreadyOwnedByYou" {
		t.Fatal(err)
	}
	if _, err := c.PutObjectLockConfiguration(ctx, &s3.PutObjectLockConfigurationInput{Bucket: &e.bucket, ObjectLockConfiguration: &types.ObjectLockConfiguration{
		ObjectLockEnabled: types.ObjectLockEnabledEnabled,
		Rule:              &types.ObjectLockRule{DefaultRetention: &types.DefaultRetention{Mode: types.ObjectLockRetentionModeCompliance, Days: aws.Int32(1)}}}}); err != nil {
		t.Fatal(err)
	}
}
