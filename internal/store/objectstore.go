package store

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Content-MD5 は保管先の要求(B2 は保持期限つきの書き込みで要る。spec 4.2)。暗号の用途ではない
	"encoding/base64"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// monthlyMargin は月次の世代のマニフェストの retain-until に足す余裕(延長周期 7 日。spec 4.2 の書き込みの retain-until)。
const monthlyMargin = 7 * 24 * time.Hour

// ObjectStore は contract.ObjectStore の実物(S3 互換の保管先。spec 4.2、7.5)。
// 依存は公開フィールドで受ける(契約試験が差し込む)。Offset は試験ビルドの「Date に足すオフセット」(製品ビルドでは nil)。
// retain-until と Content-MD5 は呼び出し側から受けずに付ける。
type ObjectStore struct {
	Endpoint, Bucket, AccessKey, SecretKey string
	// Region は署名に使う領域名(空は us-east-1。MinIO・偽の保管先はこれでよい)。
	Region string
	// Prefix は spec 4.2 の `<p>`(既定 "sitate/")。実物が全操作のキーに付け、ListVersions の Key から外す(contract/storage.go の注記)。
	Prefix string
	Params contract.RetentionParams
	Offset contract.Offsetter
	// Clock は TIME_SKEW の比べる相手(バックアップサーバーの時計)。nil は実時刻。試験ビルドの結合は模擬時間の時計を入れる。
	Clock contract.Clock

	once   sync.Once
	client *s3.Client
	sem    chan struct{}
	mu     sync.Mutex
	date   time.Time
	// haveDate は date が直前の応答の Date かどうか。dateAt はその応答を受けた手元の時刻(古い Date で判定・計算しないために使う)。
	haveDate bool
	dateAt   time.Time
	wall     func() time.Time
	// 試験用: 待ちと要求のタイムアウトの差し替え(0 / nil は既定)。
	sleep   func(context.Context, time.Duration) error
	timeout time.Duration
}

var _ contract.ObjectStore = (*ObjectStore)(nil)

func (s *ObjectStore) key(k string) *string { return aws.String(s.Prefix + k) }

func (s *ObjectStore) offset() (time.Duration, error) {
	if s.Offset == nil {
		return 0, nil
	}
	return s.Offset.Offset()
}

func (s *ObjectStore) localNow(ctx context.Context) time.Time {
	if s.Clock != nil {
		if t, err := s.Clock.Now(ctx); err == nil {
			return t
		}
	}
	return time.Now().UTC()
}

func (s *ObjectStore) wallNow() time.Time {
	if s.wall != nil {
		return s.wall()
	}
	return time.Now()
}

// ensureDate は応答がまだ無いとき、先にバケット設定を 1 回読んで Date を得る(spec 4.2。D-0105)。
// 直前の応答が TIME_SKEW_MAX の半分より古いときも読み直す(古い Date で判定すると、時計が合っていても TIME_SKEW になり、retain-until も短くなる)。
func (s *ObjectStore) ensureDate(ctx context.Context) error {
	s.mu.Lock()
	stale := !s.haveDate || s.wallNow().Sub(s.dateAt) > contract.TimeSkewMax/2
	s.mu.Unlock()
	if !stale {
		return nil
	}
	_, err := s.BucketConfig(ctx)
	return err
}

// CheckSkew は contract.ObjectStore を満たす。直前の応答の Date(古ければ読み直す)+ オフセットと手元の時計の差が TIME_SKEW_MAX を超えれば TIME_SKEW。
func (s *ObjectStore) CheckSkew(ctx context.Context) error {
	if err := s.ready(); err != nil {
		return err
	}
	if err := s.ensureDate(ctx); err != nil {
		return err
	}
	_, err := s.checkSkew(ctx)
	return err
}

// storeNow は直前の応答の Date + 注入したオフセット(retain-until・TIME_SKEW の基準。spec 4.2)。
func (s *ObjectStore) storeNow() (time.Time, error) {
	d, ok := s.lastDate()
	if !ok {
		return time.Time{}, internalErr("contract")
	}
	off, err := s.offset()
	if err != nil {
		return time.Time{}, err
	}
	return d.Add(off), nil
}

func (s *ObjectStore) skewDetail() map[string]any {
	skew := time.Duration(0)
	if now, err := s.storeNow(); err == nil {
		skew = s.localNow(context.Background()).Sub(now)
	}
	return map[string]any{"skew_s": int64(skew / time.Second), "max_s": int64(contract.TimeSkewMax / time.Second)}
}

// checkSkew はバックアップサーバーの時計と「Date + オフセット」の差が TIME_SKEW_MAX を超えたら TIME_SKEW を返す(保持操作と書き込みを止める)。
func (s *ObjectStore) checkSkew(ctx context.Context) (time.Time, error) {
	now, err := s.storeNow()
	if err != nil {
		return time.Time{}, err
	}
	skew := s.localNow(ctx).Sub(now)
	if skew > contract.TimeSkewMax || skew < -contract.TimeSkewMax {
		return time.Time{}, newErr(contract.CodeTimeSkew, s.skewDetail())
	}
	return now, nil
}

