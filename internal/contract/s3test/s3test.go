// Package s3test は S3 互換の保管先(MinIO、偽の保管先のサーバー)に対する試験の道具。
// Object Lock の意味論を S3 の API を通して照らす。製品のコードは import しない。
package s3test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// DateRecorder は応答の Date ヘッダを記録する http.RoundTripper。
type DateRecorder struct {
	Base http.RoundTripper
	Last string
}

// RoundTrip は http.RoundTripper を満たす。
func (d *DateRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := d.Base.RoundTrip(r)
	if err == nil {
		d.Last = resp.Header.Get("Date")
	}
	return resp, err
}

// NewClient は path-style の S3 クライアントを作る。
func NewClient(endpoint, ak, sk string, rec *DateRecorder) *s3.Client {
	hc := &http.Client{Timeout: 30 * time.Second}
	if rec != nil {
		rec.Base = http.DefaultTransport
		hc.Transport = rec
	}
	return s3.New(s3.Options{
		Region: "us-east-1", RetryMaxAttempts: 1, BaseEndpoint: aws.String(endpoint), UsePathStyle: true, HTTPClient: hc,
		Credentials:                aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(ak, sk, "")),
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
}

// ErrCode は S3 のエラーコードを返す(エラーでなければ空)。
func ErrCode(err error) string {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		return ae.ErrorCode()
	}
	return ""
}

// RunObjectLock は Object Lock の意味論を照らす。bucket は Object Lock 有効で作成済み。
// advance が nil でなければ、共有の値を d 進めて(サーバーは再起動しない)削除が通ることも照らす。
//
// 検査: 保持中の版の削除の拒否(コードが AccessDenied か InvalidRequest。NotImplemented などは不合格)、短縮の拒否(同じコード)、
// 延長が通り GetObjectRetention が Compliance と延長後の期限を返すこと、書き込みで明示した retain-until がそのまま採られること、
// x-amz-bypass-governance-retention を付けても削除が拒否されること、期限の境界(+23 時間で拒否、+25 時間で通り、版が消える)。
// writer が nil でなければ、書き込み鍵のクライアントでも版指定の削除が「保持中は拒否、期限後は通る」ことを照らす
// (書き込み鍵は版指定の削除を持つ。spec 3.2.6)。
func RunObjectLock(t *testing.T, c *s3.Client, bucket string, advance func(d time.Duration), writer *s3.Client) {
	t.Helper()
	ctx := context.Background()
	_, err := c.PutObjectLockConfiguration(ctx, &s3.PutObjectLockConfigurationInput{
		Bucket: &bucket,
		ObjectLockConfiguration: &types.ObjectLockConfiguration{
			ObjectLockEnabled: types.ObjectLockEnabledEnabled,
			Rule:              &types.ObjectLockRule{DefaultRetention: &types.DefaultRetention{Mode: types.ObjectLockRetentionModeCompliance, Days: aws.Int32(1)}},
		},
	})
	if err != nil {
		t.Fatalf("既定保持 1 日の設定: %v", err)
	}
	run := fmt.Sprintf("k%d/", time.Now().UnixNano()) // 同じバケットを再利用しても前の回の版と混ざらない
	put, err := c.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: aws.String(run + "a"), Body: bytes.NewReader([]byte("hello"))})
	if err != nil || put.VersionId == nil {
		t.Fatalf("PutObject: %v", err)
	}
	vid := put.VersionId
	if _, err := c.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: aws.String(run + "a"), VersionId: vid}); err == nil {
		t.Fatal("保持 1 日で書いた版の削除が通った(拒否されるべき)")
	} else if code := ErrCode(err); code != "AccessDenied" && code != "InvalidRequest" {
		// 偽の保管先と実際の S3 は AccessDenied、MinIO(WORM protected)は InvalidRequest を返す。
		t.Errorf("削除の拒否のコード: %q", code)
	}
	short := time.Now().Add(time.Hour)
	_, err = c.PutObjectRetention(ctx, &s3.PutObjectRetentionInput{Bucket: &bucket, Key: aws.String(run + "a"), VersionId: vid,
		Retention: &types.ObjectLockRetention{Mode: types.ObjectLockRetentionModeCompliance, RetainUntilDate: &short}})
	if err == nil {
		t.Error("保持の短縮が通った(拒否されるべき)")
	} else if code := ErrCode(err); code != "AccessDenied" && code != "InvalidRequest" {
		t.Errorf("短縮の拒否のコード: %q(エラーなら何でもよいわけではない)", code)
	}
	runExplicit(t, c, writer, bucket, run, advance)
	lv, err := c.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: &bucket, Prefix: aws.String(run + "a")})
	if err != nil || len(lv.Versions) != 1 {
		t.Fatalf("ListObjectVersions: %v %v", lv, err)
	}
	if _, err := c.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: aws.String(run + "a")}); err != nil {
		t.Errorf("版指定なしの削除(削除マーカー): %v", err)
	}
	if advance != nil {
		advance(48 * time.Hour)
		if _, err := c.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: aws.String(run + "a"), VersionId: vid}); err != nil {
			t.Errorf("共有の値を 2 日進めた後の版指定の削除が通らない: %v", err)
		}
	}
}

