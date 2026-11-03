package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// 手順書(docs/install.md、または make sign-test が作った写し)の手順の表を読み、
// 各手順の完了確認コマンドと期待する値を取り出す。完了確認を試験の中に写さず、手順書から読む(作業指示書 5.16)。

// Expect は期待する値の 1 要素(JSON Pointer と演算子と右辺)。
type Expect struct {
	Pointer string // 例 /result/last_backup/ok。<i> は配列の全要素
	Op      string // = ⊇ ⊆
	Value   string // コード片の中身。{{印}} は解決前のまま
	Skip    bool   // 注記「書き込み鍵の版指定なしの削除の要素を除く」(spec 4.2)。op=unversioned_delete かつ key_role=write の要素を比べない
}

const skipNote = "書き込み鍵の版指定なしの削除の要素を除く"

// Check は 1 つの完了確認コマンドと、その結果に対する期待する値。
type Check struct {
	Command string // 例 sitate status
	Expect  []Expect
}

// Step は手順書の表の 1 行。
type Step struct {
	ID     string
	Checks []Check
}

var (
	codeSpanRe = regexp.MustCompile("`([^`]*)`")
	nthSplitRe = regexp.MustCompile(`[0-9]+ つ目[:：]`)
	itemRe     = regexp.MustCompile("^`([^`]*)` *(=|⊇|⊆) *`([^`]*)`$")
	headerCols = []string{"手順", "目的", "実行するもの", "完了確認コマンド", "期待する値", "失敗時"}
)

func splitCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// stripNotes は丸括弧(入れ子を含む)の注記を、コード片の外に限って取り除く。
func stripNotes(s string) string {
	var b strings.Builder
	depth, code := 0, false
	for _, r := range s {
		switch {
		case r == '`':
			code = !code
			if depth == 0 {
				b.WriteRune(r)
			}
		case code:
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

func parseExpects(seg string) ([]Expect, error) {
	var out []Expect
	var cur strings.Builder
	code := false
	flush := func() error {
		it := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(cur.String()), "。"))
		cur.Reset()
		if it == "" {
			return nil
		}
		m := itemRe.FindStringSubmatch(it)
		if m == nil {
			return fmt.Errorf("期待する値の要素が読めない: %q", it)
		}
		out = append(out, Expect{Pointer: m[1], Op: m[2], Value: m[3], Skip: strings.Contains(seg, skipNote) && strings.Contains(m[1], "/<i>/")})
		return nil
	}
	for _, r := range stripNotes(seg) {
		if r == '`' {
			code = !code
		}
		if !code && r == '、' {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		cur.WriteRune(r)
	}
	return out, flush()
}

func parseChecks(cmdCell, expCell string) ([]Check, error) {
	var cmds []string
	for _, m := range codeSpanRe.FindAllStringSubmatch(cmdCell, -1) {
		cmds = append(cmds, m[1])
	}
	if len(cmds) == 0 {
		return nil, fmt.Errorf("完了確認コマンドが無い")
	}
	segs := []string{expCell}
	if len(cmds) > 1 {
		locs := nthSplitRe.FindAllStringIndex(expCell, -1)
		if len(locs) != len(cmds) {
			return nil, fmt.Errorf("期待する値の区間が %d 個(コマンドは %d 個)", len(locs), len(cmds))
		}
		segs = nil
		for i, l := range locs {
			end := len(expCell)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			segs = append(segs, expCell[l[1]:end])
		}
	}
	var out []Check
	for i, c := range cmds {
		ex, err := parseExpects(segs[i])
		if err != nil {
			return nil, err
		}
		out = append(out, Check{Command: c, Expect: ex})
	}
	return out, nil
}

// ParseInstall は手順書の本文から、手順の表の全行を読む(付録の行を含む)。
func ParseInstall(doc string) ([]Step, error) {
	var steps []Step
	in := false
	for n, l := range strings.Split(doc, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "|") {
			in = false
			continue
		}
		c := splitCells(l)
		if strings.Join(c, "|") == strings.Join(headerCols, "|") {
			in = true
			continue
		}
		if !in || strings.Trim(strings.Join(c, ""), "-: ") == "" || len(c) != len(headerCols) {
			continue
		}
		checks, err := parseChecks(c[3], c[4])
		if err != nil {
			return nil, fmt.Errorf("手順 %s(%d 行目): %w", c[0], n+1, err)
		}
		steps = append(steps, Step{ID: c[0], Checks: checks})
	}
	return steps, nil
}

// LoadInstall は手順書を読んで手順を返す。path が空なら環境変数 SITATE_INSTALL_DOC、無ければ ../../docs/install.md。
func LoadInstall(path string) ([]Step, error) {
	if path == "" {
		path = os.Getenv("SITATE_INSTALL_DOC")
	}
	if path == "" {
		path = "../../docs/install.md"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseInstall(string(b))
}

// Marks は印({{名前}})の解決値。DIST_KEY_FINGERPRINT は文字列、TIMER_NAMES と WARNING_ALLOWLIST は文字列の配列。
type Marks map[string]json.RawMessage
