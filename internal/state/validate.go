package state

// 定義ファイルの検査と正規化(spec 4.1)。指摘は DEF_INVALID の issues[](pointer と reason。値は入れない)の形で返す。

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // 実行環境にタイムゾーンの表が無くても timezone を検査できるようにする

	"gopkg.in/yaml.v3"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 指摘の reason(spec 3.1.3 の DEF_INVALID)。
const (
	rMissing   = "missing"
	rUnit      = "unit"
	rUnresolve = "unresolved_ref"
	rInvalid   = "invalid_value"
	rUnknown   = "unknown_key"
	rRootForb  = "root_forbidden"
)

// MinApprovalDelayHours は approval_delay_hours の下限(spec 3.6。通知の 6 時間 + 毎時の apply の周期 1 時間)。
const MinApprovalDelayHours = 7

type issue struct {
	Pointer string `json:"pointer"`
	Reason  string `json:"reason"`
}

var (
	refRe      = regexp.MustCompile(`^[a-z0-9-]{1,27}(-next)?$`)
	targetIDRe = regexp.MustCompile(`^t[1-9][0-9]{0,3}$`)
	declIDRe   = regexp.MustCompile(`^d[1-9][0-9]{0,3}$`)
	scheduleRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	adapterRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
)

// forbiddenExact は根にできない場所そのもの((i)(ii))、forbiddenTree はその下も含む場所((iii))。
var (
	forbiddenExact = map[string]bool{"/": true, "/etc": true, "/var": true, "/home": true, "/srv": true, "/opt": true}
	forbiddenTree  = []string{"/proc", "/sys", "/dev", "/run", "/boot", "/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/etc/ssh", "/root", "/tmp", "/var/tmp"}
)

type validator struct {
	ctx      context.Context
	s        *StateFiles
	skipRefs bool
	issues   []issue
	refs     []refUse
}

type refUse struct{ pointer, ref string }

func (v *validator) add(ptr, reason string) { v.issues = append(v.issues, issue{ptr, reason}) }

func esc(k string) string { return strings.NewReplacer("~", "~0", "/", "~1").Replace(k) }

// ValidateDef は contract.StateFiles を満たす。入力は YAML(JSON を含む)。指摘が無ければ正規化した JSON と def_hash を返す。
// 書かれた項目だけを正規化に入れ、既定値は補わない。skipRefs が真なら参照名の解決を省く。
func (s *StateFiles) ValidateDef(ctx context.Context, def []byte, skipRefs bool) ([]byte, string, json.RawMessage, error) {
	v := &validator{ctx: ctx, s: s, skipRefs: skipRefs}
	var doc any
	if err := yaml.NewDecoder(bytes.NewReader(def)).Decode(&doc); err != nil {
		v.add("", rInvalid)
		return v.result(nil)
	}
	norm, ok := toJSONValue(doc)
	root, isMap := norm.(map[string]any)
	if !ok || !isMap {
		v.add("", rInvalid)
		return v.result(nil)
	}
	v.root(root)
	if err := v.resolveRefs(); err != nil {
		return nil, "", nil, err
	}
	if err := v.checkAgainstApproved(root); err != nil {
		return nil, "", nil, err
	}
	return v.result(root)
}

func (v *validator) result(norm any) ([]byte, string, json.RawMessage, error) {
	if len(v.issues) > 0 {
		sort.SliceStable(v.issues, func(i, j int) bool { return v.issues[i].Pointer < v.issues[j].Pointer })
		b, _ := json.Marshal(v.issues)
		return nil, "", b, nil
	}
	raw, err := json.Marshal(norm)
	if err != nil {
		return nil, "", nil, internalErr()
	}
	c, err := contract.CanonicalJSON(raw)
	if err != nil {
		return nil, "", nil, internalErr()
	}
	h, err := contract.CanonicalHash(c)
	if err != nil {
		return nil, "", nil, internalErr()
	}
	return c, h, json.RawMessage("[]"), nil
}

// toJSONValue は YAML の値を JSON の値(map[string]any、[]any、string、float64、bool、nil)にする。表せなければ偽。
func toJSONValue(x any) (any, bool) {
	switch t := x.(type) {
	case map[string]any:
		m := map[string]any{}
		for k, e := range t {
			c, ok := toJSONValue(e)
			if !ok {
				return nil, false
			}
			m[k] = c
		}
		return m, true
	case []any:
		a := make([]any, len(t))
		for i, e := range t {
			c, ok := toJSONValue(e)
			if !ok {
				return nil, false
			}
			a[i] = c
		}
		return a, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint64:
		return float64(t), true
	case string, float64, bool, nil:
		return t, true
	}
	return nil, false
}

