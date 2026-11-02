package store

// 延長の目標 A・B の対象(spec 4.2)。prune は A と B の対象を「延長の対象」として消さない。

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// extendMargin は目標 A の延長周期(7 日。spec 4.2)。
const extendMargin = 7 * 24 * time.Hour

type targetSet struct {
	a     map[objID]time.Time // 目標 A: 参照する保持中のスナップショットのうち最も遅い保持期限 + 延長周期
	b     map[objID]bool      // 目標 B: 今 + 日次の保持期間
	fails []contract.ExtendFailure
}

func (t *targetSet) addA(id objID, until time.Time) {
	if until.After(t.a[id]) {
		t.a[id] = until
	}
}

type snapView struct {
	ref, versionID           string
	retentionUntil, storedAt time.Time
}

func (s *stateRecs) snaps() []snapView {
	var out []snapView
	for _, sn := range s.snapshots {
		out = append(out, snapView{ref: sn.str("ref"), versionID: sn.str("version_id"), retentionUntil: sn.tm("retention_until"), storedAt: sn.tm("stored_at")})
	}
	return out
}

// latestByKey は prefix の記録のうち、キーごとに書き込み時刻が最も新しい版。
func (s *stateRecs) latestByKey(prefix string) []objID {
	best := map[string]rec{}
	for _, o := range s.objects {
		k := o.str("key")
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if cur, ok := best[k]; !ok || o.tm("written_at").After(cur.tm("written_at")) {
			best[k] = o
		}
	}
	var out []objID
	for k, o := range best {
		out = append(out, objID{k, o.str("version_id")})
	}
	return out
}

// latestOne は prefix の記録のうち書き込み時刻が最も新しい 1 つ(`config/` の最新版)。
func (s *stateRecs) latestOne(prefix string) (objID, bool) {
	var best rec
	for _, o := range s.objects {
		if strings.HasPrefix(o.str("key"), prefix) && (best == nil || o.tm("written_at").After(best.tm("written_at"))) {
			best = o
		}
	}
	if best == nil {
		return objID{}, false
	}
	return objID{best.str("key"), best.str("version_id")}, true
}

func (s *stateRecs) byKeyPrefix(prefix string) []objID {
	var out []objID
	for _, o := range s.objects {
		if strings.HasPrefix(o.str("key"), prefix) {
			out = append(out, objID{o.str("key"), o.str("version_id")})
		}
	}
	return out
}

// configOf は定義の写し(config/<def_hash>。保管先のキーでは接頭辞 sha256: を除く)の記録した版。
func (s *stateRecs) configOf(defHash string) []objID {
	h := strings.TrimPrefix(defHash, "sha256:")
	if h == "" {
		return nil
	}
	return s.byKeyPrefix("config/" + h)
}

// targets は今(Date + オフセット)の A・B の対象を求める。マニフェストを引けない世代・記録が読めない世代は、strict なら誤りで返し、そうでなければ
// fails に載せて残りを続ける。参照を一部しか引けなかった世代も、引けた範囲は対象に入れる。
func (r *Retention) targets(ctx context.Context, s *stateRecs, now time.Time, strict bool) (*targetSet, error) {
	t := &targetSet{a: map[objID]time.Time{}, b: map[objID]bool{}}
	usedBin := map[string]bool{}
	oldest := contract.AddMonthsUTC(now, -contract.RetentionMonthlyMonths)
	for _, sv := range s.snaps() {
		if sv.ref == "" || sv.versionID == "" || sv.retentionUntil.IsZero() { // 読めない世代を期限切れ扱いにしない(保持中の世代が消える)
			if strict {
				return nil, internalErr("contract")
			}
			t.fails = append(t.fails, contract.ExtendFailure{Key: "manifests/" + sv.ref, VersionID: sv.versionID, Code: contract.CodeInternal})
			continue
		}
		retained := sv.retentionUntil.After(now)
		prot := s.protected[sv.ref]
		if !retained && !prot {
			continue
		}
		if !sv.storedAt.IsZero() && sv.storedAt.Before(oldest) {
			oldest = sv.storedAt
		}
		info, err := r.walk(ctx, sv.ref, sv.versionID)
		if err != nil {
			if ab := aborted(ctx); ab != nil {
				return nil, ab
			}
			if strict {
				return nil, err
			}
			t.fails = append(t.fails, contract.ExtendFailure{Key: "manifests/" + sv.ref, VersionID: sv.versionID, Code: codeOf(err)})
		}
		mid := objID{"manifests/" + sv.ref, sv.versionID}
		usedBin[info.BinaryVersion] = true
		if retained {
			until := sv.retentionUntil.Add(extendMargin)
			t.addA(mid, until)
			for _, ref := range info.Refs {
				t.addA(ref, until)
			}
			for _, c := range s.configOf(info.DefHash) {
				t.addA(c, until)
			}
		}
		if prot {
			t.b[mid] = true
			for _, ref := range info.Refs {
				t.b[ref] = true
			}
			for _, c := range s.configOf(info.DefHash) {
				t.b[c] = true
			}
		}
	}
	if err := r.sharedB(ctx, s, t, usedBin, oldest); err != nil {
		return nil, err
	}
	return t, nil
}

// sharedB は共有物(キーリングの最新版、repo.json、config/ の最新版、bin/、延長の範囲の journal/)を目標 B に足す。
func (r *Retention) sharedB(ctx context.Context, s *stateRecs, t *targetSet, usedBin map[string]bool, journalSince time.Time) error {
	for _, id := range s.latestByKey("keyring/") {
		t.b[id] = true
	}
	for _, id := range s.latestByKey("repo.json") {
		t.b[id] = true
	}
	if id, ok := s.latestOne("config/"); ok {
		t.b[id] = true
	}
	var kit struct {
		Confirmed *string `json:"confirmed_core_version"`
	}
	if err := r.readState(ctx, "kit", &kit); err != nil {
		return err
	}
	if kit.Confirmed != nil {
		usedBin[*kit.Confirmed] = true
	}
	if r.BinVersion != "" {
		usedBin[r.BinVersion] = true
	}
	for _, id := range s.byKeyPrefix("bin/") {
		// 今の核の版が分からなければ(BinVersion が空)すべての bin/ を共有物として扱い、消さない。
		if r.BinVersion == "" || usedBin[binVersionOf(id.Key)] {
			t.b[id] = true
		}
	}
	for _, o := range s.objects {
		if strings.HasPrefix(o.str("key"), "journal/") && !o.tm("written_at").Before(journalSince) {
			t.b[objID{o.str("key"), o.str("version_id")}] = true
		}
	}
	return nil
}

// binVersionOf は `bin/<version>/...` の <version>。
func binVersionOf(key string) string {
	rest := strings.TrimPrefix(key, "bin/")
	if i := strings.Index(rest, "/"); i >= 0 {
		return rest[:i]
	}
	return rest
}

func (t *targetSet) sortedA() []objID {
	var ids []objID
	for id := range t.a {
		ids = append(ids, id)
	}
	sortIDs(ids)
	return ids
}

func sortIDs(ids []objID) {
	sort.Slice(ids, func(i, j int) bool {
		if ids[i].Key != ids[j].Key {
			return ids[i].Key < ids[j].Key
		}
		return ids[i].VersionID < ids[j].VersionID
	})
}
