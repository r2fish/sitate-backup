//go:build mainpath

package mainpath

// 第 3 段の seal: notify test(確認語を hook で受ける)→ propose seal → 承認(署名と確認語)→ 遅延 → apply(仕様 3.6、3.8 の手順 10)。

import (
	"encoding/json"
	"strings"
	"time"
)

func (w *s2) sealedFlag() bool {
	var st struct {
		Sealed bool `json:"sealed"`
	}
	if err := json.Unmarshal([]byte(w.bak.root("cat "+stateDir+"/state.json")), &st); err != nil {
		w.t.Fatalf("state.json: %v", err)
	}
	return st.Sealed
}

func (w *s2) seal() {
	t := w.t
	if w.sealedFlag() {
		t.Fatal("seal の前なのに sealed が真")
	}
	// 通知先に確認語つきの試験通知を送る。人は通知で受け取った語を承認カードで入力する(台本は hook から読む)。
	nt := w.g.call("", "notify", "test").ok(t, "notify test")
	t.Logf("notify test: %.400s", nt.Env.Result)
	word := lastConfirmationWord(t, hookDest)
	if len(strings.Fields(word)) != 3 {
		t.Fatalf("確認語が 3 語でない")
	}
	ps := w.g.call(w.definition()+"\n", "propose", "seal", "--file", "-").ok(t, "propose seal")
	t.Logf("propose seal: %.600s", ps.Env.Result)
	id, _ := ps.resultMap(t)["change_id"].(string)
	if id == "" {
		t.Fatalf("propose seal が change_id を返さない")
	}
	if n := len(hookEvents(t, hookDest, "change.proposed")); n == 0 {
		t.Fatalf("change.proposed が hook に届いていない")
	}
	pv := w.pendingView(id)
	if pv.State != "proposed" || pv.Hash != pv.Computed {
		t.Fatalf("提案の状態: state=%s 記録の hash=%s 計算=%s delay_hours=%d", pv.State, pv.Hash, pv.Computed, pv.Delay)
	}
	t.Logf("提案 %s: delay_hours=%d、canonical_hash は台本が記録から計算し直した値と一致", id, pv.Delay)

	// 承認前の apply は適用しない。
	pre := w.g.call("", "apply", "--id", id)
	t.Logf("承認前の apply: ok=%v code=%s %.300s", pre.Env.OK, pre.code(), pre.Env.Result)
	if w.sealedFlag() {
		t.Fatalf("承認の前の apply で sealed になった")
	}
	// 確認語が違う承認は受理しない。
	bad := w.approveSeal(id, "wrong wrong wrong")
	bad.fails(t, "違う確認語の承認", "NOTIFY_PROOF_MISMATCH", bad.Code)
	if pv := w.pendingView(id); pv.State != "proposed" {
		t.Fatalf("違う確認語の承認で状態が変わった: %s", pv.State)
	}
	w.approveAndApply(id, word)
}

// approveAndApply は承認(署名と確認語)→ 遅延があれば待つ → apply → sealed の確認。
func (w *s2) approveAndApply(id, word string) {
	t := w.t
	ap := w.approveSeal(id, word).ok(t, "approve")
	t.Logf("approve: %.400s", ap.Env.Result)
	if got, _ := ap.resultMap(t)["notify_proof_ok"].(bool); !got {
		t.Fatalf("notify_proof_ok が真でない: %s", ap.Env.Result)
	}
	if len(hookEvents(t, hookDest, "change.approved")) == 0 {
		t.Fatalf("change.approved が hook に届いていない")
	}
	pv := w.pendingView(id)
	nb, err := time.Parse(time.RFC3339, str(pv.NotBefore))
	if pv.State != "approved" || err != nil {
		t.Fatalf("承認の後の状態: state=%s not_before=%v", pv.State, pv.NotBefore)
	}
	now := w.simNow()
	if d := nb.Sub(now); d < time.Duration(pv.Delay)*time.Hour-time.Minute || d > time.Duration(pv.Delay)*time.Hour+time.Minute {
		t.Fatalf("not_before が受理の時刻 + delay_hours(%d)でない: now=%s not_before=%s", pv.Delay, now, nb)
	}
	// seal の遅延は 0(提案の delay_hours。仕様 3.6 の表)。遅延があれば、途中の apply は適用せず、共有の時刻を進めてから適用する。
	if pv.Delay > 0 {
		mid := w.g.call("", "apply", "--id", id)
		t.Logf("遅延の途中の apply: ok=%v code=%s %.300s", mid.Env.OK, mid.code(), mid.Env.Result)
		if w.sealedFlag() {
			t.Fatalf("遅延の途中の apply で sealed になった")
		}
		w.setSimTime(nb.Add(time.Minute))
	}
	ar := w.g.call("", "apply", "--id", id).ok(t, "apply(遅延の後)")
	t.Logf("apply: %.600s", ar.Env.Result)
	if !w.sealedFlag() {
		t.Fatalf("apply の後も sealed が偽")
	}
	if pv := w.pendingView(id); pv.State != "applied" {
		t.Fatalf("apply の後の状態が applied でない: %s", pv.State)
	}
	if len(hookEvents(t, hookDest, "change.applied")) == 0 {
		t.Fatalf("change.applied が hook に届いていない")
	}
	// D-0164: /etc/sitate は bootstrap と同じ形(所有者 sitate・モード 0750)で、apply が sitate.yaml を書いた。
	if got := strings.TrimSpace(w.bak.root("stat -c '%U:%G %a' /etc/sitate")); got != "sitate:sitate 750" {
		t.Fatalf("/etc/sitate の所有者とモードが想定と違う: %q", got)
	}
	if got := w.bak.root("test -s /etc/sitate/sitate.yaml && echo present"); !strings.Contains(got, "present") {
		t.Fatalf("/etc/sitate/sitate.yaml が書かれていない")
	}
	t.Logf("seal 完了: sealed=true、change.proposed / approved / applied が hook に届いた")
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
