//go:build mainpath

package mainpath

// 受入試験 A6 の CLI を直接使う回(仕様書 6.5 A6 の (1)〜(10)(12)。台本 test/acceptance/scripts/A6/script.md)。
// 実物の sitate を gate 経由で呼び、各回の封筒を check_a6.py --cli-results の形(1 行 {"item","argv","envelope"})の JSONL に書き、
// 同じ期待(a6Expect)で試験の中でも判定する。呼び方: make test-mainpath と同じ環境の前提(ACCEPT=<NAME>)で
//   go test -count=1 -v -timeout 40m -tags test,mainpath -run '^TestA6CLI$' ./test/mainpath/
// 出力先は環境変数 A6_CLI_RESULTS(既定は試験の一時ディレクトリ)。

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// a6Expect は項目 → 拒否の原因コード(A6-3a・3b・4・5 は apply の failed[].code / waiting[].reason。他は封筒の /error/code。test/tools/check_a6.py の EXPECT と同じ。A6-6 は a6Expire で別に判定する)。
var a6Expect = map[string]string{
	"A6-1": "SIGNATURE_INVALID", "A6-1-broken": "SIGNATURE_INVALID", "A6-1-proposed": "CHANGE_STATE_INVALID", "A6-2": "SIGNATURE_INVALID", "A6-3a": "PRECONDITION_CHANGED", "A6-3b": "SIGNATURE_INVALID",
	"A6-4": "DELAY_NOT_ELAPSED", "A6-5": "DELAY_NOT_ELAPSED", "A6-7": "SIGNATURE_INVALID", "A6-8": "ALREADY_SEALED",
	"A6-9": "APPROVAL_REQUIRED", "A6-3a-competing": "PRECONDITION_CHANGED", "A6-10": "NOTIFY_PROOF_MISMATCH", "A6-12": "DEF_CHANGE_MIXED",
}

type a6Run struct {
	*s2
	out   *os.File
	items map[string]bool
}

// record は封筒(標準出力の最後のレコード)を JSONL に 1 行書き、期待のある項目なら判定する。
func (r *a6Run) record(item string, argv []string, o cliOut, extra ...any) {
	t := r.t
	lines := strings.Split(strings.TrimSpace(o.Raw), "\n")
	last := []byte(lines[len(lines)-1])
	m := map[string]any{"item": item, "argv": argv, "envelope": json.RawMessage(last)}
	for i := 0; i+1 < len(extra); i += 2 {
		m[extra[i].(string)] = extra[i+1]
	}
	row, _ := json.Marshal(m)
	if _, err := r.out.Write(append(row, '\n')); err != nil {
		t.Fatalf("結果の JSONL に書けない: %v", err)
	}
	r.items[item] = true
	if want, ok := a6Expect[item]; ok {
		if a6ApplyItems[item] {
			// apply の拒否は結果の failed[].code / waiting[].reason に載る(仕様書 3.1.2)。封筒の /error/code でも可。適用されていないこと(applied[] が空)も確かめる。
			if !(o.code() == want || strings.Contains(applyCodes(o), ":"+want)) || applied(o) {
				t.Errorf("%s: 期待は拒否の原因 %s で applied[] が空、実際は ok=%v %s 結果=%.300s", item, want, o.Env.OK, o.code(), detailOf(o))
				return
			}
		} else if o.Env.OK || o.code() != want {
			t.Errorf("%s: 期待は ok=false・%s、実際は ok=%v %s detail=%.300s", item, want, o.Env.OK, o.code(), detailOf(o))
			return
		}
	}
	t.Logf("%s: ok=%v %s(終了 %d)", item, o.Env.OK, o.code(), o.Code)
}

var a6ApplyItems = map[string]bool{"A6-1": true, "A6-1-broken": true, "A6-3a": true, "A6-3b": true, "A6-4": true, "A6-5": true, "A6-3a-competing": true}

