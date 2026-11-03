//go:build mainpath

package mainpath

// 第 3 段の DB の対象(fix-db): 変種 add の元サーバー(sawatari_ryo_shop・shopdb を持つクラスタ)の DB を対象に含む定義で、
// 実物の sitate の backup が DB の対象を ok で取り(ダンプが空でない)、dest への restore がダンプを固定名で置くことを確かめる。
// 主経路の第 1〜3 段の他の関数は DB のクラスタを declined にしてファイルの対象だけを通すので、DB の取得はここだけが通す。

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestStage3DB(t *testing.T) {
	if os.Getenv("SITATE_ACCEPT_NAME") == "" {
		t.Fatal("受入試験の環境の変数が無い(ACCEPT=<NAME> を付けて実行)")
	}
	w := newS2(t)
	w.dbTargets = []string{"sawatari_ryo_shop", "shopdb"}
	w.initAndStorage()
	w.enrollSource()
	o := w.g.call("", "backup").ok(t, "backup(DB の対象を含む)")
	t.Logf("backup: %.1500s", o.Env.Result)
	var r struct {
		Ref     string `json:"snapshot_ref"`
		Health  string `json:"health"`
		Targets []struct {
			ID, Status string
			Bytes      int64
			ErrorCode  *string `json:"error_code"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(o.Env.Result, &r); err != nil || len(r.Targets) != 1+len(w.dbTargets) {
		t.Fatalf("backup の結果の targets[] が期待と違う: %v %.600s", err, o.Env.Result)
	}
	for _, tg := range r.Targets {
		if tg.Status != "ok" {
			ec := ""
			if tg.ErrorCode != nil {
				ec = *tg.ErrorCode
			}
			t.Errorf("対象 %s が ok でない: status=%s error_code=%s\nadapter-stderr: %s", tg.ID, tg.Status, ec, w.bak.root("cat "+stateDir+"/adapter-stderr.log | tail -c 3000"))
		}
		if tg.ID != "t1" && tg.Bytes <= 0 {
			t.Errorf("DB の対象 %s のダンプが空(bytes=%d)", tg.ID, tg.Bytes)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Logf("health=%s", r.Health)
	w.installCore()
	snap := w.snapshotKey(r.Ref)
	to := destRestoreRoot + "-db"
	w.dst.root("install -d -m 0700 -o sitate -g sitate " + to)
	ro := w.restoreOnDest(r.Ref, to, snap).ok(t, "dest の restore(DB を含む)")
	t.Logf("restore: %.1200s", ro.Env.Result)
	for i := range w.dbTargets {
		id := "t" + strconv.Itoa(i+2)
		for _, f := range []string{"db.dump", "globals.sql"} {
			p := fmt.Sprintf("%s/%s/%s", to, id, f)
			sz := strings.TrimSpace(w.dst.root("stat -c %s " + p))
			if n, _ := strconv.Atoi(sz); n <= 0 {
				t.Errorf("復元先の %s が空か無い(%q)", p, sz)
			}
		}
		// ダンプは pg_dump のカスタム形式(先頭 5 バイトが PGDMP。復元先に pg_restore は無いので先頭の印で確かめる)。
		if magic := strings.TrimSpace(w.dst.root("head -c 5 " + fmt.Sprintf("%s/%s/db.dump", to, id))); magic != "PGDMP" {
			t.Errorf("復元先の %s/db.dump の先頭が PGDMP でない: %q", id, magic)
		}
	}
}
