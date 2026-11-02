package store

import (
	"context"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Prune は contract.Retention を満たす。保持期限を過ぎたスナップショット(保護中の世代を除く)のマニフェストと、どの保持中・保護中のマニフェストからも
// 参照されず retain-until を過ぎた版、延長の対象から外れた記録した版、記録の無い版(retain-until を過ぎたもの)を、版 ID を指定して消す(spec 4.2)。
// 延長の対象(目標 A・B)の版は retain-until を過ぎていても消さない。削除停止中と seal の前(sealed が JSON の true でない)は何もしない。
// 各削除の直前に削除停止を確かめ直す(規則 18)。参照を引けない世代や読めない記録があるときは、消してよい版が分からないので何も消さずに誤りを返す。
// 消した chunks/ の版は Index から外す。Index が nil で chunks/ を消す場面は、消す前に INTERNAL で止まる。
func (r *Retention) Prune(ctx context.Context) (contract.PruneResult, error) {
	var res contract.PruneResult
	if err := r.ready(); err != nil {
		return res, err
	}
	if err := aborted(ctx); err != nil {
		return res, err
	}
	if err := r.Store.CheckSkew(ctx); err != nil {
		return res, err
	}
	s, err := r.loadRecs(ctx)
	if err != nil {
		return res, err
	}
	if !s.sealed {
		return res, newErr(contract.CodeNotSealed, nil)
	}
	if h, err := r.readHold(ctx); err != nil || h.Active {
		res.Held = err == nil
		return res, err
	}
	now, err := r.now(ctx)
	if err != nil {
		return res, err
	}
	ts, err := r.targets(ctx, s, now, true)
	if err != nil {
		return res, err
	}
	vs, err := r.Store.ListVersions(ctx, "")
	if err != nil {
		return res, err
	}
	for _, sv := range s.snaps() {
		if s.protected[sv.ref] && !sv.retentionUntil.After(now) {
			res.Protected++
		}
	}
	gone := map[objID]bool{}
	err = r.pruneEach(ctx, vs, ts, now, &res, gone)
	if len(gone) > 0 {
		rc, cancel := recordCtx(ctx)
		defer cancel()
		if werr := r.mutate(rc, func(s *stateRecs) { s.drop(gone) }, nil); werr != nil && err == nil {
			err = werr
		}
	}
	return res, err
}

// expired は版の retain-until が今以前か。読めなければ誤り(ゼロで期限切れ扱いにしない)。保持の無い版(ゼロ)は期限切れ。版が無ければ skip。
func (r *Retention) expired(ctx context.Context, v contract.VersionInfo, now time.Time) (expired, skip bool, err error) {
	until := v.RetainUntil
	if until.IsZero() {
		u, err := r.currentRetain(ctx, v.Key, v.VersionID)
		if isMissing(err) {
			return false, true, nil
		}
		if err != nil {
			return false, false, err
		}
		until = u
	}
	return !until.After(now), false, nil
}

func (r *Retention) pruneEach(ctx context.Context, vs []contract.VersionInfo, ts *targetSet, now time.Time, res *contract.PruneResult, gone map[objID]bool) error {
	minAge := time.Duration(r.params().DailyDays()) * 24 * time.Hour
	for _, v := range vs {
		id := objID{v.Key, v.VersionID}
		if ts.b[id] || !ts.a[id].IsZero() {
			continue // 延長の対象は消さない
		}
		if v.DeleteMarker {
			if !hasPrefix(v.Key, "probe/") {
				continue // probe/ の削除マーカーだけを消す(bucket check の版指定なしの削除の試しで残る。spec 4.2)
			}
		} else {
			if now.Sub(v.LastModified) < minAge {
				continue // 書いたばかりの版は保持期限の内(読み取りを省く)
			}
			exp, skip, err := r.expired(ctx, v, now)
			if err != nil {
				return err
			}
			if skip || !exp {
				continue
			}
		}
		if strings.HasPrefix(v.Key, "chunks/") && r.Index == nil {
			return nilDep() // 索引から外せないまま chunks/ を消さない
		}
		if err := aborted(ctx); err != nil {
			return err
		}
		if h, err := r.readHold(ctx); err != nil || h.Active { // 各削除の直前に削除停止を確かめ直す
			res.Held = err == nil
			return err
		}
		if err := r.deleteOne(ctx, v, res, gone); err != nil {
			return err
		}
	}
	return nil
}

func (r *Retention) deleteOne(ctx context.Context, v contract.VersionInfo, res *contract.PruneResult, gone map[objID]bool) error {
	err := r.Store.DeleteVersion(ctx, v.Key, v.VersionID)
	switch {
	case err == nil:
		res.Deleted++
	case isMissing(err):
	case isDenied(err):
		res.SkippedLocked++ // 保管先のロックが生きている(核の「今」より保管先の判定が遅い)。誤りにせず続ける
		return nil
	default:
		return err
	}
	gone[objID{v.Key, v.VersionID}] = true
	if strings.HasPrefix(v.Key, "chunks/") {
		return r.Index.Remove(ctx, v.VersionID)
	}
	return nil
}