// applied は apply の結果の applied[] が空でないか。
func applied(o cliOut) bool {
	var r struct {
		Applied []json.RawMessage `json:"applied"`
	}
	_ = json.Unmarshal(o.Env.Result, &r)
	return len(r.Applied) > 0
}

// applyCodes は apply の結果の failed[].code と waiting[].reason(封筒は ok=true のまま、拒否の原因はここに載る)。
func applyCodes(o cliOut) string {
	var r struct {
		Failed  []struct{ Code string }   `json:"failed"`
		Waiting []struct{ Reason string } `json:"waiting"`
	}
	_ = json.Unmarshal(o.Env.Result, &r)
	var out []string
	for _, f := range r.Failed {
		out = append(out, "failed:"+f.Code)
	}
	for _, w := range r.Waiting {
		out = append(out, "waiting:"+w.Reason)
	}
	return strings.Join(out, ",")
}

func detailOf(o cliOut) string {
	if o.Env.Error == nil {
		return string(o.Env.Result)
	}
	return string(o.Env.Error.Detail)
}

func (r *a6Run) call(item, stdin string, argv ...string) cliOut {
	o := r.g.call(stdin, argv...)
	r.record(item, argv, o)
	return o
}

func (r *a6Run) propose(label string, stdin string, argv ...string) string {
	o := r.g.call(stdin, append([]string{"propose"}, argv...)...).ok(r.t, label)
	id, _ := o.resultMap(r.t)["change_id"].(string)
	if id == "" {
		r.t.Fatalf("%s: change_id が無い", label)
	}
	return id
}

// approveWith は pending から計算し直した canonical_hash に priv で署名して承認する(別の鍵を渡せば他の鍵の署名になる)。
func (r *a6Run) approveWith(id string, priv ed25519.PrivateKey, signedAt time.Time) (cliOut, []string) {
	pv := r.pendingView(id)
	sa := signedAt.UTC().Format(time.RFC3339)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte("sitate-approval/v1\n"+pv.Hash+"\n"+sa)))
	argv := []string{"approve", "--id", id, "--signature", sig, "--signed-at", sa}
	return r.g.call("", argv...), argv
}

// toNotBefore は共有の模擬時刻を change の not_before の 1 分後へ進める(戻さない)。
func (r *a6Run) toNotBefore(id string) {
	nb, err := time.Parse(time.RFC3339, str(r.pendingView(id).NotBefore))
	if err != nil {
		r.t.Fatalf("%s: not_before が読めない", id)
	}
	r.setSimTime(nb.Add(time.Minute))
}

func (r *a6Run) approveOK(id string) {
	o, _ := r.approveWith(id, r.approvalPriv, r.simNow())
	o.ok(r.t, "approve")
}

