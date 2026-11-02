package adapter

// 完了条件 3(標準エラーの長さの制限と伏せ字)と、標準エラー・秘密が結果とログに漏れないこと。

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func TestRedactForms(t *testing.T) {
	pem := "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\nBBBB\n-----END OPENSSH PRIVATE KEY-----"
	cases := []struct{ name, in, secret, kind string }{
		{"要求の秘密(8 バイト以上)", "token=" + secretVal + " end", secretVal, "secret"},
		{"秘密鍵", "k:\n" + pem + "\nafter", "", "private_key"},
		{"LLM API キー", "key sk-ant-abcdefghijklmnopqrstuv x", "", "api_key"},
		{"AWS のアクセスキー ID", "id AKIAABCDEFGHIJKLMNOP x", "", "storage_key"},
		{"B2 のキー", "app K001" + strings.Repeat("a", 27) + " x", "", "storage_key"},
		{"Webhook の URL", "post https://hooks.example.invalid/services/" + strings.Repeat("a1", 12) + "?x=1 x", "", "webhook_url"},
		{"URL の利用者情報", "GET https://user:pa55word@example.invalid/path", "", "credential"},
		{"Authorization の行", "Authorization: Bearer abcdefgh12345\nnext", "", "credential"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var sec []string
			if c.secret != "" {
				sec = []string{c.secret}
			}
			out := redact(c.in, sec)
			if !strings.Contains(out, "[REDACTED:"+c.kind+"]") {
				t.Errorf("[REDACTED:%s] が無い: %q", c.kind, out)
			}
			for _, leak := range []string{secretVal, "AAAA", "sk-ant-abcdef", "AKIAABCD", "pa55word", "Bearer", "a1a1a1a1a1"} {
				if strings.Contains(out, leak) {
					t.Errorf("%q が残った: %q", leak, out)
				}
			}
		})
	}
	two := "K001" + strings.Repeat("a", 27) + " K002" + strings.Repeat("b", 27)
	if out := redact(two, nil); strings.Count(out, "[REDACTED:storage_key]") != 2 || strings.Contains(out, "bbbb") {
		t.Errorf("区切り 1 文字で並ぶ B2 のキーを両方伏せる: %q", out)
	}
	if out := redact("https://user:p@ss@example.invalid/x", nil); strings.Contains(out, "ss@") || strings.Contains(out, "p@") {
		t.Errorf("@ を含むパスワードの残りが出た: %q", out)
	}
	if out := redact("short ab", []string{"ab"}); out != "short ab" {
		t.Errorf("8 バイト未満の値は伏せない: %q", out)
	}
	if out := redact("plain line\nmore", nil); out != "plain line\nmore" {
		t.Errorf("該当しない文は変えない: %q", out)
	}
}

// stderrScript は長い標準エラー(上限を超える)の先頭に秘密を出し、末尾は改行で終わらない。
const stderrScript = `cat >/dev/null
echo "token=` + secretVal + `" >&2
i=0; while [ $i -lt 2200 ]; do echo "line $i padding padding padding padding padding" >&2; i=$((i+1)); done
printf 'partial-line-without-newline' >&2
`

func stderrExpectedTotal() int {
	n := len("token="+secretVal+"\n") + len("partial-line-without-newline")
	for i := 0; i < 2200; i++ {
		n += len(fmt.Sprintf("line %d padding padding padding padding padding\n", i))
	}
	return n
}

func withSecretURL() []byte {
	return []byte(strings.Replace(string(request("sitate.notify/v1")), "https://hooks.example.invalid/x", secretVal, 1))
}

