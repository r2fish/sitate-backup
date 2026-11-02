package main

import (
	"fmt"
	"regexp"
	"strings"
)

var requiredHeadings = []string{"開始前に用意するもの", "省けない手順", "確認項目", "手順のやり直し"}
var tableHeader = []string{"手順", "目的", "実行するもの", "完了確認コマンド", "期待する値", "失敗時"}

var nthRe = regexp.MustCompile(`([0-9]+) つ目[:：]`)

// splitByNth は期待する値の欄を完了確認コマンドごとに振り分ける。コマンドが 1 つなら欄の全体。
// 2 つ以上なら「1 つ目: … 2 つ目: …」(コマンドの並びの順)で、各区間がその番号のコマンドの結果に対する期待する値になる。
func splitByNth(cell string, n int) ([]string, error) {
	locs := nthRe.FindAllStringSubmatchIndex(cell, -1)
	if n == 1 {
		if len(locs) != 0 {
			return nil, fmt.Errorf("完了確認コマンドが 1 つなのに期待する値に「n つ目:」がある")
		}
		return []string{cell}, nil
	}
	if len(locs) != n {
		return nil, fmt.Errorf("完了確認コマンドが %d 個のときは期待する値を「1 つ目: … 2 つ目: …」の %d 区間に分ける(%d 区間ある)", n, n, len(locs))
	}
	if strings.TrimSpace(cell[:locs[0][0]]) != "" {
		return nil, fmt.Errorf("期待する値の「1 つ目:」の前に文字がある")
	}
	segs := make([]string, n)
	for i, m := range locs {
		if cell[m[2]:m[3]] != fmt.Sprint(i+1) {
			return nil, fmt.Errorf("「n つ目:」の番号が 1 から順でない: %s つ目", cell[m[2]:m[3]])
		}
		end := len(cell)
		if i+1 < n {
			end = locs[i+1][0]
		}
		segs[i] = cell[m[1]:end]
	}
	return segs, nil
}

func cells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Lint は手順書の本文を検査する。root は製品リポジトリの根。第 2 戻り値は骨格(未実装の印)のとき真。
func Lint(root, doc string) ([]string, bool, error) { return lintDoc(root, doc, true) }

// lintDoc の complete が偽のときは、手順 1〜13 の有無を見ない(行単位の試験用)。
func lintDoc(root, doc string, complete bool) ([]string, bool, error) {
	if strings.Contains(doc, notImplementedMark) {
		return nil, true, nil
	}
	d, err := loadData(root)
	if err != nil {
		return nil, false, err
	}
	lines := strings.Split(doc, "\n")
	var problems []string
	for _, h := range requiredHeadings {
		found := false
		for _, l := range lines {
			if strings.HasPrefix(l, "#") && strings.Contains(l, h) {
				found = true
			}
		}
		if !found {
			problems = append(problems, "必須の見出しが無い: "+h)
		}
	}
	tp, seen := d.lintTables(lines)
	problems = append(problems, tp...)
	problems = append(problems, lintHeaders(lines)...)
	problems = append(problems, lintMarks(doc)...)
	if complete {
		problems = append(problems, lintSteps(seen)...)
		problems = append(problems, lintStep1(lines)...)
	}
	return problems, false, nil
}

func (d *lintData) lintTables(lines []string) ([]string, map[string]bool) {
	var problems []string
	seen := map[string]bool{}
	rows := 0
	inTable := false
	for i, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l), "|") {
			inTable = false
			continue
		}
		c := cells(l)
		if sameStrings(c, tableHeader) {
			inTable = true
			continue
		}
		if !inTable || strings.Trim(strings.Join(c, ""), "-: ") == "" {
			continue
		}
		rows++
		problems = append(problems, d.lintRow(i+1, c, seen)...)
	}
	if rows == 0 {
		problems = append(problems, "手順の表(| 手順 | 目的 | 実行するもの | 完了確認コマンド | 期待する値 | 失敗時 |)に手順の行が無い")
	}
	return problems, seen
}

func (d *lintData) lintRow(line int, c []string, seen map[string]bool) []string {
	var out []string
	add := func(ps []string) {
		for _, p := range ps {
			out = append(out, fmt.Sprintf("%s:%d: %s", path, line, p))
		}
	}
	if len(c) != len(tableHeader) {
		return []string{fmt.Sprintf("%s:%d: 列の数が %d(%d が必要)", path, line, len(c), len(tableHeader))}
	}
	if !stepRe.MatchString(c[0]) {
		add([]string{"手順番号の書式が不正: " + c[0]})
	} else if seen[c[0]] {
		add([]string{"手順番号が重複: " + c[0]})
	}
	seen[c[0]] = true
	if c[5] == "" {
		add([]string{"失敗時が空(原因コードを書くか、無ければ `—` と書く)"})
	}
	if c[1] == "" {
		add([]string{"目的が空"})
	}
	add(d.checkExec(c[2]))
	spans := codeSpan.FindAllStringSubmatch(c[3], -1)
	if len(spans) == 0 {
		add([]string{"完了確認コマンドが無い(`sitate …` のコード片が 1 つ以上必要)"})
		return out
	}
	var cmds []string
	for _, s := range spans {
		cmd, err := d.checkCommandSpan(s[1], "tool")
		if err != nil {
			add([]string{"完了確認コマンド: " + err.Error()})
			return out
		}
		cmds = append(cmds, cmd)
	}
	segs, err := splitByNth(c[4], len(cmds))
	if err != nil {
		add([]string{err.Error()})
		return out
	}
	for i, seg := range segs {
		add(d.checkExpected(seg, cmds[i]))
	}
	add(d.checkFailures(c[5]))
	return out
}
