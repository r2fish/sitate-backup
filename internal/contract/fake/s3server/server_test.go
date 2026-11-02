package s3server_test

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/fake/s3server"
	"github.com/r2fish/sitate-backup/internal/contract/s3test"
)

func start(t *testing.T) (*s3.Client, *s3test.DateRecorder, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "simtime")
	now := time.Now()
	if err := contract.WriteSimTime(path, now, now); err != nil {
		t.Fatal(err)
	}
	core := fake.NewCore(fake.FuncClock(func() (time.Time, error) {
		st, err := contract.ReadSimTime(path)
		if err != nil {
			return time.Time{}, err
		}
		return st.Now(time.Now())
	}))
	ts := httptest.NewServer(s3server.New(core, "AKIATEST", "secret-test"))
	t.Cleanup(ts.Close)
	rec := &s3test.DateRecorder{}
	return s3test.NewClient(ts.URL, "AKIATEST", "secret-test", rec), rec, path, ts.URL
}

func TestServerObjectLockThroughS3API(t *testing.T) {
	c, rec, path, _ := start(t)
	ctx := context.Background()
	if _, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("lockb"), ObjectLockEnabledForBucket: aws.Bool(true)}); err != nil {
		t.Fatal(err)
	}
	s3test.RunObjectLock(t, c, "lockb", func(d time.Duration) {
		now := time.Now()
		_ = contract.WriteSimTime(path, now.Add(d), now)
	}, nil)
	// 応答の Date は実時刻(共有の値を 2 日進めた後でも)
	if d, err := time.Parse("Mon, 02 Jan 2006 15:04:05 GMT", rec.Last); err != nil || time.Since(d) > time.Minute || time.Since(d) < -time.Minute {
		t.Errorf("Date が実時刻でない: %q %v", rec.Last, err)
	}
	cfg, err := c.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String("lockb")})
	if err != nil || cfg.Status != types.BucketVersioningStatusEnabled {
		t.Errorf("GetBucketVersioning: %v %v", cfg, err)
	}
}

func TestServerInternalErrorWhenSimTimeBroken(t *testing.T) {
	c, _, path, _ := start(t)
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := c.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("x1x")})
	if !s3test.IsInternal(err) {
		t.Errorf("壊れた共有の値で 5xx にならない: %v", err)
	}
	_ = os.Remove(path)
	_, err = c.GetBucketVersioning(context.Background(), &s3.GetBucketVersioningInput{Bucket: aws.String("x1x")})
	if !s3test.IsInternal(err) {
		t.Errorf("共有の値が無いのに 5xx にならない: %v", err)
	}
}

func TestServerRejectsBadSignatureAndOutOfScope(t *testing.T) {
	_, _, _, url := start(t)
	bad := s3test.NewClient(url, "AKIATEST", "wrong-secret", nil)
	_, err := bad.ListObjectVersions(context.Background(), &s3.ListObjectVersionsInput{Bucket: aws.String("b")})
	if s3test.ErrCode(err) != "SignatureDoesNotMatch" {
		t.Errorf("署名違い: %v", err)
	}
	good := s3test.NewClient(url, "AKIATEST", "secret-test", nil)
	_, err = good.ListBuckets(context.Background(), &s3.ListBucketsInput{})
	if s3test.ErrCode(err) != "NotImplemented" {
		t.Errorf("範囲外の操作が S3 の形のエラーにならない: %v", err)
	}
}
