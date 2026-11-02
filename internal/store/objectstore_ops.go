package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// ListVersions は contract.ObjectStore を満たす。`<p>` の下だけを返し、Key から `<p>` を外す。削除マーカーも含める。
// RetainUntil は埋めない(S3 の一覧は retain-until を返さず、全版を読むと要求数が版の数に比例する。spec 4.2 の verify は抜き取る)。
// 必要な版だけ RetainUntilOf で版指定で読む。
func (s *ObjectStore) ListVersions(ctx context.Context, prefix string) ([]contract.VersionInfo, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	var all []contract.VersionInfo
	var keyMarker, verMarker *string
	for {
		var out *s3.ListObjectVersionsOutput
		err := s.call(ctx, func(actx context.Context) error {
			var err error
			out, err = s.client.ListObjectVersions(actx, &s3.ListObjectVersionsInput{
				Bucket: aws.String(s.Bucket), Prefix: s.key(prefix), KeyMarker: keyMarker, VersionIdMarker: verMarker,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, v := range out.Versions {
			all = append(all, contract.VersionInfo{
				Key: strings.TrimPrefix(aws.ToString(v.Key), s.Prefix), VersionID: aws.ToString(v.VersionId),
				LastModified: aws.ToTime(v.LastModified).UTC(), Size: aws.ToInt64(v.Size), IsLatest: aws.ToBool(v.IsLatest),
			})
		}
		for _, m := range out.DeleteMarkers {
			all = append(all, contract.VersionInfo{
				Key: strings.TrimPrefix(aws.ToString(m.Key), s.Prefix), VersionID: aws.ToString(m.VersionId),
				LastModified: aws.ToTime(m.LastModified).UTC(), IsLatest: aws.ToBool(m.IsLatest), DeleteMarker: true,
			})
		}
		if !aws.ToBool(out.IsTruncated) {
			return all, nil
		}
		keyMarker, verMarker = out.NextKeyMarker, out.NextVersionIdMarker
	}
}

// RetainUntil は contract.ObjectStore を満たす。版指定で現在の retain-until を読む。保持の無い版はゼロ、版が無ければ contract.ErrMissing。読めなければ誤り。
func (s *ObjectStore) RetainUntil(ctx context.Context, key, versionID string) (time.Time, error) {
	if err := s.ready(); err != nil {
		return time.Time{}, err
	}
	if versionID == "" {
		return time.Time{}, internalErr("contract")
	}
	var until time.Time
	err := s.call(ctx, func(actx context.Context) error {
		out, err := s.client.GetObjectRetention(actx, &s3.GetObjectRetentionInput{Bucket: aws.String(s.Bucket), Key: s.key(key), VersionId: aws.String(versionID)})
		var ae smithy.APIError
		if errors.As(err, &ae) && (ae.ErrorCode() == "NoSuchObjectLockConfiguration" || ae.ErrorCode() == "ObjectLockConfigurationNotFoundError") {
			until = time.Time{}
			return nil
		}
		if err != nil {
			return err
		}
		if out.Retention != nil {
			until = aws.ToTime(out.Retention.RetainUntilDate).UTC()
		}
		return nil
	})
	return until, err
}

// BucketConfig は contract.ObjectStore を満たす。Object Lock が有効で既定保持の規則が無いバケットは LockMode を "COMPLIANCE"、DefaultRetention を 0 で返す。
func (s *ObjectStore) BucketConfig(ctx context.Context) (contract.BucketConfig, error) {
	var cfg contract.BucketConfig
	if err := s.ready(); err != nil {
		return cfg, err
	}
	err := s.call(ctx, func(actx context.Context) error {
		v, err := s.client.GetBucketVersioning(actx, &s3.GetBucketVersioningInput{Bucket: aws.String(s.Bucket)})
		if err != nil {
			return err
		}
		cfg.Versioning = v.Status == types.BucketVersioningStatusEnabled
		return nil
	})
	if err != nil {
		return cfg, err
	}
	err = s.call(ctx, func(actx context.Context) error {
		l, err := s.client.GetObjectLockConfiguration(actx, &s3.GetObjectLockConfigurationInput{Bucket: aws.String(s.Bucket)})
		var ae smithy.APIError
		if errors.As(err, &ae) && ae.ErrorCode() == "ObjectLockConfigurationNotFoundError" {
			return nil // Object Lock なし(LockMode は空)
		}
		if err != nil {
			return err
		}
		c := l.ObjectLockConfiguration
		if c == nil || c.ObjectLockEnabled != types.ObjectLockEnabledEnabled {
			return nil
		}
		// 既定保持が無いバケットは、製品が書く方式(明示の retain-until は常に Compliance)として COMPLIANCE とし、DefaultRetention を 0 で示す。
		cfg.LockMode = "COMPLIANCE"
		if c.Rule != nil && c.Rule.DefaultRetention != nil {
			d := c.Rule.DefaultRetention
			cfg.LockMode = strings.ToUpper(string(d.Mode))
			cfg.DefaultRetention = int(aws.ToInt32(d.Days)) + 365*int(aws.ToInt32(d.Years))
		}
		return nil
	})
	return cfg, err
}

// StoreTime は contract.ObjectStore を満たす。直前の応答の Date(実時刻。オフセットは足さない)。応答がまだ無ければバケット設定を 1 回読んで得る。
func (s *ObjectStore) StoreTime(ctx context.Context) (time.Time, error) {
	if err := s.ready(); err != nil {
		return time.Time{}, err
	}
	if err := s.ensureDate(ctx); err != nil {
		return time.Time{}, err
	}
	d, _ := s.lastDate()
	return d, nil
}
