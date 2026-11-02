package store

// StoreProbe だけが使う `probe/` 限定の操作(spec 4.2「`bucket check` の試し方」)。契約の ObjectStore には入れず、StoreProbe が *ObjectStore の型を要求して呼ぶ。

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Content-MD5 は保管先の要求。暗号の用途ではない
	"encoding/base64"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// PutNoRetention は保持期限を明示せずに書く(`probe/` への書き込み。バケットの既定保持が掛かる。spec 4.2)。Content-MD5 は付ける。
// `probe/` 以外のキーには使わない。
func (s *ObjectStore) PutNoRetention(ctx context.Context, key string, body []byte) (string, error) {
	if err := s.ready(); err != nil {
		return "", err
	}
	if !strings.HasPrefix(key, "probe/") {
		return "", internalErr("contract")
	}
	sum := md5.Sum(body) //nolint:gosec
	var vid string
	err := s.call(ctx, func(actx context.Context) error {
		out, err := s.client.PutObject(actx, &s3.PutObjectInput{
			Bucket: aws.String(s.Bucket), Key: s.key(key), Body: bytes.NewReader(body), ContentLength: aws.Int64(int64(len(body))),
			ContentMD5: aws.String(base64.StdEncoding.EncodeToString(sum[:])),
		})
		if err != nil {
			return err
		}
		vid = aws.ToString(out.VersionId)
		if vid == "" {
			return internalErr("contract")
		}
		return nil
	})
	return vid, err
}

func (s *ObjectStore) probeKey(key string) error {
	if err := s.ready(); err != nil {
		return err
	}
	if !strings.HasPrefix(key, "probe/") {
		return internalErr("contract") // probe/ 以外には使わない
	}
	return nil
}

// UnversionedDelete は `probe/` のキーへの版指定なしの削除を試す(通れば削除マーカーが残るだけ)。製品で版指定なしの DELETE を送るのはここだけ。
func (s *ObjectStore) UnversionedDelete(ctx context.Context, key string) error {
	if err := s.probeKey(key); err != nil {
		return err
	}
	return s.call(ctx, func(actx context.Context) error {
		_, err := s.client.DeleteObject(actx, &s3.DeleteObjectInput{Bucket: aws.String(s.Bucket), Key: s.key(key)})
		return err
	})
}

// SetLegalHold は `probe/` の版のリーガルホールドを設定(on)または外す(off)。設定の試行が通ったら、呼び出し側が外す。
func (s *ObjectStore) SetLegalHold(ctx context.Context, key, versionID string, on bool) error {
	if err := s.probeKey(key); err != nil {
		return err
	}
	if versionID == "" {
		return internalErr("contract")
	}
	st := types.ObjectLockLegalHoldStatusOff
	if on {
		st = types.ObjectLockLegalHoldStatusOn
	}
	return s.call(ctx, func(actx context.Context) error {
		_, err := s.client.PutObjectLegalHold(actx, &s3.PutObjectLegalHoldInput{Bucket: aws.String(s.Bucket), Key: s.key(key), VersionId: aws.String(versionID), LegalHold: &types.ObjectLockLegalHold{Status: st}})
		return err
	})
}
