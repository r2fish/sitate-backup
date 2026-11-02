package main

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	codeSpan       = regexp.MustCompile("`([^`]*)`")
	stepRe         = regexp.MustCompile(`^[A-Za-z]?[0-9]+(-[0-9]+)?$`)
	qhRe           = regexp.MustCompile(`^[QH][0-9]+$`)
	pointerRe      = regexp.MustCompile(`^(/([A-Za-z0-9_]+|<i>))+$`)
	flagRe         = regexp.MustCompile(`(^| )--([a-z][a-z0-9-]*)`)
	codeWordRe     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,}$`)
	placeholdr     = regexp.MustCompile(`\{\{[A-Z0-9_]+\}\}`)
	fileFlagRe     = regexp.MustCompile(`(?:^| )--file +([^ ]+)`)
	expectedItemRe = regexp.MustCompile("^`[^`]*` *(=|⊇|⊆) *`[^`]*`$")
)

// splitCommand はコード片 "sitate def validate --file -" を、サブコマンド名("def validate")と残りに分ける。bootstrap 系は sitate を付けても付けなくてもよい。
func (d *lintData) splitCommand(span string) (string, string, bool) {
	words := strings.Fields(strings.TrimPrefix(strings.TrimSpace(span), "sitate "))
	for n := 2; n >= 1; n-- {
		if len(words) >= n {
			name := strings.Join(words[:n], " ")
			if _, ok := d.public[name]; ok {
				return name, strings.Join(words[n:], " "), true
			}
		}
	}
	return "", "", false
}

// commonFlags は spec 3.1.1 の呼び出しの行にある共通の引数(gate の許可リストの共通引数に、--secret-fd を加えたもの)。
// --secret-fd は gate の経路には無く(エージェントは使わない)、手順書の `init` の行が使う。--invoked-by はタイマー専用で含めない。
var commonFlags = map[string]bool{"dry-run": true, "run-id": true, "secrets-stdin": true, "secret-fd": true}

// checkCommandSpan はコード片が wantPublic(公開区分の和集合)のサブコマンドであることと、引数名が許されることを検査する。
func (d *lintData) checkCommandSpan(span string, wantPublic ...string) (string, error) {
	name, rest, ok := d.splitCommand(span)
	if !ok {
		return "", fmt.Errorf("サブコマンドが CLI スキーマに無い: `%s`", span)
	}
	got := d.public[name]
	okKind := false
	for _, w := range wantPublic {
		okKind = okKind || got == w
	}
	if !okKind {
		return name, fmt.Errorf("`%s` の公開区分は %s(%s が必要)", name, got, strings.Join(wantPublic, " または "))
	}
	if name == "bootstrap" || name == "bootstrap-restore" {
		return name, nil
	}
	// --file は標準入力(-)だけ(エージェントの経路は道具に出さない。gate の stdin_dash)。パスを渡す形は落とす。
	if m := fileFlagRe.FindStringSubmatch(rest); m != nil && m[1] != "-" {
		return name, fmt.Errorf("`%s` の --file は `-`(標準入力)だけ: --file %s", name, m[1])
	}
	for _, m := range flagRe.FindAllStringSubmatch(rest, -1) {
		if !d.flags[name][m[2]] && !commonFlags[m[2]] {
			return name, fmt.Errorf("`%s` は --%s を受けない", name, m[2])
		}
	}
	return name, nil
}

// stripParens は丸括弧(ASCII と全角。入れ子を含む)の注記を、コード片の外に限って取り除く。
func stripParens(s string) string {
	var b strings.Builder
	depth, inCode := 0, false
	for _, r := range s {
		switch {
		case r == '`':
			inCode = !inCode
			if depth == 0 {
				b.WriteRune(r)
			}
		case inCode:
			if depth == 0 {
				b.WriteRune(r)
			}
		case r == '(' || r == '（':
			depth++
		case (r == ')' || r == '）') && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// splitTop は s をコード片の外の区切り文字で分ける。
