package approval

// gate の判定(spec 3.1.5)。入力は SSH_ORIGINAL_COMMAND の字面で、エージェントから届く信頼できない入力として扱う。
// 判定だけを持ち、実行は cli。許可リストはデータ(contract.LoadGateAllowlist)。拒否の detail に語そのものを入れない。

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// gate の GATE_DENIED の detail.reason(spec 3.1.3)。
const (
	gateNotAllowed = "not_allowed"
	gateLocal      = "local_restore"
	gateRelay      = "relay"
	gateFilePath   = "file_path"
	gateInvokedBy  = "invoked_by"
	gateLexical    = "lexical"
	gateFlagForm   = "flag_form"
	gateDuplicate  = "duplicate"
	gateSecretFD   = "secret_fd"
)

const (
	flagSecretFD  = "secret-fd"
	flagInvokedBy = "invoked-by"
	flagRelay     = "relay"
	secretOutHead = "stdout-first-record"
)

var (
	gateOnce  sync.Once
	gateList  contract.GateAllowlist
	gateErr   error
	gateRefRe *regexp.Regexp
	b64Re     = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)
	intRe     = regexp.MustCompile(`^[0-9]{1,18}$`)
)

func loadGate() (contract.GateAllowlist, error) {
	gateOnce.Do(func() {
		gateList, gateErr = contract.LoadGateAllowlist()
		if gateErr == nil {
			gateRefRe, gateErr = regexp.Compile(gateList.RefPattern)
		}
	})
	return gateList, gateErr
}

func gateDeny(reason string, idx int) contract.GateDecision {
	var index any
	if idx >= 0 {
		index = idx
	}
	return contract.GateDecision{Denied: apiErr(contract.CodeGateDenied, map[string]any{"reason": reason, "index": index})}
}

// lexicalOK は字句規則 2(空・先頭末尾の空白・続く空白・制御文字・不正な UTF-8)に当たらないことを返す。
func lexicalOK(line string) bool {
	if line == "" || line[0] == ' ' || line[len(line)-1] == ' ' || strings.Contains(line, "  ") || !utf8.ValidString(line) {
		return false
	}
	for i := 0; i < len(line); i++ {
		if line[i] < 0x20 || line[i] == 0x7f {
			return false
		}
	}
	return true
}

// matchCommand は字句規則 3(先頭の 2 語を先に照らす)。見つからなければ nil。
func matchCommand(l contract.GateAllowlist, words []string) *contract.GateCommand {
	for _, n := range []int{2, 1} {
		if len(words) < n {
			continue
		}
		for i := range l.Subcommands {
			if slices.Equal(l.Subcommands[i].Command, words[:n]) {
				return &l.Subcommands[i]
			}
		}
	}
	return nil
}

// GateCommandWords は拒否の封筒の command(字句規則 3 で決めたサブコマンドの名前の語の配列。決まらなければ ["gate"]。判定の規則 4)。
func GateCommandWords(line string) []string {
	l, err := loadGate()
	if err != nil || !lexicalOK(line) {
		return []string{"gate"}
	}
	if c := matchCommand(l, strings.Split(line, " ")); c != nil {
		return slices.Clone(c.Command)
	}
	return []string{"gate"}
}

// Gate は許可リストと --secret-fd の読み替えの判定(contract.Approval)。依存を持たない。
func (*Approval) Gate(_ context.Context, line string) (contract.GateDecision, error) {
	l, err := loadGate()
	if err != nil {
		return contract.GateDecision{}, internalErr()
	}
	if !lexicalOK(line) {
		return gateDeny(gateLexical, -1), nil
	}
	words := strings.Split(line, " ")
	cmd := matchCommand(l, words)
	if cmd == nil {
		return gateDeny(gateNotAllowed, 0), nil
	}
	for i, w := range words {
		switch w {
		case "--" + flagInvokedBy:
			return gateDeny(gateInvokedBy, i), nil
		case "--" + flagRelay:
			return gateDeny(gateRelay, i), nil
		}
	}
	return parseGate(l, cmd, words), nil
}

type gateParse struct {
	l      contract.GateAllowlist
	cmd    *contract.GateCommand
	words  []string
	seen   map[string]bool
	argv   []string
	secret bool
}

func parseGate(l contract.GateAllowlist, cmd *contract.GateCommand, words []string) contract.GateDecision {
	p := &gateParse{l: l, cmd: cmd, words: words, seen: map[string]bool{}, argv: slices.Clone(words[:len(cmd.Command)])}
	i, d, bad := p.positionals(len(cmd.Command))
	if bad {
		return d
	}
	for i < len(words) {
		var ni int
		ni, d, bad = p.flag(i)
		if bad {
			return d
		}
		i = ni
	}
	if len(cmd.Command) == 1 && cmd.Command[0] == "restore" && !p.seen["to"] { // --to の無い restore は既定の宛先に頼る形になる
		return gateDeny(gateLocal, -1)
	}
	out := contract.GateDecision{Allowed: true, Argv: p.argv}
	if p.secret {
		out.SecretOut = secretOutHead
	}
	return out
}

