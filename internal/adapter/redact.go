package adapter

import (
	"regexp"
	"sort"
	"strings"
)

// 標準エラーの伏せ字(spec 3.2.1「標準エラー」「伏せ字」)。

const minSecretLen = 8 // 要求の secrets の値のうち伏せる最小のバイト数

var (
	rePrivateKey = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|\z)`)
	reAPIKey     = regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{20,}`)
	reAWSKey     = regexp.MustCompile(`(?:AKIA|ASIA)[A-Z0-9]{16}`)
	reB2Key      = regexp.MustCompile(`(^|[^A-Za-z0-9+/])K[0-9]{3}[A-Za-z0-9+/]{27}($|[^A-Za-z0-9+/])`)
	reWebhook    = regexp.MustCompile(`https://[^/\s"'<>?#]*[/?][^\s"'<>]*?[A-Za-z0-9_-]{20}[^\s"'<>]*`)
	reURLUser    = regexp.MustCompile(`//[^/\s@]+:[^/\s]*@`)
	reAuthLine   = regexp.MustCompile(`(?im)^([ \t]*Authorization:)[^\n]*`)
)

func mark(kind string) string { return "[REDACTED:" + kind + "]" }

// limitStderr は受けたバイト列の最後の改行より後(途中で切れた行)を捨て、捨てた分を数えて返す。
// received は受けたバイト数の全部(先頭の上限までを buf に持ち、残りは読み捨てた分)。
func limitStderr(buf []byte, received int64) (kept string, dropped int64) {
	i := strings.LastIndexByte(string(buf), '\n')
	if i < 0 {
		return "", received
	}
	return string(buf[:i+1]), received - int64(i+1)
}

// redact は伏せ字を当てる。secrets は要求の secrets に入れた値(8 バイト未満は対象外)。
func redact(s string, secrets []string) string {
	vals := make([]string, 0, len(secrets))
	for _, v := range secrets {
		if len(v) >= minSecretLen {
			vals = append(vals, v)
		}
	}
	sort.Slice(vals, func(i, j int) bool { return len(vals[i]) > len(vals[j]) })
	for _, v := range vals {
		s = strings.ReplaceAll(s, v, mark("secret"))
	}
	s = rePrivateKey.ReplaceAllString(s, mark("private_key"))
	s = reAPIKey.ReplaceAllString(s, mark("api_key"))
	s = reAWSKey.ReplaceAllString(s, mark("storage_key"))
	for { // 区切り 1 文字で並ぶキーは、先の一致が区切りを使うため、変化が無くなるまで繰り返す
		n := reB2Key.ReplaceAllString(s, "${1}"+mark("storage_key")+"${2}")
		if n == s {
			break
		}
		s = n
	}
	s = reWebhook.ReplaceAllString(s, mark("webhook_url"))
	s = reURLUser.ReplaceAllString(s, "//"+mark("credential")+"@")
	return reAuthLine.ReplaceAllString(s, "${1} "+mark("credential"))
}
