package approval

// 定義の差分を spec 4.1 の任意項目の表の「変更の種別」に分ける純粋な関数(spec 3.6 の def-change、A6 (12))。
// 定義を受ける種別(def-change・def-change-unlisted・checks-weaken)は、自分の種別の項目だけを変えられる。

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 差分の種別(提案の種別の名前。表に種別が無い項目は kindNone)。
const (
	kindNone     = "none"
	kindUnlisted = "def-change-unlisted"
)

// defGroup は同じ種別に当たる差分の JSON Pointer。
type defGroup struct {
	Kind     string   `json:"kind"`
	Pointers []string `json:"pointers"`
}

// 値だけで種別が決まる項目(4.1 の表で def-change / 0 のもの)。
var defChangePaths = []string{"/schedule", "/timezone", "/storage/0/region", "/storage/0/addressing", "/notify/lang", "/cost_ceiling_yen", "/server_monthly_yen"}

func flatten(raw []byte) (map[string]string, error) {
	out := map[string]string{}
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	var walk func(p string, v any)
	walk = func(p string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				walk(p+"/"+strings.NewReplacer("~", "~0", "/", "~1").Replace(k), c)
			}
			if len(x) == 0 && p != "" {
				out[p] = "{}"
			}
		case []any:
			for i, c := range x {
				walk(p+"/"+itoa(i), c)
			}
			if len(x) == 0 && p != "" {
				out[p] = "[]"
			}
		default:
			b, _ := json.Marshal(x)
			out[p] = string(b)
		}
	}
	walk("", v)
	return out, nil
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func changedPaths(before, after []byte) ([]string, error) {
	b, err := flatten(before)
	if err != nil {
		return nil, err
	}
	a, err := flatten(after)
	if err != nil {
		return nil, err
	}
	var out []string
	for p, v := range a {
		if bv, ok := b[p]; !ok || bv != v {
			out = append(out, p)
		}
	}
	for p := range b {
		if _, ok := a[p]; !ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

func underPrefix(p string, roots ...string) bool {
	for _, r := range roots {
		if p == r || strings.HasPrefix(p, r+"/") {
			return true
		}
	}
	return false
}

func sectionOf(raw []byte, key string) []byte {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m[key]
}

// classifyDefDiff は before から after への差分を種別ごとの群に分ける。
func classifyDefDiff(before, after []byte) ([]defGroup, error) {
	paths, err := changedPaths(before, after)
	if err != nil {
		return nil, err
	}
	by := map[string][]string{}
	add := func(kind, p string) { by[kind] = append(by[kind], p) }
	var notifyTo, checks []string
	for _, p := range paths {
		switch {
		case underPrefix(p, defChangePaths...):
			add("def-change", p)
		case p == "/auto_hold_days":
			if holdDaysOf(after) > holdDaysOf(before) {
				add("checks-weaken", p)
			} else {
				add("def-change", p)
			}
		case underPrefix(p, "/checks"):
			checks = append(checks, p)
		case p == "/approval_delay_hours":
			add("delay-change", p)
		case p == "/restore_test":
			add(freqKind(before, after), p)
		case underPrefix(p, "/restore"):
			add("restore-change", p)
		case underPrefix(p, "/source"):
			add("source-change", p)
		case underPrefix(p, "/notify"):
			notifyTo = append(notifyTo, p)
		case underPrefix(p, "/storage"):
			add(kindNone, p)
		default:
			add(kindUnlisted, p)
		}
	}
	if len(checks) > 0 {
		w, err := ChecksWeaken(wrapChecks(before), wrapChecks(after))
		if err != nil {
			return nil, err
		}
		k := "def-change"
		if w {
			k = "checks-weaken"
		}
		by[k] = append(by[k], checks...)
	}
	if len(notifyTo) > 0 {
		k := notifyKind(before, after)
		by[k] = append(by[k], notifyTo...)
	}
	return groupsOf(by), nil
}

func wrapChecks(raw []byte) []byte {
	b, _ := json.Marshal(map[string]json.RawMessage{"checks": sectionOf(raw, "checks")})
	return b
}

func holdDaysOf(raw []byte) float64 {
	d, _ := parseChecks(raw)
	return holdDays(d)
}

func freqKind(before, after []byte) string {
	rank := func(raw []byte) int {
		var d struct {
			R string `json:"restore_test"`
		}
		_ = json.Unmarshal(raw, &d)
		if freqRank[d.R] == 0 {
			return freqRank["monthly"]
		}
		return freqRank[d.R]
	}
	if rank(after) > rank(before) {
		return "restore-test-increase"
	}
	return "restore-test-reduce"
}

// notifyKind: 変更前が変更後の部分集合(足すだけ)なら notify-add、削除・差し替えを含めば notify-change。
func notifyKind(before, after []byte) string {
	var b, a struct {
		Notify notifySection `json:"notify"`
	}
	_ = json.Unmarshal(before, &b)
	_ = json.Unmarshal(after, &a)
	for _, t := range b.Notify.To {
		if !contains(a.Notify.To, t) {
			return "notify-change"
		}
	}
	keep := func(x, y *string) bool { return x == nil || eqPtr(x, y) }
	if !keep(b.Notify.SmtpRef, a.Notify.SmtpRef) || !keep(b.Notify.HeartbeatRef, a.Notify.HeartbeatRef) {
		return "notify-change"
	}
	return "notify-add"
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func groupsOf(by map[string][]string) []defGroup {
	kinds := make([]string, 0, len(by))
	for k := range by {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	out := make([]defGroup, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, defGroup{Kind: k, Pointers: by[k]})
	}
	return out
}

// allowedDefKinds は定義を受ける種別が差分に持てる種別: def-change は def-change だけ。
// def-change-unlisted と checks-weaken は自分の種別と def-change(遅延が掛かる側に安全な項目が混じるだけ)。
func allowedDefKinds(op string) []string {
	switch op {
	case "def-change":
		return []string{"def-change"}
	case "def-change-unlisted":
		return []string{kindUnlisted, "def-change"}
	case "checks-weaken":
		return []string{"checks-weaken", "def-change"}
	}
	return nil
}

// checkDefDiff は範囲外の種別の項目が混じる差分を DEF_CHANGE_MIXED(groups[])にする。
func checkDefDiff(op string, before, after []byte) error {
	groups, err := classifyDefDiff(before, after)
	if err != nil {
		return internalErr()
	}
	allowed := allowedDefKinds(op)
	var bad []defGroup
	for _, g := range groups {
		if !contains(allowed, g.Kind) {
			bad = append(bad, g)
		}
	}
	if len(bad) > 0 {
		return apiErr(contract.CodeDefChangeMixed, map[string]any{"groups": bad})
	}
	return nil
}
