package recv

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 完了条件(作業指示書 5.8 の 1 と本体): 調査の処理と source-setup は、root の `sh -s` と、パスワードを問わない sudo を持つ
// 管理アカウントの `sudo -n sh -s` のどちらで流しても動き、元サーバーに何も書かず(調査)、スクリプトも一時ファイルも残さない
// (spec 3.3 の「接続先と資格情報」「実行の仕方」)。root の使い捨て容器と sudo が要る(SITATE_DESTRUCTIVE_SETUP=1)。
// 「sudo -n がパスワードを求める / sudo が無い」の判定(step: sudo、sudo_unavailable)は核(U1c)の側で、ここでは試さない。

const admin = "sitate-adm"

func sudoAdmin(t *testing.T) {
	t.Helper()
	destructive(t)
	if _, err := exec.LookPath("sudo"); err != nil {
		t.Skip("sudo が無い容器。未検証")
	}
	for _, c := range []string{
		"id " + admin + " >/dev/null 2>&1 || useradd -m -s /bin/sh " + admin,
		"mkdir -p /etc/sudoers.d && echo '" + admin + " ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/90-sitate-test && chmod 440 /etc/sudoers.d/90-sitate-test",
	} {
		if out, err := shRoot(t, c, nil); err != nil {
			t.Fatalf("%s\n%s", c, out)
		}
	}
	t.Cleanup(func() { _, _ = shRoot(t, "rm -f /etc/sudoers.d/90-sitate-test; userdel -r "+admin+" 2>/dev/null", nil) })
}

// 管理アカウントとして `sudo -n sh -s` で流す。パスワードを求められる状態なら sudo -n が非 0 で落ちる。
func viaSudo(t *testing.T, script []byte) (string, error) {
	return shRoot(t, "setpriv --reuid="+admin+" --regid="+admin+" --init-groups sudo -n sh -s 2>/dev/null", script)
}

func viaRoot(t *testing.T, script []byte) (string, error) {
	return shRoot(t, "sh -s 2>/dev/null", script)
}

// 一時ディレクトリの名前の一覧。Go の試験が自分で作るもの(Test... と go-build...)は、並行して走る別パッケージの試験が増減させるため除く。
const lsTmp = `ls -A /tmp "$TMPDIR" 2>/dev/null | grep -v -e '^Test' -e '^go-build' | sort`

// fsSnapshot は書き込みを見つけるための前後比較の材料(ファイルシステムの中身と一時ディレクトリの名前)。
// 容器の自分の入出力(/proc・/sys・/dev・/run・キャッシュ・作業の複製)と、PostgreSQL の試験が使う場所は除く。
func fsSnapshot(t *testing.T) string {
	t.Helper()
	cmd := `find / -xdev \( -path /proc -o -path /sys -o -path /dev -o -path /run -o -path /cache -o -path /src -o -path /w -o -path /var/lib/postgresql -o -path /var/log -o -path /tmp \) -prune -o ` +
		`\( -type d -printf '%p\t%y\t%m\t%U\n' -o -printf '%p\t%y\t%s\t%T@\t%m\t%U\n' \) | sort; echo ---; ` + lsTmp
	out, _ := shRoot(t, cmd, nil)
	return out
}

func surveyScript(t *testing.T, scope string) []byte {
	body, err := os.ReadFile("source-survey")
	if err != nil {
		t.Fatal(err)
	}
	return []byte("SITATE_SCOPE=" + sq(scope) + "\nSITATE_DU_TIMEOUT_S='30'\n" + string(body))
}

func TestSurveyViaRootAndSudoWritesNothing(t *testing.T) {
	sudoAdmin(t)
	for _, scope := range []string{"full", "db_only"} {
		for name, run := range map[string]func(*testing.T, []byte) (string, error){"sh -s": viaRoot, "sudo -n sh -s": viaSudo} {
			before := fsSnapshot(t)
			out, err := run(t, surveyScript(t, scope))
			after := fsSnapshot(t)
			if err != nil {
				t.Errorf("%s %s: 失敗: %v\n%.200s", scope, name, err, out)
				continue
			}
			if verr := contract.Validate("receiver/survey.json#/$defs/source_survey", []byte(out)); verr != nil {
				t.Errorf("%s %s: スキーマに合わない: %v", scope, name, verr)
			}
			if before != after {
				t.Errorf("%s %s: 元サーバーのファイルシステムが変わった(書いた・残した)\n%s", scope, name, lineDiff(before, after))
			}
		}
	}
}

func TestSetupViaSudoThenRootIdempotent(t *testing.T) {
	sudoAdmin(t)
	root := "/srv/sitate-sudo-root"
	_ = os.RemoveAll(root)
	defer os.RemoveAll(root)
	put(t, root+"/a/f", "x", 0o640)
	var pre strings.Builder
	for k, v := range filesVars(t, root, "203.0.113.9") {
		pre.WriteString(k + "=" + sq(v) + "\n")
	}
	body, _ := os.ReadFile("source-setup")
	script := []byte(pre.String() + string(body))
	tmpBefore, _ := shRoot(t, lsTmp, nil)
	out, err := viaSudo(t, script)
	if err != nil || !strings.Contains(out, `"step":"done"`) {
		t.Fatalf("sudo -n sh -s で導入できる: %v\n%s", err, out)
	}
	if after, _ := shRoot(t, lsTmp, nil); after != tmpBefore {
		t.Errorf("sudo 経由の導入が一時ファイルを残した:\n%s\n→\n%s", tmpBefore, after)
	}
	// 別の経路(root の sh -s)での再実行は全段 unchanged(同じ状態に揃う)
	out2, err := viaRoot(t, script)
	if err != nil {
		t.Fatalf("root の sh -s で再実行: %v\n%s", err, out2)
	}
	for _, ln := range strings.Split(strings.TrimSpace(out2), "\n") {
		if strings.HasPrefix(ln, "{") && !strings.Contains(ln, `"result":"unchanged"`) {
			t.Errorf("再実行で変わった段: %s", ln)
		}
	}
	if o, err := shRoot(t, "setpriv --reuid=sitate-ro --regid=sitate-ro --clear-groups cat "+root+"/a/f", nil); err != nil || o != "x" {
		t.Errorf("sudo 経由で付けた ACL で sitate-ro が読める: %v %q", err, o)
	}
}

// lineDiff は前後の一覧の差の行(最大 10 行)。
func lineDiff(a, b string) string {
	in := map[string]int{}
	for _, l := range strings.Split(a, "\n") {
		in[l]--
	}
	for _, l := range strings.Split(b, "\n") {
		in[l]++
	}
	var out []string
	for l, n := range in {
		if n != 0 && len(out) < 10 {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
