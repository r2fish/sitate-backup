package store

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// StoreProbe は contract.StoreProbe の実物(`probe/` への書き込みと、各役割の鍵での操作の試行。spec 4.2「`bucket check` の試し方」、3.2.6 の操作の表)。
// Keys は役割(admin / write / read)ごとの ObjectStore。試す対象は PutProbe が書いた版だけ。
type StoreProbe struct {
	Keys map[string]contract.ObjectStore

	mu  sync.Mutex
	key string // 直近の PutProbe の `probe/` のキー
	ver string // その版 ID
}

var _ contract.StoreProbe = (*StoreProbe)(nil)

func (p *StoreProbe) role(name string) (contract.ObjectStore, error) {
	if p.Keys == nil {
		return nil, nilDep()
	}
	s, ok := p.Keys[name]
	if !ok || s == nil {
		return nil, newErr(contract.CodeArgInvalid, map[string]any{"flag": "--role", "reason": "unknown"})
	}
	return s, nil
}

func (p *StoreProbe) last() (string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.key, p.ver
}

// PutProbe は contract.StoreProbe を満たす。書き込み鍵で `probe/` のキーに書き、版 ID を返す(呼び出し側が書く前に書き込み予定として記録し、版 ID を index/probe に残す)。
func (p *StoreProbe) PutProbe(ctx context.Context, key string, body []byte) (string, error) {
	s, err := p.role("write")
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(key, "probe/") {
		return "", newErr(contract.CodeArgInvalid, map[string]any{"flag": "prefix", "reason": "format"})
	}
	if err := aborted(ctx); err != nil {
		return "", err
	}
	v, err := p.put(ctx, s, key, body)
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	p.key, p.ver = key, v
	p.mu.Unlock()
	return v, nil
}

// put は probe/ へ保持期限を明示せずに書く(実物の *ObjectStore)。契約の ObjectStore の偽物には保持期限なしの書き込みが無く、日次で書く(試験の道具だけ)。
func (p *StoreProbe) put(ctx context.Context, s contract.ObjectStore, key string, body []byte) (string, error) {
	if o, ok := s.(*ObjectStore); ok {
		return o.PutNoRetention(ctx, key, body)
	}
	r, err := s.Put(ctx, key, bytes.NewReader(body), contract.RetentionDaily)
	return r.VersionID, err
}

// Try は contract.StoreProbe を満たす。役割 role の鍵で op を試し、通れば allowed。拒否されたら false(権限かロックか問わない)。
// 試せる op(spec 4.2「bucket check の試し方」): 読み取り系(list_versions・get_version・get_retention・get_bucket_config)、put(PutProbe の役割)、
// retention_shorten(probe/ の版の retain-until を今の値より前へ変える試み)、legal_hold(通れば外す)、unversioned_delete(probe/ の版指定なしの削除。通れば削除マーカーが残る)。
// 試す対象は直近の PutProbe の版だけ。legal_hold と unversioned_delete は実物の *ObjectStore を要し、ほかの型は誤りで止める。ほかの op は ARG_INVALID(capabilities で判定する)。
func (p *StoreProbe) Try(ctx context.Context, role, op string) (bool, error) {
	s, err := p.role(role)
	if err != nil {
		return false, err
	}
	if err := aborted(ctx); err != nil {
		return false, err
	}
	key, ver := p.last()
	switch op {
	case "list_versions":
		return result(s.ListVersions(ctx, "probe/"))
	case "get_bucket_config":
		return result(s.BucketConfig(ctx))
	case "put":
		if key == "" {
			return false, newErr(contract.CodeArgInvalid, map[string]any{"flag": "--op", "reason": "mismatch"})
		}
		_, err := p.put(ctx, s, key, []byte("probe"))
		return allowed(err)
	case "get_version", "get_retention", "retention_shorten", "legal_hold", "unversioned_delete":
		if ver == "" {
			return false, newErr(contract.CodeArgInvalid, map[string]any{"flag": "--op", "reason": "mismatch"})
		}
		return p.onVersion(ctx, s, op, key, ver)
	}
	return false, newErr(contract.CodeArgInvalid, map[string]any{"flag": "--op", "reason": "unknown"})
}

func (p *StoreProbe) onVersion(ctx context.Context, s contract.ObjectStore, op, key, ver string) (bool, error) {
	switch op {
	case "get_version":
		rc, err := s.Get(ctx, key, ver)
		if err == nil {
			_ = rc.Close()
		}
		return allowed(err)
	case "get_retention":
		return result(s.RetainUntil(ctx, key, ver))
	case "retention_shorten":
		cur, err := s.RetainUntil(ctx, key, ver)
		if err != nil {
			return allowed(err)
		}
		return triedDenied(s.Extend(ctx, key, ver, cur.Add(-time.Hour)))
	}
	o, ok := s.(*ObjectStore) // 版指定なしの削除・リーガルホールドは probe/ 限定の実物の操作
	if !ok {
		return false, internalErr("contract")
	}
	if op == "unversioned_delete" {
		return triedDenied(o.UnversionedDelete(ctx, key))
	}
	err := o.SetLegalHold(ctx, key, ver, true)
	if err != nil {
		return triedDenied(err)
	}
	return true, o.SetLegalHold(ctx, key, ver, false) // 通ったら外して終える
}

func result[T any](_ T, err error) (bool, error) { return allowed(err) }

// triedDenied は「試す」操作(保持の短縮・版指定なしの削除・リーガルホールド)の結果を許可の真偽にする。
// 拒否されれば理由を問わず合格(spec 3.2.6)なので、権限・ロックの拒否に加え、保管先が返した 4xx(事業者ごとに InvalidArgument などで拒む)も拒否とする。
// 到達不能・取り消し・時計のずれ・5xx は誤りのまま。正当な操作(延長など)の 4xx はここを通らず誤りのまま。
func triedDenied(err error) (bool, error) {
	var ce *contract.Error
	if errors.As(err, &ce) && ce.Code == contract.CodeInternal {
		if st := httpStatusOf(ce); st >= 400 && st < 500 {
			return false, nil
		}
	}
	return allowed(err)
}

// allowed は操作の結果を許可の真偽にする。拒否は false(誤りにしない)。それ以外の誤り(到達不能・取り消し・時計のずれ)は誤りのまま返す。
func allowed(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if isDenied(err) {
		return false, nil
	}
	return false, err
}
