//go:build mainpath

package mainpath

// 主経路の第 3 段(作業指示書 6 章): 第 2 段の経路(導入 → 初回バックアップ → 復元先の登録)の先を、実物の sitate で通す。
// seal(承認の署名・通知の確認語・遅延 → apply)→ seal の後の backup 2 回 → 復元テスト → 世代ごとの復元の照合 → seal の後の拒否 → 失敗の経路。
// 代役と通っていないものは test/records/mainpath/stage3-2026-11-03.md。

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// genRec は 1 つの世代: ref と、その backup の直後に台本が元データから計算した照合の行(保存済みマニフェストは使わない)。
type genRec struct {
	ref, name, want string
}

func (w *s2) setupNotify() {
	t := w.t
	url := accEnv(t, "SITATE_ACCEPT_HOOK_URL") + "/hooks/" + hookDest
	b, _ := json.Marshal(map[string]string{hookDest: url})
	o := w.g.call(string(b)+"\n", "notify", "set", "--to", hookDest, "--secret-ref", hookDest, "--secrets-stdin").ok(t, "notify set")
	t.Logf("notify set: %s", o.Env.Result)
}

func TestStage3MainPath(t *testing.T) {
	if os.Getenv("SITATE_ACCEPT_NAME") == "" {
		t.Fatal("受入試験の環境の変数が無い(ACCEPT=<NAME> を付けて実行)")
	}
	w := newS2(t)
	w.notifyTo = hookDest
	w.dryRunsBeforeDefinition() // 手順 2・3(D-0165)。定義の前
	w.setupNotify()
	w.initAndStorage()
	w.enrollSource()
	var gens []genRec
	// 導入の途中(seal の前)の最初の世代。
	ref0 := w.backupAndList()
	gens = append(gens, genRec{ref0, "seal の前", w.src.root(listing(srcRoot))})
	w.destEnrollAndCheck()
	// 登録の後の dest はバックアップサーバーからの SSH だけを受ける(隔離)。台本はバックアップサーバーに入った SSH の中から dest に入る。
	w.dst.via = w.bak

	w.seal()
	gens = append(gens, w.sealedBackups()...)
	w.verifyJournal()

	w.failedRestoreTest(gens[len(gens)-1].ref)
	w.passingRestoreTest(gens[len(gens)-1].ref)
	w.restoreEachGeneration(gens)
	w.afterSealDenied(gens)
	w.afterSealKinds()
	w.checkMlock()
}

// destEnrollAndCheck は restore-dest enroll → check(第 2 段と同じ呼び出し)。
func (w *s2) destEnrollAndCheck() {
	t := w.t
	o := w.g.call(w.sourceAdmin(), "restore-dest", "enroll", "--dest", w.destAt(), "--adapter", "ssh-host", "--admin-ref", "admin", "--secrets-stdin").ok(t, "restore-dest enroll")
	t.Logf("restore-dest enroll: %.400s", o.Env.Result)
	if got, _ := o.resultMap(t)["host_key_fp"].(string); got != accEnv(t, "SITATE_ACCEPT_DEST_HOST_KEY_FP") {
		t.Fatalf("restore-dest enroll の host_key_fp が起動時の記録と違う: %s", got)
	}
	c := w.g.call("", "restore-dest", "check").ok(t, "restore-dest check")
	t.Logf("restore-dest check: %.400s", c.Env.Result)
}

// mutateSource は元データを変える(元サーバーの管理アクセス)。種別・モード・サイズ・更新時刻・リンク先・内容を変える操作を混ぜる。
func (w *s2) mutateSource(n int, marker string) {
	d := fmt.Sprintf("%s/docs/gen%d", srcRoot, n)
	prev := fmt.Sprintf("%s/docs/gen%d", srcRoot, n-1)
	script := fmt.Sprintf(`set -eu
mkdir -p %[1]s
printf '%%s gen%[2]d\n' %[3]s > %[1]s/note.txt
touch -d '2026-10-%02[2]d 08:00:00 UTC' %[1]s/note.txt
ln -sfn note.txt %[1]s/latest-link
printf 'appended in gen%[2]d\n' >> %[4]s/docs/readme.txt
touch -d '2026-10-%02[2]d 09:30:00 UTC' %[4]s/docs/readme.txt
chmod %[5]s %[4]s/config/app.conf
printf 'edited in gen%[2]d\n' >> %[4]s/public/asset-1.css
touch -d '2026-10-%02[2]d 10:00:00 UTC' %[4]s/public/asset-1.css
`, d, n, shq(marker), srcRoot, map[int]string{1: "0640", 2: "0660"}[n])
	if n > 1 {
		script += "rm -rf " + prev + "\n"
	}
	w.src.root(script)
}

// sealedBackups は seal の後に元データを変えて backup を 2 回行い、各世代の ref と照合の行を返す。
func (w *s2) sealedBackups() []genRec {
	t := w.t
	var out []genRec
	for n := 1; n <= 2; n++ {
		marker := fmt.Sprintf("MP3-PLAINTEXT-MARKER-%d-%d", n, time.Now().UnixNano())
		w.mutateSource(n, marker)
		before := w.src.root(listing(srcRoot))
		var o cliOut
		w.bak.peakRSS(t, fmt.Sprintf("backup%d", n), func() { o = w.g.call("", "backup").ok(t, fmt.Sprintf("backup(seal の後 %d 回目)", n)) })
		t.Logf("backup(seal の後 %d 回目): %.500s", n, o.Env.Result)
		ref, _ := o.resultMap(t)["snapshot_ref"].(string)
		if ref == "" {
			t.Fatalf("backup の結果に snapshot_ref が無い: %s", o.Env.Result)
		}
		after := w.src.root(listing(srcRoot))
		if before != after {
			t.Fatalf("backup の間に元データが変わった(照合の期待値が定まらない)")
		}
		out = append(out, genRec{ref, fmt.Sprintf("seal の後 %d", n), after})
		w.lastMarker = marker
	}
	l := w.g.call("", "list").ok(t, "list")
	for _, g := range out {
		if !strings.Contains(string(l.Env.Result), g.ref) {
			t.Fatalf("list に世代 %s が無い", g.ref)
		}
	}
	if out[0].ref == out[1].ref {
		t.Fatalf("2 回の backup が同じ世代を返した: %s", out[0].ref)
	}
	t.Logf("list: %.800s", l.Env.Result)
	return out
}
