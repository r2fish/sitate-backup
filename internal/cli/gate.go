package cli

// gate(spec 3.1.5)と秘密の専用出力経路(spec 3.1.4)。判定は approval(Gate)が持ち、cli は判定に従って実行し、
// 秘密の出力先を標準出力の先頭レコードに読み替える。書いた後に秘密の値を保持しない。

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type secretMode int

const (
	secretNone   secretMode = iota
	secretStdout            // gate が --secret-fd を標準出力の先頭レコードに読み替えた
)

const secretStdoutName = "stdout-first-record"

// gate は SSH_ORIGINAL_COMMAND を判定の部品へ渡し、許可なら実行する引数列で execute を呼ぶ。
func (d *Deps) gate(ctx context.Context, rest []string, stdout io.Writer) outcome {
	o := outcome{command: []string{"gate"}}
	if len(rest) > 0 {
		o.err = argErr("--positional", "unknown")
		return o
	}
	if d.Approval == nil {
		o.err = toError(contract.ErrNotImplemented("cli.gate"))
		return o
	}
	line := d.env("SSH_ORIGINAL_COMMAND")
	dec, err := d.Approval.Gate(ctx, line)
	if err != nil {
		o.err = toError(err)
		return o
	}
	if !dec.Allowed {
		o.command = gateCommandWords(line)
		o.err = dec.Denied
		if o.err == nil {
			o.err = newErr(contract.CodeGateDenied, map[string]any{"reason": "not_allowed", "index": 0})
		}
		return o
	}
	mode := secretNone
	if dec.SecretOut == secretStdoutName {
		mode = secretStdout
	}
	o = execute(ctx, d, dec.Argv, mode)
	return d.deliverPhrase(o, stdout, mode, nil)
}

// gateCommandWords は拒否の封筒の command(字句規則 3 で決めたサブコマンドの名前の語。決まらなければ ["gate"])。
func gateCommandWords(line string) []string {
	if line == "" || !utf8.ValidString(line) || strings.HasPrefix(line, " ") || strings.HasSuffix(line, " ") || strings.Contains(line, "  ") {
		return []string{"gate"}
	}
	for _, r := range line {
		if r < 0x20 || r == 0x7f {
			return []string{"gate"}
		}
	}
	if sub, _, ok := matchSub(strings.Split(line, " ")); ok && sub.Public != contract.PublicInternal && sub.Public != contract.PublicTestOnly {
		return sub.Words
	}
	return []string{"gate"}
}

// env は環境変数を返す(試験が差し替える)。
func (d *Deps) env(k string) string {
	if d.getenv != nil {
		return d.getenv(k)
	}
	return os.Getenv(k)
}

// openFD は --secret-fd <n> の記述子を書き込み用に返す(試験が差し替える)。
func (d *Deps) openFD(n int) io.WriteCloser {
	if d.fdOpen != nil {
		return d.fdOpen(n)
	}
	return os.NewFile(uintptr(n), "secret-fd")
}

// deliverPhrase は outcome の秘密を出力先へ書いて消す。書けなければ phrase_emitted を立てずに INTERNAL にする。
func (d *Deps) deliverPhrase(o outcome, stdout io.Writer, mode secretMode, fd *int) outcome {
	if o.phrase == nil {
		return o
	}
	defer func() { o.phrase.Wipe(); o.phrase = nil }()
	if o.err != nil { // 失敗の封筒に秘密を添えない(戻り道は spec 3.1.4)
		return o
	}
	rec := append(append([]byte(nil), o.phrase.Reveal()...), '\n')
	defer wipe(rec)
	var err error
	switch {
	case mode == secretStdout:
		_, err = stdout.Write(rec)
	case fd != nil:
		w := d.openFD(*fd)
		if w == nil {
			err = os.ErrInvalid
		} else {
			_, err = w.Write(rec)
			if cerr := w.Close(); err == nil {
				err = cerr
			}
		}
	default:
		err = os.ErrInvalid
	}
	if err != nil || !json.Valid(rec) {
		o.err = internalErr()
		return o
	}
	o.phraseEmitted = true
	return o
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