func splitTop(s string, seps string) []string {
	var out []string
	var cur strings.Builder
	inCode := false
	for _, r := range s {
		if r == '`' {
			inCode = !inCode
		}
		if !inCode && strings.ContainsRune(seps, r) {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	return append(out, cur.String())
}

// checkExec は「実行するもの」の文法(spec 3.8 の 6 種)を検査する。要素は「、」か「→」で区切る。
func (d *lintData) checkExec(cell string) []string {
	var out []string
	plain := strings.TrimSpace(stripParens(cell))
	if plain == "" {
		return []string{"実行するものが空"}
	}
	for _, tok := range splitTop(plain, "、→") {
		tok = strings.TrimSpace(tok)
		spans := codeSpan.FindAllStringSubmatch(tok, -1)
		switch {
		case tok == "確認だけ", qhRe.MatchString(tok), strings.HasPrefix(tok, "外:") && len(tok) > len("外:"):
		case strings.HasPrefix(tok, "面:"):
			for _, s := range spans {
				if _, err := d.checkCommandSpan(s[1], "tool", "ui-only"); err != nil {
					out = append(out, "面の操作: "+err.Error())
				}
			}
		case len(spans) >= 1:
			// 道具: コード片は公開区分 tool のサブコマンド。前後の地の文(「定義を組み立てて … に渡す」など)は読まない。
			for _, s := range spans {
				if _, err := d.checkCommandSpan(s[1], "tool"); err != nil {
					out = append(out, "道具: "+err.Error())
				}
			}
		default:
			out = append(out, fmt.Sprintf("実行するものの要素が文法(道具 / Qn / Hn / 面: / 外: / 確認だけ)に合わない: %q", tok))
		}
	}
	return out
}

// checkExpected は期待する値の書式と、Pointer が送る欄の表にあることを検査する。cmd は完了確認コマンドのサブコマンド名。
func (d *lintData) checkExpected(cell, cmd string) []string {
	var out []string
	plain := strings.TrimSpace(stripParens(cell))
	if plain == "" {
		return []string{"期待する値が空"}
	}
	for _, it := range splitTop(plain, "、") {
		it = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(it), "。"))
		spans := codeSpan.FindAllStringSubmatch(it, -1)
		m := expectedItemRe.FindStringSubmatch(strings.TrimSpace(it))
		if len(spans) != 2 || m == nil {
			out = append(out, fmt.Sprintf("期待する値が「`Pointer` = `値`」(=、⊇、⊆)の形でない: %q", it))
			continue
		}
		ptr, op, val := spans[0][1], m[1], spans[1][1]
		if !pointerRe.MatchString(ptr) {
			out = append(out, fmt.Sprintf("JSON Pointer の書式が不正: %q", ptr))
			continue
		}
		if val == "" && !placeholdr.MatchString(it) {
			out = append(out, "期待する値の右辺が空: "+it)
		}
		out = append(out, checkMark(ptr, op, val)...)
		if p := d.checkResultPointer(ptr, cmd); p != "" {
			out = append(out, p)
		}
		key := strings.ReplaceAll(ptr, "<i>", "*")
		if !d.sendListed("envelope", key) && !d.sendListed(cmd, key) {
			out = append(out, fmt.Sprintf("Pointer %s が送る欄の表(%s)に載っていない", ptr, cmd))
		}
	}
	return out
}

// checkFailures は失敗時の欄の原因コードが 3.1.3 のスキーマの一覧にあることを検査する。
func (d *lintData) checkFailures(cell string) []string {
	var out []string
	for _, s := range codeSpan.FindAllStringSubmatch(cell, -1) {
		if codeWordRe.MatchString(s[1]) && !d.codes[s[1]] {
			out = append(out, "原因コードが一覧に無い: "+s[1])
		}
	}
	return out
}

// sendListed は Pointer(<i> は *)が where の送る欄にあるかを返す。配列やオブジェクトそのものを指す Pointer は、その下の欄が載っていれば載っているとみなす。
func (d *lintData) sendListed(where, key string) bool {
	if d.sendWhere[where][key] {
		return true
	}
	for k := range d.sendWhere[where] {
		if strings.HasPrefix(k, key+"/") {
			return true
		}
	}
	return false
}
