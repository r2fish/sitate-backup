package state

// state.json の objects[](記録した版 ID。spec 4.2・4.5): 保管先へ書いた config/・journal/ などの版を key ごとに残す。

import (
	"context"
	"encoding/json"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// ObjectRec は objects[] の要素。
type ObjectRec struct {
	Key         string `json:"key"`
	VersionID   string `json:"version_id"`
	Size        int64  `json:"size"`
	ETag        string `json:"etag"`
	RetainUntil string `json:"retain_until"`
	WrittenAt   string `json:"written_at"`
}

// ReadObjects は objects[] を返す(項目が無ければ空)。
func ReadObjects(ctx context.Context, st contract.StateFiles) ([]ObjectRec, error) {
	raw, err := st.Get(ctx, "objects")
	if err != nil {
		return nil, err
	}
	var objs []ObjectRec
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &objs); err != nil {
			return nil, internalErr()
		}
	}
	return objs, nil
}

// RecordObject は objects[] へ key の版を足す(state.lock の短い排他の下。同じ key と版が既にあれば足さない)。呼び出し側は state.lock を持っていないこと。
func RecordObject(ctx context.Context, st contract.StateFiles, rec ObjectRec) error {
	h, err := st.Lock(ctx)
	if err != nil {
		return err
	}
	defer h.Release()
	objs, err := ReadObjects(ctx, st)
	if err != nil {
		return err
	}
	for _, o := range objs {
		if o.Key == rec.Key && o.VersionID == rec.VersionID {
			return nil
		}
	}
	b, err := json.Marshal(append(objs, rec))
	if err != nil {
		return internalErr()
	}
	return st.Update(ctx, h, "objects", b)
}

// ObjectFromPut は Put の結果から objects[] の要素を作る。
func ObjectFromPut(key string, res contract.PutResult, now time.Time) ObjectRec {
	r := ObjectRec{Key: key, VersionID: res.VersionID, Size: res.Size, ETag: res.ETag, WrittenAt: now.UTC().Format(time.RFC3339)}
	if !res.RetainUntil.IsZero() {
		r.RetainUntil = res.RetainUntil.UTC().Format(time.RFC3339)
	}
	return r
}