func asInt(x any) (int64, bool) {
	f, ok := x.(float64)
	if !ok || f != float64(int64(f)) {
		return 0, false
	}
	return int64(f), true
}

// obj は ptr の値が許すキーだけを持つオブジェクトであることを確かめる(余分なキーは unknown_key)。
func (v *validator) obj(ptr string, x any, allowed ...string) map[string]any {
	m, ok := x.(map[string]any)
	if !ok {
		v.add(ptr, rInvalid)
		return nil
	}
	for k := range m {
		found := false
		for _, a := range allowed {
			found = found || a == k
		}
		if !found {
			v.add(ptr+"/"+esc(k), rUnknown)
		}
	}
	return m
}

func (v *validator) str(ptr string, m map[string]any, key string, required bool) (string, bool) {
	x, ok := m[key]
	if !ok {
		if required {
			v.add(ptr+"/"+key, rMissing)
		}
		return "", false
	}
	s, ok := x.(string)
	if !ok || s == "" {
		v.add(ptr+"/"+key, rInvalid)
		return "", false
	}
	return s, true
}

func (v *validator) ref(ptr string, m map[string]any, key string, required bool) {
	s, ok := v.str(ptr, m, key, required)
	if !ok {
		return
	}
	if !refRe.MatchString(s) || (strings.HasSuffix(s, "-next") && len(s) > 32) {
		v.add(ptr+"/"+key, rInvalid)
		return
	}
	v.refs = append(v.refs, refUse{ptr + "/" + key, s})
}

func (v *validator) enum(ptr string, m map[string]any, key string, vals ...string) {
	x, ok := m[key]
	if !ok {
		return
	}
	s, _ := x.(string)
	for _, c := range vals {
		if c == s {
			return
		}
	}
	v.add(ptr+"/"+key, rInvalid)
}

// intIn は整数の項目の検査。整数でなければ unit、範囲の外は invalid_value。
func (v *validator) intIn(ptr string, m map[string]any, key string, lo, hi int64) {
	x, ok := m[key]
	if !ok {
		return
	}
	n, isInt := asInt(x)
	switch {
	case !isInt:
		v.add(ptr+"/"+key, rUnit)
	case n < lo || n > hi:
		v.add(ptr+"/"+key, rInvalid)
	}
}

func (v *validator) root(m map[string]any) {
	v.obj("", m, "version", "source", "storage", "restore", "notify", "schedule", "timezone", "restore_test",
		"approval_delay_hours", "auto_hold_days", "checks", "cost_ceiling_yen", "server_monthly_yen")
	if ver, ok := m["version"]; !ok {
		v.add("/version", rMissing)
	} else if n, isInt := asInt(ver); !isInt || n != 1 {
		v.add("/version", rInvalid)
	}
	v.source(m)
	v.storage(m)
	v.restore(m)
	v.notify(m)
	if s, ok := m["schedule"]; ok {
		if str, isStr := s.(string); !isStr || !scheduleRe.MatchString(str) {
			v.add("/schedule", rUnit)
		}
	}
	if tz, ok := m["timezone"]; ok {
		if str, isStr := tz.(string); !isStr || str == "" {
			v.add("/timezone", rInvalid)
		} else if _, err := time.LoadLocation(str); err != nil {
			v.add("/timezone", rInvalid)
		}
	}
	v.enum("", m, "restore_test", "monthly", "weekly")
	v.intIn("", m, "approval_delay_hours", MinApprovalDelayHours, 1<<31)
	v.intIn("", m, "auto_hold_days", 1, 1<<31)
	v.intIn("", m, "cost_ceiling_yen", 0, 1<<53)
	v.intIn("", m, "server_monthly_yen", 0, 1<<53)
	v.checks(m)
}

// resolveRefs は参照名が秘密ファイルとして存在することを確かめる(Refs が無いか skipRefs が真なら省く)。
func (v *validator) resolveRefs() error {
	if v.skipRefs || v.s.Refs == nil {
		return nil
	}
	for _, r := range v.refs {
		ok, err := v.s.Refs(v.ctx, r.ref)
		if err != nil {
			return err
		}
		if !ok {
			v.add(r.pointer, rUnresolve)
		}
	}
	return nil
}

// rootForbidden は files の根が根に置けない場所の (i)〜(iii) に当たるか。
func rootForbidden(p string) bool {
	if forbiddenExact[p] {
		return true
	}
	for _, f := range forbiddenTree {
		if p == f || strings.HasPrefix(p, f+"/") {
			return true
		}
	}
	return false
}

func normalAbs(p string) bool { return strings.HasPrefix(p, "/") && path.Clean(p) == p }
