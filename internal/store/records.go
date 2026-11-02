package store

// state.json の objects[]・snapshots[] の読み書き。知らない項目を落とさないよう、要素は項目名から生の JSON への写像で持つ。

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type objID struct{ Key, VersionID string }

type rec map[string]json.RawMessage

func (x rec) str(k string) string {
	var s string
	_ = json.Unmarshal(x[k], &s)
	return s
}

func (x rec) tm(k string) time.Time {
	t, err := time.Parse(time.RFC3339, x.str(k))
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func (x rec) setTime(k string, t time.Time) {
	b, _ := json.Marshal(t.UTC().Format(time.RFC3339))
	x[k] = b
}

func (x rec) boolean(k string) bool {
	var v bool
	_ = json.Unmarshal(x[k], &v)
	return v
}

// stateRecs は objects[] と snapshots[] の読んだ写し。
type stateRecs struct {
	objects, snapshots []rec
	protected          map[string]bool // 保護中の世代の ref
	sealed             bool            // 項目が JSON の true のときだけ真(無い・null・ほかの値は偽)
}

func (r *Retention) loadRecs(ctx context.Context) (*stateRecs, error) {
	s := &stateRecs{protected: map[string]bool{}}
	if err := r.readState(ctx, "objects", &s.objects); err != nil {
		return nil, err
	}
	if err := r.readState(ctx, "snapshots", &s.snapshots); err != nil {
		return nil, err
	}
	var prot []struct {
		Ref string `json:"ref"`
	}
	if err := r.readState(ctx, "protected", &prot); err != nil {
		return nil, err
	}
	for _, p := range prot {
		s.protected[p.Ref] = true
	}
	raw, err := r.State.Get(ctx, "sealed")
	if err != nil {
		return nil, err
	}
	s.sealed = strings.TrimSpace(string(raw)) == "true"
	for _, o := range s.objects { // 読めない記録は安全でない側(期限切れ・範囲外)に倒れるので、先に止める
		if o.str("key") == "" || o.str("version_id") == "" || o.tm("retain_until").IsZero() || o.tm("written_at").IsZero() {
			return nil, internalErr("contract")
		}
	}
	return s, nil
}

// recorded は記録した retain-until(版ごと)。objects[] と、マニフェスト(snapshots[] の version_id)。
func (s *stateRecs) recorded() map[objID]time.Time {
	m := map[objID]time.Time{}
	for _, o := range s.objects {
		m[objID{o.str("key"), o.str("version_id")}] = o.tm("retain_until")
	}
	for _, sn := range s.snapshots {
		m[objID{"manifests/" + sn.str("ref"), sn.str("version_id")}] = sn.tm("retain_until")
	}
	return m
}

// setRetain は延ばした版の記録の retain-until を置き換える。
func (s *stateRecs) setRetain(id objID, t time.Time) {
	for _, o := range s.objects {
		if o.str("key") == id.Key && o.str("version_id") == id.VersionID {
			o.setTime("retain_until", t)
		}
	}
	for _, sn := range s.snapshots {
		if "manifests/"+sn.str("ref") == id.Key && sn.str("version_id") == id.VersionID {
			sn.setTime("retain_until", t)
		}
	}
}

// drop は削除した版を記録から外す。
func (s *stateRecs) drop(gone map[objID]bool) {
	keep := s.objects[:0:0]
	for _, o := range s.objects {
		if !gone[objID{o.str("key"), o.str("version_id")}] {
			keep = append(keep, o)
		}
	}
	s.objects = keep
	ks := s.snapshots[:0:0]
	for _, sn := range s.snapshots {
		if !gone[objID{"manifests/" + sn.str("ref"), sn.str("version_id")}] {
			ks = append(ks, sn)
		}
	}
	s.snapshots = ks
}

func hasPrefix(k string, p ...string) bool {
	for _, x := range p {
		if strings.HasPrefix(k, x) {
			return true
		}
	}
	return false
}
