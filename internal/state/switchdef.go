package state

// 承認済みの定義の切り替え(spec 3.1.2.11 の define、4.1、4.5、7.5 StateFiles。D-0118、D-0119)。
// state.lock の下で、索引の削除 → 承認済みの定義の置き換え(def/approved.json → state.json)→ 候補の削除、の順に行う。
// どの段も自分の比較で行い、今の値と同じなら何もしない(冪等)。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func argFormat() *contract.Error {
	return newErr(contract.CodeArgInvalid, map[string]any{"flag": "--file", "reason": "format"})
}

// SwitchDef は contract.StateFiles を満たす。
func (s *StateFiles) SwitchDef(ctx context.Context, in contract.SwitchInput) error {
	if len(in.Normalized) == 0 || !json.Valid(in.Normalized) {
		return argFormat()
	}
	var newDef map[string]any
	if json.Unmarshal(in.Normalized, &newDef) != nil || newDef == nil {
		return argFormat()
	}
	hash, err := contract.CanonicalHash(in.Normalized)
	if err != nil {
		return argFormat()
	}
	held, err := s.Lock(ctx)
	if err != nil {
		return err
	}
	defer held.Release()
	st, err := s.readState()
	if err != nil {
		return err
	}
	cur, hasCur, err := s.readDefFile("approved.json")
	if err != nil {
		return err
	}
	var oldDef map[string]any
	if hasCur {
		_ = json.Unmarshal(cur.Normalized, &oldDef)
	}
	for _, id := range s.indexToDrop(oldDef, newDef, st, in.HostKeys) {
		if err := os.Remove(s.path("index", "files", id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return internalErr()
		}
	}
	changed, err := s.replaceApproved(ctx, st, in.Normalized, hash, cur, hasCur)
	if err != nil {
		return err
	}
	changed = updateHostKeys(st, in.HostKeys) || changed
	changed = applyEnroll(st, in.Enroll) || changed
	if changed {
		if err := s.writeState(st); err != nil {
			return err
		}
	}
	if err := os.Remove(s.path("def", "candidate.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return internalErr()
	}
	return nil
}

// replaceApproved は承認済みの定義の置き換え。approved.json と state.json の approved_def の両方が提案と一致していれば飛ばす。
// seal(sealed が真でない)は同じ置き換えで sealed と sealed_at も書く。st を書き換えたかを返す(書き出しは呼び出し側)。
func (s *StateFiles) replaceApproved(ctx context.Context, st map[string]json.RawMessage, norm []byte, hash string, cur defFile, hasCur bool) (bool, error) {
	var ad struct {
		DefHash string `json:"def_hash"`
	}
	_ = json.Unmarshal(st["approved_def"], &ad)
	sealed := string(st["sealed"]) == "true"
	if hasCur && cur.DefHash == hash && ad.DefHash == hash && sealed {
		return false, nil
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		t, err := s.Clock.Now(ctx)
		if err != nil {
			return false, err
		}
		now = t
	}
	ts := now.UTC().Format(time.RFC3339)
	b, _ := json.Marshal(defFile{DefHash: hash, Normalized: norm})
	if err := s.writeAtomic(s.path("def", "approved.json"), append(b, '\n')); err != nil {
		return false, internalErr()
	}
	st["approved_def"], _ = json.Marshal(map[string]string{"def_hash": hash, "approved_at": ts})
	if !sealed {
		st["sealed"] = json.RawMessage("true")
		st["sealed_at"], _ = json.Marshal(ts)
	}
	return true, nil
}

// filesTargets は定義の files の対象の id → 根。
func filesTargets(def map[string]any) map[string]string {
	out := map[string]string{}
	for _, t := range targetList(def) {
		if root, ok := t["files"].(string); ok {
			id, _ := t["id"].(string)
			out[id] = root
		}
	}
	return out
}

func sourceHost(def map[string]any) string {
	src, _ := def["source"].(map[string]any)
	h, _ := src["host"].(string)
	return h
}

// indexToDrop は切り替えの前に消す index/files/<id> の id(昇順)。外した id と根が変わった id は一覧と無関係に、
// 所在かホスト鍵が変わったときはその元サーバーのすべての files の id。承認済みの定義が無い(seal)なら消すものは無い。
func (s *StateFiles) indexToDrop(oldDef, newDef map[string]any, st map[string]json.RawMessage, hk []contract.HostKeyUpdate) []string {
	if oldDef == nil {
		return nil
	}
	oldT, newT := filesTargets(oldDef), filesTargets(newDef)
	drop := map[string]bool{}
	for id, root := range oldT {
		if nr, kept := newT[id]; !kept || nr != root {
			drop[id] = true
		}
	}
	moved := sourceHost(oldDef) != sourceHost(newDef) || sourceFPChanged(st, hk)
	if moved {
		for id := range oldT {
			drop[id] = true
		}
		for id := range newT {
			drop[id] = true
		}
	}
	out := make([]string, 0, len(drop))
	for id := range drop {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// sourceFPChanged は入力の role: source の要素の host_key_fp が host_keys[] の今の値と違うか(nil の要素と一覧が空は比べない)。
func sourceFPChanged(st map[string]json.RawMessage, hk []contract.HostKeyUpdate) bool {
	cur := hostKeys(st)
	for _, u := range hk {
		if u.Role != "source" || u.HostKeyFP == nil {
			continue
		}
		have := false
		for _, e := range cur {
			if e["role"] == "source" {
				have = true
				if e["host_key_fp"] != *u.HostKeyFP {
					return true
				}
			}
		}
		if !have {
			return true
		}
	}
	return false
}

func hostKeys(st map[string]json.RawMessage) []map[string]any {
	var arr []map[string]any
	_ = json.Unmarshal(st["host_keys"], &arr)
	return arr
}

// updateHostKeys は一覧の要素を host_keys[] の同じ role の要素の host と host_key_fp に書く(無ければ足す)。nil の fp の要素は書かない。
func updateHostKeys(st map[string]json.RawMessage, hk []contract.HostKeyUpdate) bool {
	arr := hostKeys(st)
	changed := false
	for _, u := range hk {
		if u.HostKeyFP == nil {
			continue
		}
		found := false
		for _, e := range arr {
			if e["role"] != u.Role {
				continue
			}
			found = true
			if e["host"] != u.Host || e["host_key_fp"] != *u.HostKeyFP {
				e["host"], e["host_key_fp"] = u.Host, *u.HostKeyFP
				changed = true
			}
		}
		if !found {
			arr = append(arr, map[string]any{"role": u.Role, "host": u.Host, "host_key_fp": *u.HostKeyFP, "session_id": nil})
			changed = true
		}
	}
	if changed {
		st["host_keys"], _ = json.Marshal(arr)
	}
	return changed
}

// applyEnroll は登録の結果を receiver_hash・source_clusters・restore_dest_enrollment に書く(同じ値なら書かない)。
func applyEnroll(st map[string]json.RawMessage, e *contract.EnrollResult) bool {
	if e == nil {
		return false
	}
	changed := false
	set := func(item string, v json.RawMessage) {
		if len(v) == 0 || string(st[item]) == string(v) {
			return
		}
		st[item] = v
		changed = true
	}
	if e.ReceiverHash != "" {
		h, _ := json.Marshal(e.ReceiverHash)
		set("receiver_hash", h)
	}
	set("source_clusters", e.SourceClusters)
	set("restore_dest_enrollment", e.RestoreDestEnroll)
	return changed
}