// retainUntil は保持区分と保持の定数と「Date + オフセット」から retain-until を計算する(日次 = + 日数、月次 = + 月数 + 7 日)。
func (s *ObjectStore) retainUntil(now time.Time, class contract.RetentionClass) time.Time {
	if class == contract.RetentionMonthly {
		return contract.AddMonthsUTC(now, s.Params.MonthlyMonths()).Add(monthlyMargin).Truncate(time.Second)
	}
	return now.AddDate(0, 0, s.Params.DailyDays()).UTC().Truncate(time.Second)
}

// Put は contract.ObjectStore を満たす。書き込みのたびに Compliance の retain-until を明示し、Content-MD5 を付ける。
func (s *ObjectStore) Put(ctx context.Context, key string, body io.Reader, class contract.RetentionClass) (contract.PutResult, error) {
	if err := s.ready(); err != nil {
		return contract.PutResult{}, err
	}
	if class != contract.RetentionDaily && class != contract.RetentionMonthly {
		return contract.PutResult{}, internalErr("contract") // 未知の保持区分を日次として書かない
	}
	if err := aborted(ctx); err != nil {
		return contract.PutResult{}, err
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return contract.PutResult{}, err
	}
	sum := md5.Sum(data) //nolint:gosec
	md5b64 := base64.StdEncoding.EncodeToString(sum[:])
	if err := s.ensureDate(ctx); err != nil {
		return contract.PutResult{}, err
	}
	var res contract.PutResult
	err = s.call(ctx, func(actx context.Context) error {
		now, err := s.checkSkew(actx) // 再試行のたびに直前の応答の Date から計算し直す
		if err != nil {
			return err
		}
		until := s.retainUntil(now, class)
		out, err := s.client.PutObject(actx, &s3.PutObjectInput{
			Bucket: aws.String(s.Bucket), Key: s.key(key), Body: bytes.NewReader(data), ContentLength: aws.Int64(int64(len(data))),
			ContentMD5: aws.String(md5b64), ObjectLockMode: types.ObjectLockModeCompliance, ObjectLockRetainUntilDate: aws.Time(until),
		})
		if err != nil {
			return err
		}
		if aws.ToString(out.VersionId) == "" {
			return internalErr("contract") // バージョニングが無いバケットでは版 ID を記録できない
		}
		res = contract.PutResult{VersionID: aws.ToString(out.VersionId), Size: int64(len(data)), ETag: strings.Trim(aws.ToString(out.ETag), `"`), RetainUntil: until}
		return nil
	})
	return res, err
}

type cancelCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelCloser) Close() error { err := c.ReadCloser.Close(); c.cancel(); return err }

// Get は contract.ObjectStore を満たす。版 ID を必ず指定する。版が無い・削除マーカーなら contract.ErrMissing。
func (s *ObjectStore) Get(ctx context.Context, key, versionID string) (io.ReadCloser, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if versionID == "" {
		return nil, internalErr("contract") // 版 ID を指定しない読み出しは使わない(spec 4.2)
	}
	var rc io.ReadCloser
	err := s.call(ctx, func(actx context.Context) error {
		// 本体を返すので、要求ごとのタイムアウトの取り消しは Close まで遅らせる。
		bctx, cancel := context.WithTimeout(ctx, s.reqTimeout())
		out, err := s.client.GetObject(bctx, &s3.GetObjectInput{Bucket: aws.String(s.Bucket), Key: s.key(key), VersionId: aws.String(versionID)})
		if err != nil {
			cancel()
			return err
		}
		rc = cancelCloser{out.Body, cancel}
		return nil
	})
	return rc, err
}

// DeleteVersion は contract.ObjectStore を満たす。必ず版 ID を指定する(版指定なしの削除は使わない)。
func (s *ObjectStore) DeleteVersion(ctx context.Context, key, versionID string) error {
	if err := s.ready(); err != nil {
		return err
	}
	if versionID == "" {
		return internalErr("contract")
	}
	if err := s.ensureDate(ctx); err != nil {
		return err
	}
	return s.call(ctx, func(actx context.Context) error {
		if _, err := s.checkSkew(actx); err != nil {
			return err
		}
		_, err := s.client.DeleteObject(actx, &s3.DeleteObjectInput{Bucket: aws.String(s.Bucket), Key: s.key(key), VersionId: aws.String(versionID)})
		return err
	})
}

// Extend は contract.ObjectStore を満たす。Compliance のまま retain-until を retainUntil にする。短縮は保管先が拒否する。
func (s *ObjectStore) Extend(ctx context.Context, key, versionID string, retainUntil time.Time) error {
	if err := s.ready(); err != nil {
		return err
	}
	if versionID == "" {
		return internalErr("contract")
	}
	if err := s.ensureDate(ctx); err != nil {
		return err
	}
	return s.call(ctx, func(actx context.Context) error {
		if _, err := s.checkSkew(actx); err != nil {
			return err
		}
		_, err := s.client.PutObjectRetention(actx, &s3.PutObjectRetentionInput{
			Bucket: aws.String(s.Bucket), Key: s.key(key), VersionId: aws.String(versionID),
			Retention: &types.ObjectLockRetention{Mode: types.ObjectLockRetentionModeCompliance, RetainUntilDate: aws.Time(retainUntil.UTC().Truncate(time.Second))},
		})
		return err
	})
}
