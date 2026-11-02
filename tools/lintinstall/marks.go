package main

import (
	"fmt"
	"regexp"
	"strings"
)

// 印({{名前}})の許可リスト。印の置換と値の定義は sign.sh と U4 が持つ。ここは名前の綴りと、置いてよい Pointer・演算子を縛る。
// 手順 1 の指紋は、直書きも別の印も許さず、signing_key_fp の右辺の DIST_KEY_FINGERPRINT に限る(A2 の手順 1 まで綴りの誤りに気づけないため)。
type markRule struct{ ptr, op string }

var allowedMarks = map[string]markRule{
	"DIST_KEY_FINGERPRINT": {"/result/signing_key_fp", "="},
	"TIMER_NAMES":          {"/result/schedule", "⊇"},
	"WARNING_ALLOWLIST":    {"/warnings/<i>/code", "⊆"},
}

const fingerprintPtr = "/result/signing_key_fp"

var anyMarkRe = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// lintMarks は本文のどこにある {{…}} も許可リストの名前であることを検査する(綴りの誤りを落とす)。
func lintMarks(doc string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range anyMarkRe.FindAllString(doc, -1) {
		name := strings.TrimSuffix(strings.TrimPrefix(m, "{{"), "}}")
		if _, ok := allowedMarks[name]; !ok && !seen[m] {
			seen[m] = true
			out = append(out, fmt.Sprintf("印 %s は許可リスト(DIST_KEY_FINGERPRINT・TIMER_NAMES・WARNING_ALLOWLIST)に無い", m))
		}
	}
	return out
}

// checkMark は期待する値の 1 要素(Pointer・演算子・右辺)の印の使い方を検査する。
func checkMark(ptr, op, val string) []string {
	var out []string
	marks := anyMarkRe.FindAllString(val, -1)
	if ptr == fingerprintPtr {
		if op != "=" || val != "{{DIST_KEY_FINGERPRINT}}" {
			out = append(out, fmt.Sprintf("%s の右辺は {{DIST_KEY_FINGERPRINT}} だけ(指紋の直書きと別の印を許さない): %s %s", fingerprintPtr, op, val))
		}
		return out
	}
	for _, m := range marks {
		name := strings.TrimSuffix(strings.TrimPrefix(m, "{{"), "}}")
		r, ok := allowedMarks[name]
		switch {
		case !ok:
			// 名前の綴りは lintMarks が落とす。
		case r.ptr != ptr || r.op != op || val != m:
			out = append(out, fmt.Sprintf("印 %s は `%s` %s %s の形だけに置ける: `%s` %s %s", m, r.ptr, r.op, m, ptr, op, val))
		}
	}
	return out
}

var stepsRequired = func() []string {
	var s []string
	for i := 1; i <= 13; i++ {
		s = append(s, fmt.Sprint(i))
	}
	return s
}()

// lintSteps は手順 1〜13 がすべてあることを検査する(行が欠けても合格しない)。
func lintSteps(seen map[string]bool) []string {
	var out []string
	for _, s := range stepsRequired {
		if !seen[s] {
			out = append(out, "手順 "+s+" の行が無い(手順 1〜13 がすべて要る)")
		}
	}
	return out
}

// lintHeaders は見出し行が「手順」と「完了確認コマンド」を含むのに、手順の表の見出しと一字違うものを落とす
// (見出しを変えると表が読まれず、検査を通り抜けるため)。見出し行は直後が区切り行(---)の表の行。
func lintHeaders(lines []string) []string {
	var out []string
	for i := 0; i+1 < len(lines); i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "|") || strings.Trim(strings.TrimSpace(lines[i+1]), "|-: ") != "" || !strings.HasPrefix(strings.TrimSpace(lines[i+1]), "|") {
			continue
		}
		c := cells(lines[i])
		if sameStrings(c, tableHeader) {
			continue
		}
		joined := strings.Join(c, "|")
		if strings.Contains(joined, "手順") && strings.Contains(joined, "完了確認コマンド") {
			out = append(out, fmt.Sprintf("%s:%d: 見出しが手順の表と違う(`| %s |` が必要): %s", path, i+1, strings.Join(tableHeader, " | "), strings.TrimSpace(lines[i])))
		}
	}
	return out
}

var fpRowRe = regexp.MustCompile("`/result/signing_key_fp` *= *`\\{\\{DIST_KEY_FINGERPRINT\\}\\}`")

// lintStep1 は手順 1 の行の期待する値に、配布用の鍵の指紋の照合(signing_key_fp = {{DIST_KEY_FINGERPRINT}})があることを検査する。
func lintStep1(lines []string) []string {
	var out []string
	for i, l := range lines {
		c := cells(l)
		if strings.HasPrefix(strings.TrimSpace(l), "|") && len(c) == len(tableHeader) && c[0] == "1" && !fpRowRe.MatchString(c[4]) {
			out = append(out, fmt.Sprintf("%s:%d: 手順 1 の期待する値に `%s` = `{{DIST_KEY_FINGERPRINT}}` が無い", path, i+1, fingerprintPtr))
		}
	}
	return out
}