// a6Flow は (1)〜(5)(7): 提案 → 承認 → (遅延の経過) → 前提の変更 → apply。
func (r *a6Run) a6Flow() {
	t := r.t
	// A6-1 署名なしの apply: 承認の記録(pending/approvals/<id>.json)の署名を欠かせる(試験の側で書き換える。連鎖 log.jsonl は触らない)。
	id := r.propose("A6-1 propose", "", "delay-change", "--hours", "24")
	r.approveOK(id)
	r.toNotBefore(id)
	r.bak.root(`f=` + stateDir + `/pending/approvals/` + id + `.json; sed -i 's/"sig":"[^"]*"/"sig":""/' $f; grep -c '"sig":""' $f`)
	r.call("A6-1", "", "apply", "--id", id)
	// 参考(A6-1-broken。同じ判定): 署名を壊す(別の 64 バイトの値にする)。
	id = r.propose("A6-1-broken propose", "", "delay-change", "--hours", "24")
	r.approveOK(id)
	r.toNotBefore(id)
	r.bak.root(`f=` + stateDir + `/pending/approvals/` + id + `.json; sed -i 's/"sig":"[^"]*"/"sig":"` + base64.StdEncoding.EncodeToString(make([]byte, 64)) + `"/' $f; grep -c '"sig":"AAAA' $f`)
	r.call("A6-1-broken", "", "apply", "--id", id)
	// 参考(A6-1-proposed。数えない): 承認の記録が無い proposed の変更への apply は CHANGE_STATE_INVALID(仕様書 3.1.3)。
	id = r.propose("A6-1-proposed propose", "", "delay-change", "--hours", "24")
	r.call("A6-1-proposed", "", "apply", "--id", id)
	r.g.call("", "cancel", "--id", id)
	// A6-2 他の鍵の署名。approve が拒めばその封筒、受理すれば遅延の経過の後の apply の封筒を判定する。
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	id = r.propose("A6-2 propose", "", "delay-change", "--hours", "24")
	if o, argv := r.approveWith(id, other, r.simNow()); !o.Env.OK {
		r.record("A6-2", argv, o)
	} else {
		t.Logf("A6-2: approve は他の鍵の署名を受理した。apply の封筒を判定する")
		r.toNotBefore(id)
		r.call("A6-2", "", "apply", "--id", id)
	}
	r.g.call("", "cancel", "--id", id)

	// A6-3a 承認後に、承認済みの定義の置き場 def/approved.json(核が前提のハッシュを取る定義ファイル。{"def_hash","normalized"})の内容を変えた apply。
	// 内容(normalized)に項目を足し、def_hash を別の値にする。終わりに元へ戻す。
	id = r.propose("A6-3a propose", "", "delay-change", "--hours", "30")
	r.approveOK(id)
	r.toNotBefore(id)
	r.bak.root("cp " + stateDir + "/def/approved.json /root/a6-def.bak")
	r.bak.root(`f=` + stateDir + `/def/approved.json; sed -i 's/"def_hash":"sha256:[0-9a-f]*"/"def_hash":"sha256:` + strings.Repeat("0", 64) + `"/;s/"normalized":{/"normalized":{"timezone":"UTC",/' $f; grep -c '"timezone":"UTC"' $f`)
	r.call("A6-3a", "", "apply", "--id", id)
	r.bak.root("cat /root/a6-def.bak > " + stateDir + "/def/approved.json")
	// 参考(A6-3a-competing。台本の判定に数えない): 先に別の変更が適用されて承認済みの定義が変わった後の apply(複数の変更の競合。仕様書 3.6)。
	ida := r.propose("A6-3a-competing propose A", "", "delay-change", "--hours", "28")
	idb := r.propose("A6-3a-competing propose B", "", "delay-change", "--hours", "29")
	r.approveOK(ida)
	r.approveOK(idb)
	r.toNotBefore(ida)
	r.toNotBefore(idb)
	r.g.call("", "apply", "--id", idb).ok(t, "A6-3a-competing apply B")
	r.call("A6-3a-competing", "", "apply", "--id", ida)
	// 参考(台本の判定に数えない A6-3a-yaml): /etc/sitate/sitate.yaml(人向けの写し)を書き換えた apply。値は現在と同じ 24 時間にして定義を変えない。
	id = r.propose("A6-3a-yaml propose", "", "delay-change", "--hours", "24")
	r.approveOK(id)
	r.toNotBefore(id)
	r.bak.root("cp /etc/sitate/sitate.yaml /root/a6-yaml.bak; printf '\\n# a6 edit\\n' >> /etc/sitate/sitate.yaml")
	r.call("A6-3a-yaml", "", "apply", "--id", id)
	r.bak.root("cat /root/a6-yaml.bak > /etc/sitate/sitate.yaml")

	// A6-3b 承認後に変更の記録(pending/changes/<id>.json)を書き換えた apply
	id = r.propose("A6-3b propose", "", "delay-change", "--hours", "31")
	r.approveOK(id)
	r.toNotBefore(id)
	r.bak.root(`f=` + stateDir + `/pending/changes/` + id + `.json; sed -i 's/"hours":31/"hours":8/' $f; grep -c '"hours":8' $f`)
	r.call("A6-3b", "", "apply", "--id", id)

	// A6-4 遅延中の apply
	id = r.propose("A6-4 propose", "", "delay-change", "--hours", "25")
	r.approveOK(id)
	r.call("A6-4", "", "apply", "--id", id)

	// A6-5 遅延時間の短縮の即時適用
	id = r.propose("A6-5 propose", "", "delay-change", "--hours", "7")
	r.approveOK(id)
	r.call("A6-5", "", "apply", "--id", id)

	// A6-7 signed_at を 24 時間前にした署名での承認と apply
	id = r.propose("A6-7 propose", "", "delay-change", "--hours", "26")
	if o, argv := r.approveWith(id, r.approvalPriv, r.simNow().Add(-24*time.Hour)); !o.Env.OK {
		r.record("A6-7", argv, o)
	} else {
		t.Logf("A6-7: approve は 24 時間前の signed_at を受理した。apply の封筒を判定する")
		r.toNotBefore(id)
		r.call("A6-7", "", "apply", "--id", id)
	}
}

