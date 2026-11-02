package restore

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// snapRec は state.json の snapshots[] の要素(読み取り用)。書き換えは map で行い、知らない項目を落とさない。
type snapRec struct {
	Ref             string `json:"ref"`
	VersionID       string `json:"version_id"`
	CreatedAt       string `json:"created_at"`
	StoredAt        string `json:"stored_at"`
	RetentionClass  string `json:"retention_class"`
	RetentionUntil  string `json:"retention_until"`
	Health          string `json:"health"`
	TargetsOK       bool   `json:"targets_ok"`
	RestoreVerified *bool  `json:"restore_verified"`
	RefsVerified    *bool  `json:"refs_verified"`
	RestoreFailed   bool   `json:"restore_failed"`
}

func (s snapRec) created() time.Time { t, _ := time.Parse(time.RFC3339, s.CreatedAt); return t }

// item は state.json の項目を v に読む。無い・null なら false。
func item(ctx context.Context, st contract.StateFiles, name string, v any) (bool, error) {
	raw, err := st.Get(ctx, name)
	if err != nil {
		return false, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return false, nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, internalErr("")
	}
	return true, nil
}

func loadSnapshots(ctx context.Context, st contract.StateFiles) ([]snapRec, error) {
	var s []snapRec
	_, err := item(ctx, st, "snapshots", &s)
	return s, err
}

// latestRef は restore の latest が指す世代(健全 pass かつ全対象 ok で、復元できなかった印が無い最新。spec 3.1.2.3 の流れ 4)。
func latestRef(snaps []snapRec) (snapRec, bool) {
	var best snapRec
	found := false
	for _, s := range snaps {
		if s.Health != "pass" || !s.TargetsOK || s.RestoreFailed {
			continue
		}
		if !found || s.created().After(best.created()) {
			best, found = s, true
		}
	}
	return best, found
}

// sortedByAtDesc は at(created_at)の新しい順の写し。
func sortedByAtDesc(snaps []snapRec) []snapRec {
	out := append([]snapRec(nil), snaps...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].created().After(out[j].created()) })
	return out
}

// updateSnapshots は state.json の snapshots[] の ref の要素に fn を適用して書く(state.lock の下)。
func updateSnapshots(ctx context.Context, st contract.StateFiles, held contract.StateLockHandle, ref string, fn func(map[string]any)) error {
	raw, err := st.Get(ctx, "snapshots")
	if err != nil {
		return err
	}
	var arr []map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &arr) != nil {
		return nil
	}
	changed := false
	for _, m := range arr {
		if m["ref"] == ref {
			fn(m)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	b, _ := json.Marshal(arr)
	return st.Update(ctx, held, "snapshots", b)
}

// objRec は state.json の objects[] の要素。
type objRec struct {
	Key       string `json:"key"`
	VersionID string `json:"version_id"`
	WrittenAt string `json:"written_at"`
}

// recordedObject は objects[] のうち key の最後(最新)の記録(記録した版 ID を必ず指定して読むため。spec 4.2)。
func recordedObject(ctx context.Context, st contract.StateFiles, keys ...string) (objRec, bool, error) {
	var objs []objRec
	if _, err := item(ctx, st, "objects", &objs); err != nil {
		return objRec{}, false, err
	}
	for _, key := range keys {
		for i := len(objs) - 1; i >= 0; i-- {
			if objs[i].Key == key {
				return objs[i], true, nil
			}
		}
	}
	return objRec{}, false, nil
}

// defTimezone は承認済みの定義の timezone の場所(読めなければ UTC)。
func defTimezone(def []byte) *time.Location {
	var d struct {
		Timezone string `json:"timezone"`
	}
	_ = json.Unmarshal(def, &d)
	if d.Timezone != "" {
		if loc, err := time.LoadLocation(d.Timezone); err == nil {
			return loc
		}
	}
	return time.UTC
}

func monthOf(now time.Time, loc *time.Location) string { return now.In(loc).Format("2006-01") }

func hexOfDefHash(dh string) string { return strings.TrimPrefix(dh, contract.HashPrefix) }

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
