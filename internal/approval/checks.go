package approval

// 変更の種別の判定 checks-weaken(spec 3.6 の checks-weaken の補足、4.1 の checks。D-0115 の 4、D-0118 の 1):
// 緩和か否かを変更の前後の実効値(定義の値か、無ければ閾値の定数)で比べる。

import (
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 既定の自動停止の日数(spec 4.1 の auto_hold_days。定義に書かないときの値)。
const defaultAutoHoldDays = 14

// params のキーと厳しさの向き。anyChange は値が変われば向きを問わず緩和、そのほかは大きいほど緩い(小さいほど厳しい)。
type paramRule struct {
	key       string
	def       float64
	anyChange bool
}

var paramRules = []paramRule{
	{"entropy_min_file_bytes", contract.EntropyMinFileBytes, true},
	{"entropy_sample_bytes", contract.EntropySampleBytes, true},
	{"entropy_high_bits", contract.EntropyHighBits, false},
	{"entropy_min_files", contract.EntropyMinFiles, false},
	{"entropy_ratio_warn", contract.EntropyRatioWarn, false},
	{"entropy_ratio_fail", contract.EntropyRatioFail, false},
}

type checkDef struct {
	Enabled *bool              `json:"enabled"`
	Params  map[string]float64 `json:"params"`
}

type checksDoc struct {
	Checks       map[string]checkDef `json:"checks"`
	AutoHoldDays *float64            `json:"auto_hold_days"`
}

func (c checkDef) enabled() bool { return c.Enabled == nil || *c.Enabled }

func (c checkDef) param(r paramRule) float64 {
	if v, ok := c.Params[r.key]; ok {
		return v
	}
	return r.def
}

// ChecksWeaken は、承認済みの定義 before から提案の定義 after への変更が checks-weaken にあたるかを返す
// (チェックの無効化、閾値の緩和、auto_hold_days の延長)。before・after は正規化した定義の JSON で、空は定義なしとして扱う。
// キーの削除は定数への変更として比べる。
func ChecksWeaken(before, after []byte) (bool, error) {
	b, err := parseChecks(before)
	if err != nil {
		return false, err
	}
	a, err := parseChecks(after)
	if err != nil {
		return false, err
	}
	for name := range union(b.Checks, a.Checks) {
		if checkWeakened(b.Checks[name], a.Checks[name]) {
			return true, nil
		}
	}
	return holdDays(a) > holdDays(b), nil
}

func parseChecks(raw []byte) (checksDoc, error) {
	var d checksDoc
	if len(raw) == 0 || string(raw) == "null" {
		return d, nil
	}
	return d, json.Unmarshal(raw, &d)
}

func union(a, b map[string]checkDef) map[string]struct{} {
	u := map[string]struct{}{}
	for k := range a {
		u[k] = struct{}{}
	}
	for k := range b {
		u[k] = struct{}{}
	}
	return u
}

func holdDays(d checksDoc) float64 {
	if d.AutoHoldDays == nil {
		return defaultAutoHoldDays
	}
	return *d.AutoHoldDays
}

func checkWeakened(before, after checkDef) bool {
	if before.enabled() && !after.enabled() {
		return true
	}
	for _, r := range paramRules {
		bv, av := before.param(r), after.param(r)
		if (r.anyChange && av != bv) || (!r.anyChange && av > bv) {
			return true
		}
	}
	return false
}
