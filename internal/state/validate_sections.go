package state

// 定義の checks・storage・restore・notify 節の検査(spec 4.1)。

import (
	"fmt"
	"net/url"
	"strings"
)

func (v *validator) checks(m map[string]any) {
	c, ok := m["checks"]
	if !ok {
		return
	}
	cm, ok := c.(map[string]any)
	if !ok {
		v.add("/checks", rInvalid)
		return
	}
	for name, e := range cm {
		p := "/checks/" + esc(name)
		em := v.obj(p, e, "enabled", "params")
		if em == nil {
			continue
		}
		if x, ok := em["enabled"]; ok {
			if _, isBool := x.(bool); !isBool {
				v.add(p+"/enabled", rInvalid)
			}
		}
		if x, ok := em["params"]; ok {
			if _, isMap := x.(map[string]any); !isMap {
				v.add(p+"/params", rInvalid)
			}
		}
	}
}

func (v *validator) storage(m map[string]any) {
	x, ok := m["storage"]
	if !ok {
		v.add("/storage", rMissing)
		return
	}
	arr, ok := x.([]any)
	if !ok || len(arr) > 1 {
		v.add("/storage", rInvalid)
		return
	}
	if len(arr) == 0 {
		v.add("/storage", rMissing)
		return
	}
	p := "/storage/0"
	e := v.obj(p, arr[0], "endpoint", "bucket", "key_ref", "prefix", "region", "addressing")
	if e == nil {
		return
	}
	if ep, ok := v.str(p, e, "endpoint", true); ok {
		if u, err := url.Parse(ep); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			v.add(p+"/endpoint", rInvalid)
		}
	}
	v.str(p, e, "bucket", true)
	v.ref(p, e, "key_ref", true)
	v.str(p, e, "prefix", false)
	v.str(p, e, "region", false)
	v.enum(p, e, "addressing", "path", "virtual")
}

func (v *validator) restore(m map[string]any) {
	x, ok := m["restore"]
	if !ok {
		v.add("/restore", rMissing) // 復元先を欠く定義は通さない(P4)
		return
	}
	r := v.obj("/restore", x, "adapter", "dest", "ssh_from")
	if r == nil {
		return
	}
	if a, ok := r["adapter"]; ok {
		if s, isStr := a.(string); !isStr || !adapterRe.MatchString(s) {
			v.add("/restore/adapter", rInvalid)
		}
	}
	v.str("/restore", r, "ssh_from", false)
	d, ok := r["dest"]
	if !ok {
		v.add("/restore/dest", rMissing)
		return
	}
	if dm := v.obj("/restore/dest", d, "at", "key_ref"); dm != nil {
		v.str("/restore/dest", dm, "at", true)
		v.ref("/restore/dest", dm, "key_ref", true)
	}
}

func (v *validator) notify(m map[string]any) {
	x, ok := m["notify"]
	if !ok {
		v.add("/notify", rMissing)
		return
	}
	n := v.obj("/notify", x, "to", "smtp_ref", "heartbeat_ref", "lang")
	if n == nil {
		return
	}
	to, isArr := n["to"].([]any)
	switch {
	case n["to"] == nil:
		v.add("/notify/to", rMissing)
	case !isArr || len(to) == 0:
		v.add("/notify/to", rInvalid)
	default:
		for i, e := range to {
			v.notifyTo(fmt.Sprintf("/notify/to/%d", i), e)
		}
	}
	v.ref("/notify", n, "smtp_ref", false)
	v.ref("/notify", n, "heartbeat_ref", false)
	v.enum("/notify", n, "lang", "ja", "en")
}

func (v *validator) notifyTo(p string, e any) {
	s, isStr := e.(string)
	switch {
	case !isStr || s == "":
		v.add(p, rInvalid)
	case strings.Contains(s, "@"):
		if strings.Count(s, "@") != 1 || strings.HasPrefix(s, "@") || strings.HasSuffix(s, "@") || strings.ContainsAny(s, " \t,") {
			v.add(p, rInvalid)
		}
	case !refRe.MatchString(s):
		v.add(p, rInvalid)
	default:
		v.refs = append(v.refs, refUse{p, s})
	}
}
