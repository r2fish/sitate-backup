//go:build mainpath

package mainpath

// 第 2 段の sitate の呼び出し。管理用 SSH の鍵(強制コマンド gate)で入り、SSH_ORIGINAL_COMMAND の形(語を空白 1 つで結ぶ)で渡す。

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

type envelope struct {
	Schema   string          `json:"schema"`
	Command  []string        `json:"command"`
	OK       bool            `json:"ok"`
	RunID    *string         `json:"run_id"`
	Result   json.RawMessage `json:"result"`
	Warnings []struct {
		Code string `json:"code"`
	} `json:"warnings"`
	Error *struct {
		Code   string          `json:"code"`
		Class  string          `json:"class"`
		Detail json.RawMessage `json:"detail"`
	} `json:"error"`
	PhraseEmitted bool `json:"phrase_emitted"`
}

type cliOut struct {
	Env    envelope
	Secret []byte // gate が先頭に出した秘密のレコード(あれば)
	Raw    string // 標準出力の全体(秘密を含み得る。ログに出さない)
	Stderr string
	Code   int
}

func (o cliOut) code() string {
	if o.Env.Error == nil {
		return ""
	}
	return o.Env.Error.Code
}

// gateCLI は backup の sitate ユーザーへ管理用の鍵で入って gate 経由で呼ぶ。
type gateCLI struct {
	h   *host
	key ssh.Signer
	t   *testing.T
}

// call は argv を gate へ渡す。stdin は秘密の入力(1 行目が --secrets-stdin、次が --phrase-stdin など)。
func (g *gateCLI) call(stdin string, argv ...string) cliOut {
	g.t.Helper()
	var in []byte
	if stdin != "" {
		in = []byte(stdin)
	}
	r, err := g.h.run("sitate", g.key, strings.Join(argv, " "), in)
	if err != nil {
		g.t.Fatalf("gate 経由の呼び出しに失敗: %v", err)
	}
	return parseOut(g.t, r)
}

func parseOut(t *testing.T, r execResult) cliOut {
	t.Helper()
	o := cliOut{Raw: r.Stdout, Stderr: r.Stderr, Code: r.Code}
	lines := strings.Split(strings.TrimSpace(r.Stdout), "\n")
	var recs [][]byte
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			recs = append(recs, []byte(l))
		}
	}
	if len(recs) == 0 {
		t.Fatalf("標準出力が空(終了 %d)。標準エラー: %s", r.Code, r.Stderr)
	}
	last := recs[len(recs)-1]
	if err := json.Unmarshal(last, &o.Env); err != nil || o.Env.Schema != "sitate.out/v1" {
		t.Fatalf("最後のレコードが封筒でない(終了 %d): %.200s", r.Code, bytes.TrimSpace(last))
	}
	if len(recs) == 2 {
		o.Secret = recs[0]
	} else if len(recs) > 2 {
		t.Fatalf("標準出力のレコードが 3 つ以上")
	}
	return o
}

// ok は成功の封筒を要求する。失敗なら原因コードと detail を出して止める。
func (o cliOut) ok(t *testing.T, label string) cliOut {
	t.Helper()
	if !o.Env.OK || o.Code != 0 {
		d := ""
		if o.Env.Error != nil {
			d = o.Env.Error.Code + " " + string(o.Env.Error.Detail)
		}
		t.Fatalf("%s: 失敗(終了 %d): %s\n標準エラー: %.600s", label, o.Code, d, o.Stderr)
	}
	return o
}

// fails は原因コードで失敗することを要求する。
func (o cliOut) fails(t *testing.T, label, code string, exit int) {
	t.Helper()
	if o.Env.OK || o.code() != code || o.Code != exit {
		t.Fatalf("%s: 期待は %s(終了 %d)、実際は ok=%v %s(終了 %d) %v", label, code, exit, o.Env.OK, o.code(), o.Code, o.Env.Error)
	}
}

func (o cliOut) resultMap(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(o.Env.Result, &m); err != nil {
		t.Fatalf("result が JSON のオブジェクトでない: %.200s", o.Env.Result)
	}
	return m
}