// runExplicit は明示した retain-until(実際の書き込み経路)、延長、バイパスの拒否、期限の境界を照らす。
func runExplicit(t *testing.T, c, writer *s3.Client, bucket, run string, advance func(d time.Duration)) {
	t.Helper()
	ctx := context.Background()
	until := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	key := aws.String(run + "e")
	put, err := c.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: key, Body: bytes.NewReader([]byte("e")),
		ObjectLockMode: types.ObjectLockModeCompliance, ObjectLockRetainUntilDate: &until})
	if err != nil {
		t.Fatalf("retain-until を明示した PutObject: %v", err)
	}
	get := func(v *string) *types.ObjectLockRetention {
		r, err := c.GetObjectRetention(ctx, &s3.GetObjectRetentionInput{Bucket: &bucket, Key: key, VersionId: v})
		if err != nil {
			t.Fatalf("GetObjectRetention: %v", err)
		}
		return r.Retention
	}
	if r := get(put.VersionId); r.Mode != types.ObjectLockRetentionModeCompliance || r.RetainUntilDate == nil || r.RetainUntilDate.Sub(until).Abs() > 2*time.Second {
		t.Errorf("明示した retain-until が採られていない: %+v", r)
	}
	longer := until.Add(24 * time.Hour)
	if _, err := c.PutObjectRetention(ctx, &s3.PutObjectRetentionInput{Bucket: &bucket, Key: key, VersionId: put.VersionId,
		Retention: &types.ObjectLockRetention{Mode: types.ObjectLockRetentionModeCompliance, RetainUntilDate: &longer}}); err != nil {
		t.Errorf("延長が通らない: %v", err)
	} else if r := get(put.VersionId); r.RetainUntilDate == nil || r.RetainUntilDate.Sub(longer).Abs() > 2*time.Second {
		t.Errorf("延長後の期限: %+v", r)
	}
	del := func(cl *s3.Client, bypass bool) error {
		_, err := cl.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: key, VersionId: put.VersionId, BypassGovernanceRetention: aws.Bool(bypass)})
		return err
	}
	if del(c, true) == nil {
		t.Error("バイパスを付けた削除が通った(Compliance はバイパスできない)")
	}
	if writer != nil && del(writer, false) == nil {
		t.Error("書き込み鍵の保持中の版の削除が通った")
	}
	if advance == nil {
		return
	}
	advance(23 * time.Hour)
	if del(c, false) == nil {
		t.Error("期限(延長後は +48 時間)の前なのに削除が通った")
	}
	advance(49 * time.Hour)
	cl := c
	if writer != nil {
		cl = writer
	}
	if err := del(cl, false); err != nil {
		t.Errorf("期限後の版指定の削除が通らない: %v", err)
	}
	if lv, err := c.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: &bucket, Prefix: key}); err != nil || len(lv.Versions) != 0 {
		t.Errorf("削除した版が一覧に残っている: %v %v", lv, err)
	}
}

// IsInternal は 5xx 相当の内部エラーか。
func IsInternal(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "StatusCode: 500") || ErrCode(err) == "InternalError")
}
