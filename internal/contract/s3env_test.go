package contract_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/s3test"
)

// 試験用の保管先(make test-env-up NAME=<WP> [KIND=fake])に対する Object Lock の試験。
// 接続先と資格情報は contract.EnvTestS3* の環境変数で受ける。無ければ飛ばす(NAME 付きの make test では走る)。
func TestExternalS3ObjectLock(t *testing.T) {
	ep := os.Getenv(contract.EnvTestS3Endpoint)
	if ep == "" {
		t.Skip("SITATE_TEST_S3_ENDPOINT が無い(make test-env-up NAME=<WP> の後に NAME=<WP> を付けて実行)")
	}
	rec := &s3test.DateRecorder{}
	c := s3test.NewClient(ep, os.Getenv(contract.EnvTestS3AccessKey), os.Getenv(contract.EnvTestS3SecretKey), rec)
	bucket := os.Getenv(contract.EnvTestS3Bucket)
	kind := os.Getenv(contract.EnvTestS3Kind)
	var advance func(time.Duration)
	if kind == "fake" {
		if _, err := c.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String(bucket), ObjectLockEnabledForBucket: aws.Bool(true)}); err != nil && s3test.ErrCode(err) != "BucketAlreadyOwnedByYou" {
			t.Fatal(err)
		}
		path := os.Getenv(contract.EnvSimTimeFile)
		advance = func(d time.Duration) {
			now := time.Now()
			if err := contract.WriteSimTime(path, now.Add(d), now); err != nil {
				t.Fatal(err)
			}
		}
	}
	var writer *s3.Client
	if ak := os.Getenv(contract.EnvTestS3WriteAccessKey); ak != "" {
		writer = s3test.NewClient(ep, ak, os.Getenv(contract.EnvTestS3WriteSecretKey), nil)
	}
	s3test.RunObjectLock(t, c, bucket, advance, writer)
	if kind == "fake" { // 共有の値が壊れているとき・無いときにサーバーが内部エラー(5xx)を返す
		path := os.Getenv(contract.EnvSimTimeFile)
		_ = os.WriteFile(path, []byte("{broken"), 0o644)
		_, err := c.GetBucketVersioning(context.Background(), &s3.GetBucketVersioningInput{Bucket: aws.String(bucket)})
		if !s3test.IsInternal(err) {
			t.Errorf("壊れた共有の値で 5xx にならない: %v", err)
		}
		_ = os.Remove(path)
		_, err = c.GetBucketVersioning(context.Background(), &s3.GetBucketVersioningInput{Bucket: aws.String(bucket)})
		if !s3test.IsInternal(err) {
			t.Errorf("共有の値が無いのに 5xx にならない: %v", err)
		}
		now := time.Now()
		_ = contract.WriteSimTime(path, now, now)
	}
	d, err := time.Parse("Mon, 02 Jan 2006 15:04:05 GMT", rec.Last)
	if err != nil || time.Since(d) > 2*time.Minute || time.Since(d) < -2*time.Minute {
		t.Errorf("応答の Date が実時刻でない: %q %v", rec.Last, err)
	}
}
