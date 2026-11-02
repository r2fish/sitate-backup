//go:build sitate_testbuild

package cli

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/s3test"
	"github.com/r2fish/sitate-backup/internal/state"
)

// 完了条件 11: 偽の保管先のサーバー(make test-env-up NAME=U1f-1 KIND=fake)を起動したまま test-tick --days 3 を回すと、
// 保持 1 日で書いた版の削除が、サーバーを再起動せずに、test-tick が書き換えた共有の値で進んだ後に通る。
// 版の書き込みと削除は試験の中から S3 の API で送る。接続先は環境変数(無ければ飛ばす)。
func TestTestTickAdvancesFakeStoreRetention(t *testing.T) {
	ep, path := os.Getenv(contract.EnvTestS3Endpoint), os.Getenv(contract.EnvSimTimeFile)
	if ep == "" || os.Getenv(contract.EnvTestS3Kind) != "fake" || path == "" {
		t.Skip("偽の保管先のサーバーが無い(make test-env-up NAME=U1f-1 KIND=fake の後に NAME=U1f-1 を付けて実行)")
	}
	ctx := context.Background()
	c := s3test.NewClient(ep, os.Getenv(contract.EnvTestS3AccessKey), os.Getenv(contract.EnvTestS3SecretKey), nil)
	bucket := os.Getenv(contract.EnvTestS3Bucket)
	if _, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket), ObjectLockEnabledForBucket: aws.Bool(true)}); err != nil && s3test.ErrCode(err) != "BucketAlreadyOwnedByYou" {
		t.Fatal(err)
	}
	if _, err := c.PutObjectLockConfiguration(ctx, &s3.PutObjectLockConfigurationInput{Bucket: &bucket, ObjectLockConfiguration: &types.ObjectLockConfiguration{
		ObjectLockEnabled: types.ObjectLockEnabledEnabled,
		Rule:              &types.ObjectLockRule{DefaultRetention: &types.DefaultRetention{Mode: types.ObjectLockRetentionModeCompliance, Days: aws.Int32(1)}}}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := contract.WriteSimTime(path, now, now); err != nil { // 共有の値を今に置く(前の回の進みを持ち越さない)
		t.Fatal(err)
	}
	key := aws.String(time.Now().Format("20060102T150405.000000000") + "/tick")
	put, err := c.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: key, Body: bytes.NewReader([]byte("x"))})
	if err != nil || put.VersionId == nil {
		t.Fatalf("PutObject: %v", err)
	}
	del := func() error {
		_, e := c.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: key, VersionId: put.VersionId})
		return e
	}
	if del() == nil {
		t.Fatal("保持 1 日の版が書いた直後に消えた")
	}
	dir := t.TempDir()
	d := &Deps{StateDir: dir, Clock: SimClock{Path: path}, Limits: DefaultLimits(), State: &state.StateFiles{Dir: dir}}
	d.spawn = func(context.Context, []string) (int, error) { return 0, nil } // 子の実行は完了条件 4。ここは共有の値の進みだけを見る
	if _, err := testTick(ctx, d, []string{"--days", "3"}); err != nil {
		t.Fatal(err)
	}
	if err := del(); err != nil {
		t.Fatalf("test-tick --days 3 の後も削除が通らない(サーバーは再起動していない): %v", err)
	}
}
