//go:build mainpath

package mainpath

// 第 3 段: 保持の通し(作業指示書 5.18)。verify → extend → prune を実物の sitate(gate 経由)と試験用の保管先の MinIO で行い、
// 模擬時間を 91 日進めて世代の保持期限を過ぎさせても、実際の Object Lock が残る版を prune が消さないこと(保管先も拒否する)を確かめる。
// 呼び出しは TestStage3MainPath の最後(w.retention(gens))。

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/store"
)

func arrLen(v any) int {
	a, _ := v.([]any)
	return len(a)
}

// liveLocks は保管先の全版のうち、実際のロックが now より先にある版の "キー versionID" の集合。
func (w *s2) liveLocks(now time.Time) map[string]bool {
	t := w.t
	wr := &store.ObjectStore{Endpoint: w.s3.endpoint, Bucket: w.s3.bucket, AccessKey: w.s3.wk, SecretKey: w.s3.ws, Prefix: "sitate/", Params: oneDay{}}
	ctx := context.Background()
	vs, err := wr.ListVersions(ctx, "")
	if err != nil {
		t.Fatalf("版付き一覧: %v", err)
	}
	out := map[string]bool{}
	for _, v := range vs {
		if v.DeleteMarker {
			continue
		}
		until, err := wr.RetainUntil(ctx, v.Key, v.VersionID)
		if err == nil && until.After(now) {
			out[v.Key+" "+v.VersionID] = true
		}
	}
	return out
}

func (w *s2) versionSet() map[string]bool {
	wr := &store.ObjectStore{Endpoint: w.s3.endpoint, Bucket: w.s3.bucket, AccessKey: w.s3.wk, SecretKey: w.s3.ws, Prefix: "sitate/", Params: oneDay{}}
	vs, err := wr.ListVersions(context.Background(), "")
	if err != nil {
		w.t.Fatalf("版付き一覧: %v", err)
	}
	out := map[string]bool{}
	for _, v := range vs {
		out[v.Key+" "+v.VersionID] = true
	}
	return out
}

func (w *s2) retention(gens []genRec) {
	w.retentionNow()
	w.retentionLockedPrune()
	w.retentionHold()
}

func (w *s2) retentionNow() {
	t := w.t
	// 1. verify: 記録した版 ID の存在・保持期限の照合。欠け・短い・長い・記録外の版が無く、記録の連鎖が切れていない。
	v := w.g.call("", "verify").ok(t, "verify")
	m := v.resultMap(t)
	t.Logf("verify: %.600s warnings=%v", v.Env.Result, v.Env.Warnings)
	for _, k := range []string{"missing", "retention_short", "retention_long", "unexpected_versions"} {
		if arrLen(m[k]) != 0 {
			t.Errorf("verify の %s が空でない: %v", k, m[k])
		}
	}
	if m["journal_chain_ok"] != true {
		t.Errorf("verify の journal_chain_ok = %v", m["journal_chain_ok"])
	}
	// 2. extend: 保持中の参照を延ばす。失敗が無く、最小の retain-until が分かる。
	e := w.g.call("", "extend").ok(t, "extend")
	em := e.resultMap(t)
	t.Logf("extend: %s", e.Env.Result)
	if arrLen(em["failed"]) != 0 || em["min_retain_until"] == nil {
		t.Errorf("extend: failed=%v min_retain_until=%v", em["failed"], em["min_retain_until"])
	}
	// 3. prune(いま): 何も期限を過ぎていないので 0 件。削除停止は無い。
	p := w.g.call("", "prune").ok(t, "prune(いま)")
	pm := p.resultMap(t)
	t.Logf("prune(いま): %s", p.Env.Result)
	if fmt.Sprint(pm["deleted"]) != "0" || pm["held"] != false {
		t.Errorf("いまの prune: %s", p.Env.Result)
	}
	// 4. prune に版を指定する引数は無い(gate も通さない)。
	for _, bad := range [][]string{{"prune", "--version-id", "x"}, {"prune", "--key", "manifests/x"}, {"prune", "--all"}} {
		o := w.g.call("", bad...)
		if o.Env.OK {
			t.Errorf("%s が成功した(任意の版を消せる引数は無いはず)", strings.Join(bad, " "))
		} else {
			t.Logf("%s: %s(終了 %d)", strings.Join(bad, " "), o.code(), o.Code)
		}
	}
}

func (w *s2) retentionLockedPrune() {
	t := w.t
	// 5. 模擬時間を 91 日進めて世代の保持期限を過ぎさせる。実際のロック(保管先の時計)は進まないので、期限切れの版の削除は保管先が拒否する。
	realNow := time.Now().UTC()
	locked := w.liveLocks(realNow)
	if len(locked) == 0 {
		t.Fatalf("実際のロックが生きている版が 1 つも無い(受入試験の環境の既定保持が 90 日か)")
	}
	before := w.versionSet()
	w.setSimTime(w.simNow().AddDate(0, 0, 91))
	p2 := w.g.call("", "prune").ok(t, "prune(模擬 +91 日)")
	t.Logf("prune(模擬 +91 日): %s warnings=%v", p2.Env.Result, p2.Env.Warnings)
	p2m := p2.resultMap(t)
	if fmt.Sprint(p2m["skipped_locked"]) == "0" {
		t.Errorf("期限切れの世代の版の削除を試みていない(skipped_locked = 0): %s", p2.Env.Result)
	}
	after := w.versionSet()
	var lost []string
	for k := range locked {
		if before[k] && !after[k] {
			lost = append(lost, k)
		}
	}
	sort.Strings(lost)
	if len(lost) > 0 {
		t.Errorf("実際のロックが生きている版が消えた(%d 件): %v", len(lost), lost)
	}
	t.Logf("ロック中の版 %d 件のうち消えたもの %d 件(prune 前の版 %d、後 %d)", len(locked), len(lost), len(before), len(after))
	// prune の後も記録した版はすべて在る(欠けは無い)。保護中の世代は復元に使える。
	v2 := w.g.call("", "verify").ok(t, "verify(prune の後)")
	if n := arrLen(v2.resultMap(t)["missing"]); n != 0 {
		t.Errorf("prune の後に verify の missing が %d 件: %s", n, v2.Env.Result)
	}
}

func (w *s2) retentionHold() {
	t := w.t
	vBefore := w.versionSet()
	// 6. 削除停止: hold set の後の prune は何も消さない(held)。
	w.g.call("", "hold", "set").ok(t, "hold set")
	p3 := w.g.call("", "prune").ok(t, "prune(削除停止中)")
	p3m := p3.resultMap(t)
	t.Logf("prune(削除停止中): %s", p3.Env.Result)
	if p3m["held"] != true || fmt.Sprint(p3m["deleted"]) != "0" || fmt.Sprint(p3m["skipped_locked"]) != "0" {
		t.Errorf("削除停止中の prune が削除を試みた: %s", p3.Env.Result)
	}
	// hold set は記録(journal/)の版を足すので、数でなく「前にあった版が 1 つも消えていない」で見る。
	vAfter := w.versionSet()
	for k := range vBefore {
		if !vAfter[k] {
			t.Errorf("削除停止中の prune の後に版が消えた: %s", k)
		}
	}
}
