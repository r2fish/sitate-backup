package approval

// 提案の種別ごとの引数・遅延・前提の材料・適用の操作の列(spec 3.1.2.9 の表、3.6 の表)。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strconv"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 既定の遅延時間(spec 4.1 の approval_delay_hours の既定値。定義に書かないときの値)。下限は 3.6 の注記。
const (
	defaultDelayHours = 24
	minDelayHours     = 7
	// delayHoursMax は approval_delay_hours の上限(spec 3.6、4.1。定数表の APPROVAL_DELAY_HOURS_MAX)。
	delayHoursMax = contract.ApprovalDelayHoursMax
)

type kindSpec struct {
	flags    []string // 受ける引数(--を除く)
	required []string
	// delayZero は遅延 0 で適用する種別(3.6 の 11 個と attest)。
	delayZero bool
	section   string // 節を変える種別の節の名前(前提は節のハッシュで、def_hash を含めない)
	noDefHash bool   // key-rotate 系: 前提に def_hash を含めない
	defFile   bool   // --file で定義を受ける
}

var kinds = map[string]kindSpec{
	"storage-init":           {flags: []string{"endpoint", "bucket", "prefix", "adapter", "reissue"}, delayZero: true},
	"read-key-issue":         {flags: []string{"endpoint", "bucket", "prefix", "adapter", "key-ref", "repo-id"}, delayZero: true},
	"restore-change":         {flags: []string{"restore"}, required: []string{"restore"}, section: "restore"},
	"restore-elsewhere":      {flags: []string{"backup", "to", "paths", "checks"}, delayZero: true},
	"restore-session-extend": {flags: []string{"session", "hold-hours", "backup", "to", "paths", "checks"}, delayZero: true},
	"restore-dest-release":   {flags: []string{"session", "authorized-key", "allow-from", "with-restore-change"}, delayZero: true},
	"source-change":          {flags: []string{"source"}, required: []string{"source"}, section: "source"},
	"notify-change":          {flags: []string{"notify"}, required: []string{"notify"}, section: "notify"},
	"notify-add":             {flags: []string{"notify"}, required: []string{"notify"}, section: "notify", delayZero: true},
	"key-rotate":             {noDefHash: true},
	"key-rotate-waive":       {noDefHash: true, delayZero: true},
	"phrase-reissue":         {},
	"delay-change":           {flags: []string{"hours"}, required: []string{"hours"}},
	"approval-key-change":    {flags: []string{"pubkey", "key-type"}, required: []string{"pubkey"}},
	"restore-test-reduce":    {flags: []string{"to"}, required: []string{"to"}},
	"restore-test-increase":  {flags: []string{"to"}, required: []string{"to"}, delayZero: true},
	"storage-read-extra":     {flags: []string{"backup", "to", "paths", "checks", "force"}, delayZero: true},
	"def-change":             {flags: []string{"file"}, required: []string{"file"}, delayZero: true, defFile: true},
	"def-change-unlisted":    {flags: []string{"file"}, required: []string{"file"}, defFile: true},
	"checks-weaken":          {flags: []string{"file"}, required: []string{"file"}, defFile: true},
	"hold-release":           {},
	"adapter-add":            {flags: []string{"dir"}, required: []string{"dir"}},
	"attest":                 {flags: []string{"step", "note"}, required: []string{"step"}, delayZero: true, noDefHash: true},
	"seal":                   {flags: []string{"file"}, required: []string{"file"}, delayZero: true, defFile: true},
}

// 引数に付いても args に入れない共通の引数(3.1.1)。
var commonFlags = []string{"dry-run", "run-id", "secrets-stdin"}

// defineOnly は適用の操作の列が define だけの種別(3.1.2.11 の中断後の再開。restore-change を除く)。
var defineOnly = []string{"def-change", "def-change-unlisted", "checks-weaken", "delay-change", "restore-test-reduce", "restore-test-increase", "notify-change", "notify-add", "seal"}

func flagKinds() (map[string]contract.GateFlag, error) {
	l, err := loadGate()
	if err != nil {
		return nil, err
	}
	out := map[string]contract.GateFlag{}
	for _, c := range l.Subcommands {
		if len(c.Command) == 1 && c.Command[0] == "propose" {
			for _, f := range c.Flags {
				out[f.Name] = f
			}
		}
	}
	return out, nil
}

func argKey(name string) string {
	b := []byte(name)
	for i := range b {
		if b[i] == '-' {
			b[i] = '_'
		}
	}
	return string(b)
}

// normalizeArgs は引数を正規化した args にする(3.1.2.9: キーは -- を除き - を _ にする。JSON は正規化、秘密は参照名だけ)。
// file は定義の内容に置き換える kind が別に扱うため、ここでは取り除く。
func normalizeArgs(spec kindSpec, in map[string][]string) (map[string]json.RawMessage, error) {
	fk, err := flagKinds()
	if err != nil {
		return nil, internalErr()
	}
	out := map[string]json.RawMessage{}
	for name, vals := range in {
		if slices.Contains(commonFlags, name) {
			continue
		}
		if !slices.Contains(spec.flags, name) {
			return nil, argErr("--"+name, "unknown")
		}
		v, err := argValue(fk[name], vals)
		if err != nil {
			return nil, argErr("--"+name, "format")
		}
		out[argKey(name)] = v
	}
	for _, r := range spec.required {
		if _, ok := in[r]; !ok {
			return nil, argErr("--"+r, "missing")
		}
	}
	delete(out, "file")
	return out, nil
}

func argValue(f contract.GateFlag, vals []string) (json.RawMessage, error) {
	if f.Value == "none" {
		return json.RawMessage(`true`), nil
	}
	if len(vals) != 1 || vals[0] == "" {
		return nil, errBadArg
	}
	v := vals[0]
	switch f.Value {
	case "json":
		if !json.Valid([]byte(v)) {
			return nil, errBadArg
		}
		return contract.CanonicalJSON([]byte(v))
	case "int":
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return nil, errBadArg
		}
		return json.RawMessage(strconv.FormatInt(n, 10)), nil
	case "base64":
		if _, err := base64.StdEncoding.DecodeString(v); err != nil {
			return nil, errBadArg
		}
	case "enum":
		if !slices.Contains(f.Enum, v) {
			return nil, errBadArg
		}
	}
	return json.Marshal(v)
}

type argError struct{}

func (argError) Error() string { return "bad argument" }

var errBadArg error = argError{}

// delayFor は種別の遅延時間(時間)。遅延 0 の種別は 0、それ以外は承認済みの定義の approval_delay_hours(無ければ既定)。
// 定義の値が範囲外(下限 minDelayHours・上限 delayHoursMax)なら既定へ落とさず誤りにする。
func (a *Approval) delayFor(ctx context.Context, spec kindSpec) (int, error) {
	if spec.delayZero {
		return 0, nil
	}
	norm, _, err := a.State.ReadDef(ctx)
	if err != nil {
		return 0, err
	}
	var d struct {
		Hours *int `json:"approval_delay_hours"`
	}
	if len(norm) == 0 || json.Unmarshal(norm, &d) != nil || d.Hours == nil {
		return defaultDelayHours, nil
	}
	if *d.Hours < minDelayHours || *d.Hours > delayHoursMax {
		return 0, invalidDelay()
	}
	return *d.Hours, nil
}

func invalidDelay() error {
	return apiErr(contract.CodeDefInvalid, map[string]any{"issues": []map[string]string{{"pointer": "/approval_delay_hours", "reason": "invalid_value"}}})
}
