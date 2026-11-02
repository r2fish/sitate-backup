package fake

import (
	"bytes"
	"context"
	"io"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// ObjectStore は contract.ObjectStore の偽物(Go パッケージ)。Core の 1 つのバケットに結び付く。
// 保持期限は保持区分と RetentionParams、保管先の時刻(模擬時間込み)から計算する。
type ObjectStore struct {
	Core   *Core
	Bucket string
	Role   Role
	Params contract.RetentionParams
	// Prefix は spec 4.2 の `<p>`(例 "sitate/")。実物と同じく偽物が付け、一覧から外す(storage.go の注記)。空なら付けない。
	Prefix string
}

var _ contract.ObjectStore = (*ObjectStore)(nil)

// Put は contract.ObjectStore を満たす。
func (s *ObjectStore) Put(_ context.Context, key string, body io.Reader, class contract.RetentionClass) (contract.PutResult, error) {
	if s.Role == RoleRead {
		return contract.PutResult{}, s3err("AccessDenied", 403, "読み取り鍵は書けない")
	}
	b, err := io.ReadAll(body)
	if err != nil {
		return contract.PutResult{}, err
	}
	now, err := s.Core.now()
	if err != nil {
		return contract.PutResult{}, err
	}
	days, months := s.Params.DailyDays(), s.Params.MonthlyMonths()
	until := now.Add(time.Duration(days) * 24 * time.Hour)
	if class == contract.RetentionMonthly {
		until = contract.AddMonthsUTC(now, months).AddDate(0, 0, contract.RetentionExtendPeriodDays) // spec 4.2: 月次のマニフェストは + 12 か月 + 7 日
	}
	o, err := s.Core.Put(s.Bucket, s.Prefix+key, b, PutOptions{RetainUntil: until})
	if err != nil {
		return contract.PutResult{}, err
	}
	return contract.PutResult{VersionID: o.VersionID, Size: int64(len(o.Body)), ETag: o.ETag, RetainUntil: o.RetainUntil}, nil
}

// Get は contract.ObjectStore を満たす。版が無ければ contract.ErrMissing。
func (s *ObjectStore) Get(_ context.Context, key, version string) (io.ReadCloser, error) {
	if err := s.Core.Healthy(); err != nil {
		return nil, err
	}
	o, err := s.Core.Get(s.Bucket, s.Prefix+key, version)
	if err != nil {
		if e, ok := err.(*S3Error); ok && (e.Code == "NoSuchKey" || e.Code == "NoSuchVersion") {
			return nil, contract.ErrMissing
		}
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(o.Body)), nil
}

// ListVersions は contract.ObjectStore を満たす。
func (s *ObjectStore) ListVersions(_ context.Context, prefix string) ([]contract.VersionInfo, error) {
	if err := s.Core.Healthy(); err != nil {
		return nil, err
	}
	os, err := s.Core.List(s.Bucket, s.Prefix+prefix)
	if err != nil {
		return nil, err
	}
	var r []contract.VersionInfo
	seen := map[string]bool{}
	for _, o := range os {
		r = append(r, contract.VersionInfo{Key: strings.TrimPrefix(o.Key, s.Prefix), VersionID: o.VersionID, LastModified: o.Modified, Size: int64(len(o.Body)), IsLatest: !seen[o.Key], DeleteMarker: o.DeleteMarker, RetainUntil: o.RetainUntil})
		seen[o.Key] = true
	}
	return r, nil
}

// Extend は contract.ObjectStore を満たす。
func (s *ObjectStore) Extend(_ context.Context, key, version string, until time.Time) error {
	if err := s.Core.Healthy(); err != nil {
		return err
	}
	return s.Core.PutRetention(s.Bucket, s.Prefix+key, version, until, s.Role)
}

// DeleteVersion は contract.ObjectStore を満たす。
func (s *ObjectStore) DeleteVersion(_ context.Context, key, version string) error {
	if err := s.Core.Healthy(); err != nil {
		return err
	}
	_, err := s.Core.Delete(s.Bucket, s.Prefix+key, version, s.Role)
	return err
}

// BucketConfig は contract.ObjectStore を満たす。
func (s *ObjectStore) BucketConfig(_ context.Context) (contract.BucketConfig, error) {
	if err := s.Core.Healthy(); err != nil {
		return contract.BucketConfig{}, err
	}
	v, l, d, err := s.Core.Config(s.Bucket)
	cfg := contract.BucketConfig{Versioning: v, DefaultRetention: d}
	if l {
		cfg.LockMode = "COMPLIANCE"
	}
	return cfg, err
}

// StoreTime は contract.ObjectStore を満たす。応答の Date は実時刻で(サーバーと同じ。模擬時間は Object Lock の期限の判定だけ)、
// 核が「Date + 注入したオフセット」を計算する。Healthy でなければ保管先の 5xx 相当の誤りを返す。
func (s *ObjectStore) StoreTime(_ context.Context) (time.Time, error) {
	if err := s.Core.Healthy(); err != nil {
		return time.Time{}, err
	}
	return time.Now().UTC().Truncate(time.Second), nil
}