func TestStderrLimitedAndRedacted(t *testing.T) {
	b := build(t, notifySpec(stderrScript+"echo '"+okResult+"'"))
	var recs []StderrRecord
	b.r.Stderr = func(r StderrRecord) { recs = append(recs, r) }
	res, err := b.r.Run(ctx, contract.RunContext{RunID: "r-1"}, "notify", "webhook", withSecretURL(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(res), "padding") || strings.Contains(string(res), "REDACTED") {
		t.Errorf("標準エラーが結果に出た: %s", res)
	}
	if len(recs) != 1 {
		t.Fatalf("記録は 1 回: %d", len(recs))
	}
	r := recs[0]
	if r.RunID != "r-1" || r.Kind != "notify" || r.Name != "webhook" {
		t.Errorf("記録の見出し: %+v", r)
	}
	if len(r.Text) > contract.AdapterStderrMax || !strings.HasSuffix(r.Text, "\n") || strings.Contains(r.Text, "partial-line") {
		t.Errorf("上限・最後の改行まで: len=%d", len(r.Text))
	}
	if r.Dropped <= 0 {
		t.Errorf("捨てたバイト数を記録する: %d", r.Dropped)
	}
	if strings.Contains(r.Text, secretVal) || !strings.Contains(r.Text, "token=[REDACTED:secret]") {
		t.Errorf("秘密の値を伏せていない: %q", r.Text[:40])
	}
	// 伏せ字で文が変わるため、捨てた量は伏せる前の長さで数える(受けた量 = 伏せる前の記録の量 + 捨てた量)
	if got := int64(len(r.Text)) - int64(len("token=[REDACTED:secret]")-len("token="+secretVal)) + r.Dropped; got != int64(stderrExpectedTotal()) {
		t.Errorf("受けた量 %d != 期待 %d", got, stderrExpectedTotal())
	}
}

// 失敗した実行でも、標準エラーと秘密は返す error にもその detail にも出ない。
func TestStderrAndSecretsNotInErrors(t *testing.T) {
	scripts := map[string]string{
		"終了コード":   "cat >/dev/null; echo 'leak " + secretVal + " sk-ant-abcdefghijklmnopqrstuv' >&2; exit 4",
		"出力の不正":   "cat >/dev/null; echo 'leak " + secretVal + "' >&2; echo junk",
		"アダプタの失敗": "cat >/dev/null; echo 'leak " + secretVal + "' >&2; echo '" + errLine("adapter:webhook:BAD", `{}`) + "'; exit 1",
	}
	for name, sc := range scripts {
		t.Run(name, func(t *testing.T) {
			b := build(t, notifySpec(sc))
			var got []string
			b.r.Stderr = func(r StderrRecord) { got = append(got, r.Text) }
			res, err := b.r.Run(ctx, contract.RunContext{}, "notify", "webhook", withSecretURL(), nil)
			ce, ok := err.(*contract.Error)
			if !ok {
				t.Fatalf("error を期待: %s %v", res, err)
			}
			all := err.Error() + string(ce.Detail) + string(res)
			if strings.Contains(all, "leak") || strings.Contains(all, secretVal) || strings.Contains(all, "sk-ant") {
				t.Errorf("標準エラーか秘密が error に出た: %s", all)
			}
			if len(got) != 1 || strings.Contains(got[0], secretVal) || strings.Contains(got[0], "sk-ant-abcdef") || !strings.Contains(got[0], "leak") {
				t.Errorf("記録には伏せ字を当てた文が 1 回: %q", got)
			}
		})
	}
}

// 落とした枠(adapter.json の secrets[] に無い枠)の値も、標準エラーで伏せる。
func TestStderrRedactsDroppedSlotValues(t *testing.T) {
	b := build(t, notifySpec("cat >/dev/null; echo 'smtp=smtp-pass-should-not-pass' >&2; echo '"+okResult+"'"))
	var rec StderrRecord
	b.r.Stderr = func(r StderrRecord) { rec = r }
	var m map[string]json.RawMessage
	_ = json.Unmarshal(request("sitate.notify/v1"), &m)
	m["secrets"] = json.RawMessage(`{"url":"https://hooks.example.invalid/x","smtp":"smtp-pass-should-not-pass"}`)
	req, _ := json.Marshal(m)
	if _, err := b.r.Run(ctx, contract.RunContext{}, "notify", "webhook", req, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Text, "smtp-pass") || !strings.Contains(rec.Text, "[REDACTED:secret]") {
		t.Errorf("落とした枠の値が残った: %q", rec.Text)
	}
}
