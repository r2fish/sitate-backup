package cli

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// parsed は構文解析した呼び出し(spec 3.1.1 の呼び出し行)。
type parsed struct {
	sub        contract.Subcommand
	positional []string
	args       map[string][]string // サブコマンド固有の引数(`--` を除く名前から値。値なしの引数は空の配列を 1 要素ではなく nil 値で存在だけ示す)
	dryRun     bool
	runID      string
	secretsIn  bool
	secretFD   *int
	byTimer    bool
}

type flagKind string

var identRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// commandFor は構文エラーの封筒の command。語が見つからないときは入力の先頭の語(形が合えば)か version。
func commandFor(args []string) []string {
	if len(args) > 0 && identRe.MatchString(args[0]) {
		return []string{args[0]}
	}
	return []string{"version"}
}

// matchSub は引数の先頭からサブコマンドの語を長い順に探す(`restore-dest teardown` と `restore` は語数で区別)。
func matchSub(args []string) (contract.Subcommand, int, bool) {
	best, n, ok := contract.Subcommand{}, 0, false
	for _, s := range contract.SubcommandTable {
		if len(s.Words) > len(args) || len(s.Words) <= n {
			continue
		}
		if strings.Join(args[:len(s.Words)], " ") == strings.Join(s.Words, " ") {
			best, n, ok = s, len(s.Words), true
		}
	}
	return best, n, ok
}

// flagTable はサブコマンドが受ける引数(名前から値の形)。作業指示書 5.1 の許可リストのデータ(U0)を読む。
func flagTable(words []string) (map[string]contract.GateFlag, *contract.GatePositional, bool) {
	a, err := contract.LoadGateAllowlist()
	if err != nil {
		return nil, nil, false
	}
	for _, c := range a.Subcommands {
		if strings.Join(c.Command, " ") == strings.Join(words, " ") {
			m := map[string]contract.GateFlag{}
			for _, f := range c.Flags {
				m[f.Name] = f
			}
			return m, c.Positional, true
		}
	}
	return nil, nil, false
}

// parseArgs は args(サブコマンド名から後ろ)を解析する。誤りは ARG_INVALID と、封筒の command にする語。
func parseArgs(args []string) (*parsed, []string, *contract.Error) {
	sub, n, ok := matchSub(args)
	if !ok {
		return nil, commandFor(args), argErr("--command", "unknown")
	}
	words := sub.Words
	p := &parsed{sub: sub, args: map[string][]string{}}
	if sub.Public == contract.PublicInternal || sub.Public == contract.PublicTestOnly {
		return p, words, nil // gate と test-tick は引数の解釈を呼び出し側が持つ
	}
	flags, pos, found := flagTable(words)
	if !found {
		return nil, words, argErr("--command", "unknown") // bootstrap 系はアプリが封筒を作る(spec 3.1.1)。核のサブコマンドではない
	}
	rest := args[n:]
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if !strings.HasPrefix(tok, "--") {
			if pos == nil || len(p.positional) >= pos.Count {
				return nil, words, argErr("--positional", "unknown")
			}
			p.positional = append(p.positional, tok)
			continue
		}
		name, val, hasVal := strings.Cut(tok[2:], "=")
		kind, err := flagKindOf(name, flags, sub)
		if err != nil {
			return nil, words, argErr(tok[:2+len(name)], "unknown")
		}
		flag := "--" + name
		if kind == "none" {
			if hasVal {
				return nil, words, argErr(flag, "format")
			}
		} else if !hasVal {
			if i+1 >= len(rest) {
				return nil, words, argErr(flag, "missing")
			}
			i++
			val = rest[i]
		}
		if e := p.set(name, val, kind); e != nil {
			return nil, words, e
		}
	}
	return p, words, nil
}

// flagOf の結果の値の形。共通の引数(spec 3.1.1)を先に見る。--dry-run は表の dry_run が真のサブコマンドだけ。
func flagKindOf(name string, flags map[string]contract.GateFlag, sub contract.Subcommand) (string, error) {
	switch name {
	case "dry-run":
		if sub.DryRun {
			return "none", nil
		}
	case "run-id", "invoked-by", "secret-fd":
		return "word", nil
	case "secrets-stdin":
		return "none", nil
	default:
		if f, ok := flags[name]; ok {
			return f.Value, nil
		}
	}
	return "", errUnknownFlag
}

var errUnknownFlag = &contract.Error{Code: contract.CodeArgInvalid}

func (p *parsed) set(name, val, kind string) *contract.Error {
	flag := "--" + name
	switch name {
	case "dry-run":
		p.dryRun = true
	case "secrets-stdin":
		p.secretsIn = true
	case "run-id":
		if !runIDRe.MatchString(val) {
			return argErr(flag, "format")
		}
		p.runID = val
	case "invoked-by":
		if val != contract.TimerInvokedBy {
			return argErr(flag, "format")
		}
		p.byTimer = true
		p.args[argInvokedBy] = []string{val} // 入口が導入中の飛ばしを判定できるよう Call.Args にも載せる
	case "secret-fd":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return argErr(flag, "format")
		}
		p.secretFD = &n
	default:
		if kind == "int" {
			if _, err := strconv.Atoi(val); err != nil {
				return argErr(flag, "format")
			}
		}
		if kind == "none" {
			p.args[name] = append(p.args[name], "")
		} else {
			p.args[name] = append(p.args[name], val)
		}
	}
	return nil
}
