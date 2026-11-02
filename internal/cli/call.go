package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// buildCall は部品の入口へ渡す Call を組む(spec 3.1.1)。標準入力の秘密は --secrets-stdin → --phrase-stdin / --snapshot-key-stdin の順に 1 行ずつ読む。
func buildCall(_ context.Context, d *Deps, p *parsed, runID *string) (contract.Call, *contract.Error) {
	c := contract.Call{Words: p.sub.Words, Positional: p.positional, Args: p.args, DryRun: p.dryRun}
	if runID != nil {
		c.RunID = *runID
	}
	in := d.stdinReader() // --file - が続けて読めるよう共有する
	if p.secretsIn {
		line, ok := readLine(in)
		if !ok {
			return c, argErr("--secrets-stdin", "missing")
		}
		var m map[string]json.RawMessage
		if json.Unmarshal([]byte(line), &m) != nil || m == nil {
			return c, argErr("--secrets-stdin", "format")
		}
		c.Secrets = contract.SecretInput(m)
	}
	if _, ok := p.args["phrase-stdin"]; ok {
		line, ok := readLine(in)
		if !ok {
			return c, argErr("--phrase-stdin", "missing")
		}
		c.PhraseIn = contract.NewSecret([]byte(line))
	}
	if _, ok := p.args["snapshot-key-stdin"]; ok {
		line, ok := readLine(in)
		if !ok {
			return c, argErr("--snapshot-key-stdin", "missing")
		}
		c.SnapshotKeyIn = contract.NewSecret([]byte(line))
	}
	if _, ok := p.args["notify-proof-stdin"]; ok { // 宛先 → 確認語の JSON 1 行(approve。照合は approval)
		line, ok := readLine(in)
		if !ok {
			return c, argErr("--notify-proof-stdin", "missing")
		}
		c.Args["notify-proof-stdin"] = []string{line}
	}
	c.SecretOut = p.secretFD != nil
	return c, nil
}

// readLine は 1 行(改行を除く)を読む。1 文字も読めずに終わったら偽。
func readLine(r *bufio.Reader) (string, bool) {
	if r == nil {
		return "", false
	}
	s, err := r.ReadString('\n')
	if err != nil && (err != io.EOF || s == "") {
		return "", false
	}
	return strings.TrimRight(s, "\r\n"), true
}