// a6Refusals は (8)(9)(12): 確定済みの状態での拒否。
func (r *a6Run) a6Refusals() {
	t := r.t
	r.call("A6-8", r.definition()+"\n", "propose", "seal", "--file", "-")
	r.call("A6-9", storageAdmin(), "init-storage", "--endpoint", r.s3.endpoint, "--bucket", r.s3.bucket, "--prefix", "sitate/", "--adapter", provisionAdapter, "--admin-ref", "admin", "--secrets-stdin")
	// def-change の項目(timezone)と restore.ssh_from を 1 つの差分に入れる。
	var d map[string]any
	if err := json.Unmarshal([]byte(r.definition()), &d); err != nil {
		t.Fatal(err)
	}
	d["timezone"] = "UTC"
	d["restore"].(map[string]any)["ssh_from"] = r.backupIP
	b, _ := json.Marshal(d)
	r.call("A6-12", string(b)+"\n", "propose", "def-change", "--file", "-")
}

// a6Expire は (6): 通知先(hook)を到達不能にして、承認 → 遅延の経過 → apply を繰り返し、延長の合計 7 日で expired になることを確かめる。
func (r *a6Run) a6Expire() {
	t := r.t
	// hook の名前を、何も待ち受けないアドレスへ向ける(バックアップサーバーの /etc/hosts。終わりに戻す)。
	r.bak.root("cp /etc/hosts /root/a6-hosts.bak; echo '127.0.0.2 hook' >> /etc/hosts")
	defer r.bak.root("cat /root/a6-hosts.bak > /etc/hosts")
	before := len(hookRecords(t, hookDest))
	id := r.propose("A6-6 propose", "", "delay-change", "--hours", "24")
	r.approveOK(id)
	nb0 := str(r.pendingView(id).NotBefore)
	var lastNB string
	waits := 0
	for i := 0; i < 20; i++ {
		r.toNotBefore(id)
		o := r.g.call("", "apply", "--id", id)
		pv := r.pendingView(id)
		nb := str(pv.NotBefore)
		extra := []any{"not_before", nb}
		if i == 0 {
			extra = append(extra, "not_before_before", nb0)
		}
		r.record("A6-6", []string{"apply", "--id", id}, o, extra...)
		t.Logf("A6-6 apply %d: ok=%v %s %s state=%s not_before=%s", i+1, o.Env.OK, o.code(), applyCodes(o), pv.State, nb)
		if applied(o) {
			t.Errorf("A6-6: 通知先が到達不能なのに apply が適用した")
		}
		if strings.Contains(applyCodes(o), "waiting:DELAY_NOT_ELAPSED") {
			waits++
			if lastNB != "" && nb <= lastNB {
				t.Errorf("A6-6: 延長で not_before が後ろへ動いていない: %s -> %s", lastNB, nb)
			}
			lastNB = nb
		}
		if pv.State == "expired" {
			break
		}
	}
	n := len(hookRecords(t, hookDest)) - before
	if n != 0 {
		t.Errorf("A6-6: hook に %d 件届いた(到達不能のはず)", n)
	}
	if t0, e1 := time.Parse(time.RFC3339, nb0); e1 != nil {
		t.Errorf("A6-6: not_before が読めない")
	} else if t1, e2 := time.Parse(time.RFC3339, lastNB); e2 != nil || t1.Sub(t0) < 7*24*time.Hour {
		t.Errorf("A6-6: 延長の合計が 7 日に満たない(%s -> %s、延長 %d 回)", nb0, lastNB, waits)
	}
	p := r.g.call("", "pending", "--id", id)
	r.record("A6-6", []string{"pending", "--id", id}, p, "hook_deliveries", n)
	if got := str(mapAt(p.Env.Result, "changes", 0, "state")); got != "expired" {
		t.Errorf("A6-6: pending の /result/changes/0/state = %q(期待 expired)", got)
	}
}

