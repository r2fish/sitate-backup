package s3server_test

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Content-MD5 の試験
	"encoding/base64"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/fake/s3server"
	"github.com/r2fish/sitate-backup/internal/contract/s3test"
)

type scope struct {
	adm, wr, rd *s3.Client
	b           *string
}

func newScope(t *testing.T) scope {
	t.Helper()
	core := fake.NewCore(fake.FuncClock(func() (time.Time, error) { return time.Now().UTC(), nil }))
	srv := s3server.New(core, "AKADMIN", "adminsecret")
	srv.Keys["AKWRITE"] = s3server.Key{Secret: "writesecret", Role: "write"}
	srv.Keys["AKREAD"] = s3server.Key{Secret: "readsecret", Role: "read"}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	sc := scope{
		adm: s3test.NewClient(ts.URL, "AKADMIN", "adminsecret", nil),
		wr:  s3test.NewClient(ts.URL, "AKWRITE", "writesecret", nil),
		rd:  s3test.NewClient(ts.URL, "AKREAD", "readsecret", nil),
		b:   aws.String("scope"),
	}
	_, err := sc.adm.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: sc.b, ObjectLockEnabledForBucket: aws.Bool(true)})
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	return sc
}

func must(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func putA(t *testing.T, sc scope, key string, until time.Time) *s3.PutObjectOutput {
	t.Helper()
	body := []byte("payload")
	sum := md5.Sum(body) //nolint:gosec
	put, err := sc.wr.PutObject(context.Background(), &s3.PutObjectInput{Bucket: sc.b, Key: aws.String(key), Body: bytes.NewReader(body),
		ContentMD5: aws.String(base64.StdEncoding.EncodeToString(sum[:])), ObjectLockMode: types.ObjectLockModeCompliance, ObjectLockRetainUntilDate: &until})
	must(t, "PutObject(Content-MD5, retain-until)", err)
	return put
}

// 範囲の各操作(GB-99)を S3 の API を通して照らす(1: 書き込みと読み出し)。
func TestServerOpsWriteAndRead(t *testing.T) {
	sc := newScope(t)
	ctx := context.Background()
	lc, err := sc.adm.GetObjectLockConfiguration(ctx, &s3.GetObjectLockConfigurationInput{Bucket: sc.b})
	must(t, "GetObjectLockConfiguration", err)
	if lc.ObjectLockConfiguration.ObjectLockEnabled != types.ObjectLockEnabledEnabled {
		t.Error("Object Lock が有効でない")
	}
	put := putA(t, sc, "a", time.Now().Add(48*time.Hour))
	bad := md5.Sum([]byte("other")) //nolint:gosec
	_, err = sc.wr.PutObject(ctx, &s3.PutObjectInput{Bucket: sc.b, Key: aws.String("a"), Body: bytes.NewReader([]byte("payload")), ContentMD5: aws.String(base64.StdEncoding.EncodeToString(bad[:]))})
	if s3test.ErrCode(err) != "BadDigest" {
		t.Errorf("Content-MD5 の不一致: %v", err)
	}
	_, err = sc.rd.PutObject(ctx, &s3.PutObjectInput{Bucket: sc.b, Key: aws.String("r"), Body: bytes.NewReader([]byte("x"))})
	if s3test.ErrCode(err) != "AccessDenied" {
		t.Errorf("読み取り鍵が書けた: %v", err)
	}
	g, err := sc.rd.GetObject(ctx, &s3.GetObjectInput{Bucket: sc.b, Key: aws.String("a"), VersionId: put.VersionId})
	must(t, "GetObject(版指定)", err)
	if got, _ := io.ReadAll(g.Body); string(got) != "payload" {
		t.Errorf("本体: %q", got)
	}
	h, err := sc.rd.HeadObject(ctx, &s3.HeadObjectInput{Bucket: sc.b, Key: aws.String("a"), VersionId: put.VersionId})
	must(t, "HeadObject(版指定)", err)
	if h.ObjectLockRetainUntilDate == nil {
		t.Error("HeadObject に retain-until が無い")
	}
}

// 2: 保持・削除・一覧・範囲の外。
func TestServerOpsRetentionDeleteList(t *testing.T) {
	sc := newScope(t)
	ctx := context.Background()
	until := time.Now().Add(48 * time.Hour)
	put := putA(t, sc, "a", until)
	gr, err := sc.rd.GetObjectRetention(ctx, &s3.GetObjectRetentionInput{Bucket: sc.b, Key: aws.String("a"), VersionId: put.VersionId})
	must(t, "GetObjectRetention", err)
	if gr.Retention.RetainUntilDate == nil {
		t.Error("GetObjectRetention の期限が無い")
	}
	longer := until.Add(24 * time.Hour)
	_, err = sc.adm.PutObjectRetention(ctx, &s3.PutObjectRetentionInput{Bucket: sc.b, Key: aws.String("a"), VersionId: put.VersionId,
		Retention: &types.ObjectLockRetention{Mode: types.ObjectLockRetentionModeCompliance, RetainUntilDate: &longer}})
	must(t, "PutObjectRetention(延長)", err)
	if _, err := sc.wr.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: sc.b, Key: aws.String("a"), VersionId: put.VersionId}); err == nil {
		t.Error("保持中の版は書き込み鍵でも Object Lock で拒否される")
	}
	if _, err := sc.wr.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: sc.b, Key: aws.String("a")}); err == nil {
		t.Error("既定では書き込み鍵の版指定なしの削除は拒否される")
	}
	_, err = sc.adm.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: sc.b, Key: aws.String("a")})
	must(t, "DeleteObject(版指定なし=削除マーカー)", err)
	lv, err := sc.adm.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: sc.b})
	must(t, "ListObjectVersions", err)
	if len(lv.Versions) != 1 || len(lv.DeleteMarkers) != 1 {
		t.Errorf("版と削除マーカー: %d %d", len(lv.Versions), len(lv.DeleteMarkers))
	}
	_, err = sc.adm.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: sc.b})
	must(t, "GetBucketVersioning", err)
	_, err = sc.adm.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: sc.b, Policy: aws.String("{}")})
	if s3test.ErrCode(err) != "NotImplemented" {
		t.Errorf("範囲の外: %v", err)
	}
}

// retain-until を付けた書き込みと保持の設定は Mode: COMPLIANCE だけを受ける(GOVERNANCE・無指定は拒否)。
func TestPutRequiresCompliance(t *testing.T) {
	sc := newScope(t)
	ctx := context.Background()
	until := time.Now().Add(48 * time.Hour)
	for _, mode := range []types.ObjectLockMode{types.ObjectLockModeGovernance, ""} {
		_, err := sc.wr.PutObject(ctx, &s3.PutObjectInput{Bucket: sc.b, Key: aws.String("g"), Body: bytes.NewReader([]byte("x")),
			ObjectLockMode: mode, ObjectLockRetainUntilDate: &until})
		if err == nil {
			t.Errorf("Mode %q の書き込みが通った", mode)
		}
	}
}
