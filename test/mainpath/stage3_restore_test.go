//go:build mainpath

package mainpath

// 第 3 段の復元: seal の後の restore-test(合格と失敗の経路)、復元先に平文が残らないこと、世代ごとの別のサーバーへの復元の照合。

import (
	"encoding/json"
	"fmt"
	"strings"
)

const restoreUserHome = "/home/sitate-restore" // ssh-host の専用ユーザーの作業場所(adapters/restore-dest/ssh-host の enroll)

type rtResult struct {
	RestoreOK  bool   `json:"restore_ok"`
	BackupRef  string `json:"backup_ref"`
	KeyringOK  bool   `json:"keyring_ok"`
	DefCopyOK  bool   `json:"def_copy_ok"`
	ConfigOK   bool   `json:"config_copy_ok"`
	Deferred   bool   `json:"deferred"`
	EgressByte int64  `json:"egress_bytes"`
	Teardown   *struct {
		Done      bool              `json:"done"`
		Remaining []json.RawMessage `json:"remaining"`
	} `json:"teardown"`
}

// passingRestoreTest は seal の後の restore-test: restore_ok・keyring_ok・def_copy_ok・config_copy_ok がすべて真で、直近の世代を復元する。
// 終わったら復元先に平文が残っていないことを、管理アクセスで確かめられる範囲で確かめる。
func (w *s2) passingRestoreTest(latest string) {
	t := w.t
	var o cliOut
	w.dst.peakRSS(t, "restore-test", func() {
		w.bak.peakRSS(t, "restore-test", func() { o = w.g.call("", "restore-test").ok(t, "restore-test(seal の後)") })
	})
	var rr rtResult
	if err := json.Unmarshal(o.Env.Result, &rr); err != nil {
		t.Fatalf("restore-test の結果: %v", err)
	}
	t.Logf("restore-test: %.700s", o.Env.Result)
	if !rr.RestoreOK || !rr.KeyringOK || !rr.DefCopyOK || !rr.ConfigOK || rr.Deferred || rr.BackupRef != latest || rr.EgressByte <= 0 {
		t.Fatalf("restore-test の結果が期待と違う(期待: 直近の世代 %s、restore_ok・keyring_ok・def_copy_ok・config_copy_ok がすべて真、送らない、読み出し>0)", latest)
	}
	if rr.Teardown == nil || !rr.Teardown.Done || len(rr.Teardown.Remaining) != 0 {
		t.Fatalf("復元先の後始末が残存なしで終わっていない: %s", o.Env.Result)
	}
	if len(hookEvents(t, hookDest, "restore_test.passed")) == 0 {
		t.Fatalf("restore_test.passed が hook に届いていない")
	}
	w.noPlaintextOnDest()
}

// noPlaintextOnDest は restore-test の後の dest を管理アクセス(バックアップサーバー経由の SSH)で調べる:
// 直近の世代の元データに入れた目印の文字列がどのファイルにも無いこと、専用ユーザーの処理が無いこと、作業ディレクトリが空であること。
// 使い捨て暗号化領域の鍵は復元先のメモリだけにあるため、メモリ上の平文までは見ていない。
func (w *s2) noPlaintextOnDest() {
	t := w.t
	if w.lastMarker == "" {
		t.Fatal("目印が無い")
	}
	out := w.dst.root(`grep -rIl -D skip --exclude-dir=proc --exclude-dir=sys -F ` + shq(w.lastMarker) + ` / 2>/dev/null || true`)
	if strings.TrimSpace(out) != "" {
		t.Errorf("復元先に平文の目印を含むファイルが残っている: %s", out)
	}
	ps := strings.TrimSpace(w.dst.root(`ps -u sitate-restore -o pid,args --no-headers || true`))
	for _, l := range strings.Split(ps, "\n") {
		if l != "" && !strings.Contains(l, "podman pause") && !strings.HasSuffix(strings.TrimSpace(l), " podman") {
			t.Errorf("復元先に専用ユーザーの処理が残っている: %s", l)
		}
	}
	t.Logf("復元先の専用ユーザーの処理(ps): %q", ps)
	left := w.dst.root(`ls -A ` + restoreUserHome + `/.sitate/run | wc -l`)
	if strings.TrimSpace(left) != "0" {
		t.Errorf("復元先の作業ディレクトリ ~/.sitate/run が空でない:\n%s", w.dst.root(`ls -lA `+restoreUserHome+`/.sitate/run`))
	}
	t.Logf("復元先(dest)を管理アクセスで調べた: 目印を含むファイル 0、作業ディレクトリ空、専用ユーザーの処理は podman の pause のみ")
}

// failedRestoreTest は失敗の経路: 復元先の専用ユーザーの作業場所を壊して(dest 側の障害)restore-test を実行し、
// 失敗が記録(status.last_restore_test)され、restore_test.failed が hook に届くことを確かめる。直した後は合格に戻る(別の関数)。
func (w *s2) failedRestoreTest(latest string) {
	t := w.t
	run := restoreUserHome + "/.sitate/run"
	w.dst.root(`chown root:root ` + run + ` && chmod 0500 ` + run)
	before := len(hookEvents(t, hookDest, "restore_test.failed"))
	o := w.g.call("", "restore-test")
	t.Logf("障害を入れた restore-test: ok=%v code=%s exit=%d %.500s", o.Env.OK, o.code(), o.Code, o.Env.Result)
	if o.Env.Error != nil {
		t.Logf("  error.detail: %.500s", o.Env.Error.Detail)
	}
	if o.Env.OK || o.code() == "" {
		t.Fatalf("復元先の作業場所を壊したのに restore-test が失敗しない")
	}
	w.dst.root(`chown sitate-restore:sitate-restore ` + run + ` && chmod 0700 ` + run)
	ev := hookEvents(t, hookDest, "restore_test.failed")
	if len(ev) != before+1 {
		t.Fatalf("restore_test.failed が hook に 1 件届いていない(前 %d 件、今 %d 件)", before, len(ev))
	}
	last := ev[len(ev)-1]
	if got, _ := last.fact("error_code").(string); got != o.code() {
		t.Fatalf("restore_test.failed の error_code=%v が封筒の原因コード %s と違う", last.fact("error_code"), o.code())
	}
	st := w.g.call("", "status").ok(t, "status")
	var s struct {
		Last struct {
			OK                  bool `json:"ok"`
			ConsecutiveFailures int  `json:"consecutive_failures"`
		} `json:"last_restore_test"`
	}
	_ = json.Unmarshal(st.Env.Result, &s)
	if s.Last.OK || s.Last.ConsecutiveFailures != 1 {
		t.Fatalf("status.last_restore_test が失敗を記録していない: %s", fmt.Sprint(string(st.Env.Result))[:400])
	}
	t.Logf("失敗の経路: error_code=%s、restore_test.failed が hook に届き、status.last_restore_test.ok=false(consecutive_failures=1)", o.code())
}
