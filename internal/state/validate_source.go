package state

// 定義の source 節の検査(spec 4.1 の #1・#2 と source.declined)と、承認済みの定義との比べ(id の種別の変更)。

import (
	"encoding/json"
	"fmt"
)

func (v *validator) source(m map[string]any) {
	x, ok := m["source"]
	if !ok {
		v.add("/source", rMissing)
		return
	}
	sm := v.obj("/source", x, "host", "targets", "declined")
	if sm == nil {
		return
	}
	v.str("/source", sm, "host", true)
	roots := v.targets(sm)
	v.declined(sm, roots)
}

// targets は source.targets を検査し、files の根の集合を返す。
func (v *validator) targets(sm map[string]any) map[string]bool {
	roots := map[string]bool{}
	tx, ok := sm["targets"]
	if !ok {
		v.add("/source/targets", rMissing)
		return roots
	}
	arr, ok := tx.([]any)
	if !ok || len(arr) == 0 {
		v.add("/source/targets", rInvalid)
		return roots
	}
	seen := map[string]bool{}
	for i, e := range arr {
		p := fmt.Sprintf("/source/targets/%d", i)
		t := v.obj(p, e, "id", "files", "postgresql", "port")
		if t == nil {
			continue
		}
		if id, ok := v.str(p, t, "id", true); ok {
			if !targetIDRe.MatchString(id) || seen[id] {
				v.add(p+"/id", rInvalid)
			}
			seen[id] = true
		}
		v.targetKind(p, t, roots)
	}
	return roots
}

func (v *validator) targetKind(p string, t map[string]any, roots map[string]bool) {
	f, hasF := t["files"]
	_, hasP := t["postgresql"]
	switch {
	case hasF && hasP:
		v.add(p, rInvalid)
	case !hasF && !hasP:
		v.add(p, rMissing)
	case hasF:
		if root, ok := f.(string); !ok || !normalAbs(root) {
			v.add(p+"/files", rInvalid)
		} else if rootForbidden(root) {
			v.add(p+"/files", rRootForb)
		} else {
			roots[root] = true
		}
	default:
		v.str(p, t, "postgresql", true)
	}
	if _, hasPort := t["port"]; hasPort {
		if !hasP {
			v.add(p+"/port", rUnknown)
		} else {
			v.intIn(p, t, "port", 1, 65535)
		}
	}
}

func (v *validator) declined(sm map[string]any, roots map[string]bool) {
	dx, ok := sm["declined"]
	if !ok {
		return
	}
	arr, ok := dx.([]any)
	if !ok {
		v.add("/source/declined", rInvalid)
		return
	}
	seen := map[string]bool{}
	for i, e := range arr {
		p := fmt.Sprintf("/source/declined/%d", i)
		d := v.obj(p, e, "id", "mount", "postgresql_cluster")
		if d == nil {
			continue
		}
		if id, ok := v.str(p, d, "id", true); ok {
			if !declIDRe.MatchString(id) || seen[id] {
				v.add(p+"/id", rInvalid)
			}
			seen[id] = true
		}
		mnt, hasM := d["mount"]
		_, hasC := d["postgresql_cluster"]
		switch {
		case hasM && hasC:
			v.add(p, rInvalid)
		case !hasM && !hasC:
			v.add(p, rMissing)
		case hasM:
			if root, ok := mnt.(string); !ok || !normalAbs(root) || roots[root] {
				v.add(p+"/mount", rInvalid)
			}
		default:
			v.intIn(p, d, "postgresql_cluster", 1, 65535)
		}
	}
}

// checkAgainstApproved は承認済みの定義にある対象 id の種別を変える定義を DEF_INVALID にする(spec 4.1)。
func (v *validator) checkAgainstApproved(cur map[string]any) error {
	d, ok, err := v.s.readDefFile("approved.json")
	if err != nil || !ok {
		return err
	}
	old := targetKinds(d.Normalized)
	for i, e := range targetList(cur) {
		id, _ := e["id"].(string)
		if prev, found := old[id]; found && prev != kindOf(e) {
			v.add(fmt.Sprintf("/source/targets/%d", i), rInvalid)
		}
	}
	return nil
}

func targetList(m map[string]any) []map[string]any {
	src, _ := m["source"].(map[string]any)
	arr, _ := src["targets"].([]any)
	var out []map[string]any
	for _, e := range arr {
		t, _ := e.(map[string]any)
		out = append(out, t)
	}
	return out
}

func kindOf(t map[string]any) string {
	if _, ok := t["files"]; ok {
		return "files"
	}
	return "postgresql"
}

func targetKinds(normalized []byte) map[string]string {
	out := map[string]string{}
	var m map[string]any
	if json.Unmarshal(normalized, &m) != nil {
		return out
	}
	for _, t := range targetList(m) {
		if id, _ := t["id"].(string); id != "" {
			out[id] = kindOf(t)
		}
	}
	return out
}