// positionals は位置引数(サブコマンドの語の直後。字句規則 4)を検査して次の位置を返す。
func (p *gateParse) positionals(i int) (int, contract.GateDecision, bool) {
	want := 0
	if p.cmd.Positional != nil {
		want = p.cmd.Positional.Count
	}
	for n := 0; n < want; n++ {
		if i >= len(p.words) || strings.HasPrefix(p.words[i], "-") || !p.positionalOK(p.words[i]) {
			return i, gateDeny(gateNotAllowed, min(i, len(p.words)-1)), true
		}
		p.argv = append(p.argv, p.words[i])
		i++
	}
	return i, contract.GateDecision{}, false
}

func (p *gateParse) positionalOK(w string) bool {
	pos := p.cmd.Positional
	if pos.Ref {
		return gateRefRe.MatchString(w)
	}
	return slices.Contains(pos.Values, w)
}

// flagSpec は完全に一致する引数の定義(サブコマンドの引数、共通の引数の順)。
func (p *gateParse) flagSpec(name string) *contract.GateFlag {
	for _, set := range [][]contract.GateFlag{p.cmd.Flags, p.l.CommonFlags} {
		for k := range set {
			if set[k].Name == name {
				return &set[k]
			}
		}
	}
	return nil
}

// isPrefixAbbrev は name が既知の引数の名前の前方一致の略記か。
func (p *gateParse) isPrefixAbbrev(name string) bool {
	if name == "" {
		return false
	}
	for _, set := range [][]contract.GateFlag{p.cmd.Flags, p.l.CommonFlags} {
		for _, f := range set {
			if strings.HasPrefix(f.Name, name) {
				return true
			}
		}
	}
	return p.cmd.SecretFD && strings.HasPrefix(flagSecretFD, name)
}

// flag は i 番目の語を --<名前> として読み、値を検査して次の位置を返す。
func (p *gateParse) flag(i int) (int, contract.GateDecision, bool) {
	w := p.words[i]
	if !strings.HasPrefix(w, "--") {
		if strings.HasPrefix(w, "-") {
			return i, gateDeny(gateFlagForm, i), true // 1 文字の短い形
		}
		return i, gateDeny(gateNotAllowed, i), true // 位置の語
	}
	name := w[2:]
	if strings.Contains(name, "=") {
		return i, gateDeny(gateFlagForm, i), true
	}
	if name == flagSecretFD {
		return p.secretFD(i)
	}
	spec := p.flagSpec(name)
	if spec == nil {
		if p.isPrefixAbbrev(name) {
			return i, gateDeny(gateFlagForm, i), true
		}
		return i, gateDeny(gateNotAllowed, i), true
	}
	if p.seen[name] {
		return i, gateDeny(gateDuplicate, i), true
	}
	p.seen[name] = true
	p.argv = append(p.argv, w)
	if spec.Value == "none" {
		return i + 1, contract.GateDecision{}, false
	}
	return p.value(i, spec)
}

func (p *gateParse) secretFD(i int) (int, contract.GateDecision, bool) {
	if !p.cmd.SecretFD {
		return i, gateDeny(gateSecretFD, i), true
	}
	if p.seen[flagSecretFD] {
		return i, gateDeny(gateDuplicate, i), true
	}
	p.seen[flagSecretFD] = true
	if i+1 >= len(p.words) || !intRe.MatchString(p.words[i+1]) {
		return i, gateDeny(gateSecretFD, i), true
	}
	if _, err := strconv.ParseUint(p.words[i+1], 10, 63); err != nil {
		return i, gateDeny(gateSecretFD, i), true
	}
	p.secret = true // 値は使わず、実行する引数列から除く
	return i + 2, contract.GateDecision{}, false
}

// value は値を取る引数の 1 語の値を検査する(判定の規則 1・2 を含む)。
func (p *gateParse) value(i int, spec *contract.GateFlag) (int, contract.GateDecision, bool) {
	vi := i + 1
	if vi >= len(p.words) {
		return i, gateDeny(gateNotAllowed, i), true
	}
	v := p.words[vi]
	if spec.Value == "stdin_dash" && spec.Name == "file" {
		if v != "-" {
			return i, gateDeny(gateFilePath, vi), true
		}
	} else if strings.HasPrefix(v, "-") { // 次の引数に見える値(pflag は値として食う)を通さない
		return i, gateDeny(gateNotAllowed, vi), true
	}
	if reason := p.checkValue(spec, v); reason != "" {
		return i, gateDeny(reason, vi), true
	}
	p.argv = append(p.argv, v)
	return vi + 1, contract.GateDecision{}, false
}

func (p *gateParse) checkValue(spec *contract.GateFlag, v string) string {
	if len(p.cmd.Command) == 1 && p.cmd.Command[0] == "restore" && spec.Name == "to" {
		if !slices.ContainsFunc(p.l.RestoreToPrefix, func(pre string) bool { return strings.HasPrefix(v, pre) }) {
			return gateLocal
		}
	}
	ok := true
	switch spec.Value {
	case "ref":
		ok = gateRefRe.MatchString(v)
	case "base64":
		ok = b64Re.MatchString(v)
	case "int":
		ok = intRe.MatchString(v)
	case "enum":
		ok = slices.Contains(spec.Enum, v)
	case "stdin_dash":
		ok = v == "-"
	}
	if !ok {
		return gateNotAllowed
	}
	return ""
}