// a6ChainBreak は参考(A6-3b-log。台本の判定に数えない): 承認後に pending/log.jsonl を書き換えた apply(連鎖が切れる)。最後に行う。
func (r *a6Run) a6ChainBreak() {
	id := r.propose("A6-3b-log propose", "", "delay-change", "--hours", "27")
	r.approveOK(id)
	r.toNotBefore(id)
	r.bak.root("sed -i '1s/[0-9]/9/' " + stateDir + "/pending/log.jsonl")
	r.call("A6-3b-log", "", "apply", "--id", id)
}

func mapAt(raw json.RawMessage, key string, i int, field string) any {
	var m map[string][]map[string]any
	if json.Unmarshal(raw, &m) != nil || len(m[key]) <= i {
		return nil
	}
	return m[key][i][field]
}

func TestA6CLI(t *testing.T) {
	if os.Getenv("SITATE_ACCEPT_NAME") == "" {
		t.Fatal("受入試験の環境の変数が無い(ACCEPT=<NAME> を付けて実行)")
	}
	path := os.Getenv("A6_CLI_RESULTS")
	if path == "" {
		path = filepath.Join(t.TempDir(), "a6-cli-results.jsonl")
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	t.Logf("結果の JSONL: %s", filepath.Base(path))
	w := newS2(t)
	w.notifyTo = hookDest
	r := &a6Run{s2: w, out: f, items: map[string]bool{}}
	// 導入(第 3 段と同じ経路。seal の前まで)。
	w.dryRunsBeforeDefinition()
	w.setupNotify()
	w.initAndStorage()
	w.enrollSource()
	w.backupAndList()
	w.destEnrollAndCheck()
	w.dst.via = w.bak
	// A6-10: 確認語と違う語の承認 → 正しい語で承認 → apply で seal。
	w.g.call("", "notify", "test").ok(t, "notify test")
	word := lastConfirmationWord(t, hookDest)
	ps := w.g.call(w.definition()+"\n", "propose", "seal", "--file", "-").ok(t, "propose seal")
	id, _ := ps.resultMap(t)["change_id"].(string)
	pv := w.pendingView(id)
	signedAt := w.simNow().Format(time.RFC3339)
	proof, _ := json.Marshal(map[string]string{hookDest: "wrong wrong wrong"})
	argv := []string{"approve", "--id", id, "--signature", w.sign(pv.Hash, signedAt), "--signed-at", signedAt, "--notify-proof-stdin"}
	r.call("A6-10", string(proof)+"\n", argv...)
	w.approveAndApply(id, word)
	if !w.sealedFlag() {
		t.Fatal("seal が済んでいない")
	}
	r.a6Flow()
	r.a6Refusals()
	r.a6Expire()
	r.a6ChainBreak()
	for _, it := range []string{"A6-1", "A6-2", "A6-3a", "A6-3b", "A6-4", "A6-5", "A6-6", "A6-7", "A6-8", "A6-9", "A6-10", "A6-12"} {
		if !r.items[it] {
			t.Errorf("%s の結果が無い", it)
		}
	}
}
