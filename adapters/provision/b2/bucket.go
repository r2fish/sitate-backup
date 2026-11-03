package main

import (
	"context"
	"errors"
	"strings"
)

const daysPerYear = 365 // 既定保持の単位が年なら 365 日 × 年数として比べる(spec 3.2.6)

type retention struct {
	Mode   *string `json:"mode"`
	Period *struct {
		Duration int64  `json:"duration"`
		Unit     string `json:"unit"`
	} `json:"period"`
}

type lockConfig struct {
	Authorized bool `json:"isClientAuthorizedToRead"`
	Value      *struct {
		Enabled   bool       `json:"isFileLockEnabled"`
		Retention *retention `json:"defaultRetention"`
	} `json:"value"`
}

type bucketInfo struct {
	ID   string     `json:"bucketId"`
	Name string     `json:"bucketName"`
	Lock lockConfig `json:"fileLockConfiguration"`
}

// bucketState は読めた設定。nil は読めなかった(または無い)値。
type bucketState struct {
	lockEnabled *bool
	mode        *string
	days        *int64
}

func (b *bucketInfo) state() bucketState {
	var st bucketState
	if !b.Lock.Authorized || b.Lock.Value == nil {
		return st
	}
	en := b.Lock.Value.Enabled
	st.lockEnabled = &en
	if r := b.Lock.Value.Retention; r != nil && r.Mode != nil {
		m := strings.ToUpper(*r.Mode)
		st.mode = &m
		if r.Period != nil {
			d := r.Period.Duration
			if r.Period.Unit == "years" {
				d *= daysPerYear
			}
			st.days = &d
		}
	}
	return st
}

// findBucket は自分のアカウントのバケットを名前で探す。無ければ nil。
func (s *session) findBucket(ctx context.Context, name string) (*bucketInfo, *provErr) {
	var res struct {
		Buckets []bucketInfo `json:"buckets"`
	}
	if e := s.call(ctx, "b2_list_buckets", map[string]any{"accountId": s.accountID, "bucketName": name}, &res); e != nil {
		return nil, e
	}
	for i := range res.Buckets {
		if res.Buckets[i].Name == name {
			return &res.Buckets[i], nil
		}
	}
	return nil, nil
}

func retentionBody(days int64) map[string]any {
	return map[string]any{"mode": "compliance", "period": map[string]any{"duration": days, "unit": "days"}}
}

// createBucket は create-bucket(spec 3.2.6)。同名が自分のアカウントにあれば設定を要求と比べる。
func createBucket(ctx context.Context, c *client, req *request) (map[string]any, *provErr) {
	id, secret := req.creds("admin")
	s, perr := c.authorize(ctx, id, secret, req.Endpoint, "admin")
	if perr != nil {
		return nil, perr
	}
	days := *req.DefaultRetention
	body := map[string]any{"accountId": s.accountID, "bucketName": req.Bucket, "bucketType": "allPrivate", "fileLockEnabled": true}
	var made struct {
		BucketID string `json:"bucketId"`
	}
	err := s.callRaw(ctx, "b2_create_bucket", body, &made)
	if err == nil {
		// 既定保持は作成の項目ではない(実 API は bad_request)。続けて update で付ける。失敗しても再実行は existingBucket の補完で続く。
		upd := map[string]any{"accountId": s.accountID, "bucketId": made.BucketID, "defaultRetention": retentionBody(days)}
		if e := s.call(ctx, "b2_update_bucket", upd, nil); e != nil {
			return nil, e
		}
		return bucketResult(true, days), nil
	}
	var ae *apiError
	if errors.As(err, &ae) && ae.status == 400 && ae.code == "duplicate_bucket_name" {
		return existingBucket(ctx, s, req)
	}
	return nil, mapAPIError(err, "admin")
}

func bucketResult(created bool, days int64) map[string]any {
	return map[string]any{"created": created, "versioning": true, "lock_mode": "COMPLIANCE", "default_retention_days": days}
}

// existingBucket は同名のバケットを要求と比べる。他のアカウントのものなら BUCKET_NOT_OWNED(taken)。
func existingBucket(ctx context.Context, s *session, req *request) (map[string]any, *provErr) {
	b, perr := s.findBucket(ctx, req.Bucket)
	if perr != nil {
		return nil, perr
	}
	if b == nil {
		return nil, bucketNotOwned("taken")
	}
	days := *req.DefaultRetention
	st := b.state()
	if st.lockEnabled != nil && *st.lockEnabled && st.mode == nil { // Object Lock が有効で既定保持だけが無い: 途中で失敗した作成の残りを補完する
		body := map[string]any{"accountId": s.accountID, "bucketId": b.ID, "defaultRetention": retentionBody(days)}
		if e := s.call(ctx, "b2_update_bucket", body, nil); e != nil {
			return nil, e
		}
		return bucketResult(false, days), nil
	}
	if mm := mismatches(st, days); len(mm) > 0 {
		return nil, &provErr{code: "BUCKET_MISMATCH", detail: map[string]any{"mismatches": mm}}
	}
	return bucketResult(false, days), nil
}

func mismatches(st bucketState, days int64) []map[string]any {
	mm := []map[string]any{}
	add := func(item string, expected, actual any) {
		mm = append(mm, map[string]any{"item": item, "expected": expected, "actual": actual})
	}
	if st.lockEnabled == nil || !*st.lockEnabled || st.mode == nil || *st.mode != "COMPLIANCE" {
		var actual any
		if st.mode != nil {
			actual = *st.mode
		}
		add("lock_mode", "COMPLIANCE", actual)
	}
	if st.days == nil || *st.days != days {
		var actual any
		if st.days != nil {
			actual = *st.days
		}
		if st.mode != nil || st.lockEnabled == nil {
			add("default_retention_days", days, actual)
		}
	}
	return mm
}
