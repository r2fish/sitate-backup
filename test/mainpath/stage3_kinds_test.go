//go:build mainpath

package mainpath

// 第 3 段: seal の後の種別の通し(まとめレビュー H1・M1〜M3・M5。D-0165 の前後で台本に無かった種別)。hold-release、source-change、restore-change、
// notify-change、storage-init を、実物の gate 経由で propose → 承認(署名)→ 遅延 → apply まで通し、承認カードの材料(plan)が宛先と指紋を持つことを確かめる。

import (
	"encoding/json"
	"strings"
	"time"
)

func compact(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// planOf は pending --id の変更の plan。
func (w *s2) planOf(id string) map[string]any {
	p, _ := w.pendingView(id).raw["plan"].(map[string]any)
	if p == nil {
		w.t.Fatalf("%s の plan が無い", id)
	}
	return p
}

func (w *s2) proposeKind(label string, stdin string, argv ...string) string {
	o := w.g.call(stdin, append([]string{"propose"}, argv...)...).ok(w.t, label)
	id, _ := o.resultMap(w.t)["change_id"].(string)
	if id == "" {
		w.t.Fatalf("%s: change_id が無い", label)
	}
	w.t.Logf("%s: %s plan=%.500s", label, id, compact(w.planOf(id)))
	return id
}

// approveKind は承認(署名。確認語が要る種別は proofs)→ 遅延があれば共有の時刻を進める。
func (w *s2) approveKind(label, id string, proofs map[string]string) {
	t := w.t
	pv := w.pendingView(id)
	if pv.Hash != pv.Computed {
		t.Fatalf("%s: 記録の canonical_hash を台本が計算し直した値と違う", label)
	}
	signedAt := w.simNow().Format(time.RFC3339)
	argv := []string{"approve", "--id", id, "--signature", w.sign(pv.Hash, signedAt), "--signed-at", signedAt}
	stdin := ""
	if len(proofs) > 0 {
		argv, stdin = append(argv, "--notify-proof-stdin"), compact(proofs)+"\n"
	}
	w.g.call(stdin, argv...).ok(t, label+" approve")
	pv = w.pendingView(id)
	nb, err := time.Parse(time.RFC3339, str(pv.NotBefore))
	if pv.State != "approved" || err != nil {
		t.Fatalf("%s: 承認の後の状態 %s", label, pv.State)
	}
	if pv.Delay > 0 {
		w.setSimTime(nb.Add(time.Minute))
	}
}

// applyKind は apply して applied になるのを確かめる。applied にならなければ試験を失敗にして false を返す(後ろの種別は続けて確かめる)。
func (w *s2) applyKind(label, id, stdin string, extra ...string) bool {
	t := w.t
	o := w.g.call(stdin, append([]string{"apply", "--id", id}, extra...)...)
	t.Logf("%s apply: ok=%v %.500s", label, o.Env.OK, o.Env.Result)
	if pv := w.pendingView(id); !o.Env.OK || pv.State != "applied" {
		t.Errorf("%s: apply の後の状態が applied でない: %s(%.400s)", label, pv.State, o.Env.Result)
		return false
	}
	return true
}

func (w *s2) afterSealKinds() {
	w.kindHoldRelease()
	w.kindSourceChange()
	w.kindRestoreChange()
	w.kindNotifyChange()
	w.kindStorageInit()
	w.kindEmptySource()
}

// kindEmptySource は元データを空にした backup(D-0166): 対象は partial、警告 TARGET_EMPTY、世代は incomplete、backup.partial は critical、latest は前の世代のまま。
func (w *s2) kindEmptySource() {
	t := w.t
	latest := func() string {
		l := w.g.call("", "list").ok(t, "list")
		return str(l.resultMap(t)["latest"])
	}
	before := latest()
	w.src.root("find " + srcRoot + " -mindepth 1 ! -type d -delete") // ディレクトリ(対象の根)は残し、ファイルだけを消す
	o := w.g.call("", "backup").ok(t, "backup(元データが空)")
	t.Logf("backup(空): %.600s", o.Env.Result)
	r := o.resultMap(t)
	if r["health"] != "incomplete" {
		t.Errorf("0 件の世代の health = %v", r["health"])
	}
	tg, _ := r["targets"].([]any)
	for _, x := range tg {
		if str(x.(map[string]any)["status"]) != "partial" {
			t.Errorf("対象の status が partial でない: %v", r["targets"])
		}
	}
	if len(tg) == 0 || o.warnCount("TARGET_EMPTY") != len(tg) {
		t.Errorf("対象ごとに警告 TARGET_EMPTY が要る(%d 対象): %.300s", len(tg), o.Raw)
	}
	var crit bool
	for _, e := range hookEvents(t, hookDest, "backup.partial") {
		var b struct {
			Severity string `json:"severity"`
		}
		_ = json.Unmarshal(e.Body, &b)
		crit = crit || b.Severity == "critical"
	}
	if !crit {
		t.Errorf("backup.partial が critical で hook に届いていない")
	}
	if after := latest(); after != before {
		t.Errorf("latest が空の世代に動いた: %s -> %s", before, after)
	}
}

func (w *s2) kindHoldRelease() {
	t := w.t
	w.g.call("", "hold", "set").ok(t, "hold set")
	id := w.proposeKind("propose hold-release", "", "hold-release")
	if _, ok := w.planOf(id)["causes"]; !ok {
		t.Errorf("hold-release の plan に causes[] が無い")
	}
	w.approveKind("hold-release", id, nil)
	w.applyKind("hold-release", id, "")
}

func (w *s2) kindSourceChange() {
	t := w.t
	fp := accEnv(t, "SITATE_ACCEPT_SOURCE_HOST_KEY_FP")
	// host_key_fp は省く: 核が propose で資格情報なしに取得して plan に載せる(D-0166)。admin_user は再登録に使う管理用の利用者。
	src := map[string]any{"host": accEnv(t, "SITATE_ACCEPT_SOURCE_HOST"), "admin_user": adminUser(t),
		"targets":  []any{map[string]any{"id": "t1", "files": "/srv/shopapp"}, map[string]any{"id": "t2", "files": "/srv/shopapp/public"}},
		"declined": []any{map[string]any{"id": "d1", "postgresql_cluster": 5432}}} // 対象を足す = 受け口の再設置(再登録)を伴う変更
	id := w.proposeKind("propose source-change", "", "source-change", "--source", compact(src))
	p := w.planOf(id)
	if p["host_key_fp"] != fp || p["admin_user"] != adminUser(t) || p["needs_secret"] != true || !strings.Contains(compact(p["after"]), accEnv(t, "SITATE_ACCEPT_SOURCE_HOST")) {
		t.Errorf("source-change の plan に宛先・指紋・needs_secret が無い: %s", compact(p))
	}
	w.approveKind("source-change", id, nil)
	w.applyKind("source-change", id, w.sourceAdmin(), "--source-admin-ref", "admin", "--secrets-stdin")
}

func (w *s2) kindRestoreChange() {
	t := w.t
	fp := accEnv(t, "SITATE_ACCEPT_DEST_HOST_KEY_FP")
	rc := map[string]any{"adapter": "ssh-host", "dest": map[string]any{"at": w.destAt(), "key_ref": "restore-init-2"}} // 初期資格情報の参照名を替える = 再登録を伴う変更
	id := w.proposeKind("propose restore-change", "", "restore-change", "--restore", compact(rc))
	p := w.planOf(id)
	if p["host_key_fp"] != fp || !strings.Contains(compact(p["after"]), "ssh-host") {
		t.Errorf("restore-change の plan に宛先・指紋が無い: %s", compact(p))
	}
	w.approveKind("restore-change", id, nil)
	adm := map[string]string{"restore-init-2": string(readF(t, accEnv(t, "SITATE_ACCEPT_ADMIN_KEY_FILE")))}
	b, _ := json.Marshal(adm)
	w.applyKind("restore-change", id, string(b)+"\n", "--secrets-stdin")
	w.g.call("", "restore-dest", "check").ok(t, "restore-dest check(restore-change の後)")
}

// kindNotifyChange は通知先を足す notify-change: 資格情報は propose の --secrets-stdin、足した宛先へ試験通知が届き、人が確認語を承認で入力する。
// 適用の change.applied は変更前の宛先(ops-hook)にも届く(M5)。
func (w *s2) kindNotifyChange() {
	t := w.t
	const second = "ops-hook-2"
	before := len(hookEvents(t, hookDest, "change.applied"))
	url := accEnv(t, "SITATE_ACCEPT_HOOK_URL") + "/hooks/" + second
	sec, _ := json.Marshal(map[string]string{second: url})
	id := w.proposeKind("propose notify-change", string(sec)+"\n", "notify-change", "--notify", compact(map[string]any{"to": []string{hookDest, second}}), "--secrets-stdin")
	p := w.planOf(id)
	if !strings.Contains(compact(p["proof_required"]), second) || !strings.Contains(compact(p["after"]), second) {
		t.Errorf("notify-change の plan に足した宛先の確認が無い: %s", compact(p))
	}
	w.approveKind("notify-change", id, map[string]string{second: lastConfirmationWord(t, second)})
	w.applyKind("notify-change", id, "")
	if n := len(hookEvents(t, hookDest, "change.applied")); n <= before {
		t.Errorf("notify-change の change.applied が既存の宛先に届いていない")
	}
	if len(hookEvents(t, second, "change.applied")) == 0 {
		t.Errorf("notify-change の change.applied が足した宛先に届いていない")
	}
}

// kindStorageInit は seal の後の storage-init --reissue(M1): 承認の後の適用が成功し、新しい鍵で bucket check が通る。
func (w *s2) kindStorageInit() {
	t := w.t
	id := w.proposeKind("propose storage-init", "", "storage-init", "--endpoint", w.s3.endpoint, "--bucket", w.s3.bucket, "--prefix", "sitate/", "--adapter", provisionAdapter, "--reissue")
	if p := w.planOf(id); !strings.Contains(compact(p["bucket"]), w.s3.bucket) || p["reissue"] != true {
		t.Errorf("storage-init の plan に保管先が無い: %s", compact(p))
	}
	w.approveKind("storage-init", id, nil)
	w.applyKind("storage-init", id, storageAdmin(), "--admin-ref", "admin", "--secrets-stdin")
	w.g.call("", "bucket", "check").ok(t, "bucket check(storage-init の後)")
}

// warnCount は封筒の警告のうち code が一致するものの数。
func (o cliOut) warnCount(code string) int {
	n := 0
	for _, wn := range o.Env.Warnings {
		if wn.Code == code {
			n++
		}
	}
	return n
}
