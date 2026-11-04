//go:build mainpath

package mainpath

// 元サーバーの調査の通し(作業指示書 5.18 の完了条件 8。A2 の変種 (c): DB の対象を持たないクラスタを元サーバーに置いた環境)。
// source survey → def validate(調査の候補をすべて対象か除外として書く。クラスタは source.declined の postgresql_cluster)→ source enroll → source check。
// 調査の後に state.json の source_seen[] に接続先の指紋があり、登録の後に source_clusters が書かれて同じ接続先の source_seen[] が消え、
// source check が actionable = [] を返す。根に置けない場所 (iv) に当たる根の定義では source enroll が DEF_INVALID(root_forbidden)で止まり、元サーバーに ACL が付かない。

import (
	"encoding/json"
	"strings"
)

const forbiddenRoot = "/srv/pgcopy"

func (w *s2) stateItems() map[string]json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(w.bak.root("cat "+stateDir+"/state.json")), &m); err != nil {
		w.t.Fatalf("state.json: %v", err)
	}
	return m
}

type seenRow struct {
	Host string `json:"host"`
	FP   string `json:"host_key_fp"`
}

func (w *s2) seenFPs() []string {
	var rows []seenRow
	_ = json.Unmarshal(w.stateItems()["source_seen"], &rows)
	var fps []string
	for _, r := range rows {
		fps = append(fps, r.FP)
	}
	return fps
}

// afterSurvey は source survey の結果と状態を確かめ、止まっているクラスタの印を持つ根の定義が source enroll で止まることを確かめる。
func (w *s2) afterSurvey(survey cliOut) {
	t := w.t
	fp := accEnv(t, "SITATE_ACCEPT_SOURCE_HOST_KEY_FP")
	found := false
	for _, f := range w.seenFPs() {
		found = found || f == fp
	}
	if !found {
		t.Errorf("source survey の後に state.json の source_seen[] に元サーバーの指紋が無い: %v(期待 %s)", w.seenFPs(), fp)
	}
	var sr struct {
		Clusters []struct {
			Port int `json:"port"`
		} `json:"clusters"`
	}
	_ = json.Unmarshal(survey.Env.Result, &sr)
	w.checkSurveyDirsSupported(survey)
	t.Logf("source survey: クラスタ %d 件、source_seen の指紋 %v", len(sr.Clusters), w.seenFPs())

	// 根に置けない場所 (iv): 止まっているクラスタの印(PG_VERSION と global/pg_control)を下に持つ根。
	w.src.root("mkdir -p " + forbiddenRoot + "/global && echo 16 > " + forbiddenRoot + "/PG_VERSION && : > " + forbiddenRoot + "/global/pg_control")
	bad := strings.Replace(w.definition(), `"files":"`+srcRoot+`"`, `"files":"`+forbiddenRoot+`"`, 1)
	w.g.call(bad+"\n", "def", "validate", "--file", "-").ok(t, "def validate(置けない根。(iv) は source enroll だけが見る)")
	o := w.g.call(w.sourceAdmin(), "source", "enroll", "--host", w.sourceHostArg(), "--admin-ref", "admin", "--secrets-stdin")
	if o.Env.OK || o.code() != "DEF_INVALID" || !strings.Contains(string(o.Env.Error.Detail), "root_forbidden") {
		t.Fatalf("置けない根の source enroll が DEF_INVALID(root_forbidden)で止まらない: ok=%v %s %s", o.Env.OK, o.code(), o.Env.Error)
	}
	t.Logf("置けない根の source enroll: %s(終了 %d)%.300s", o.code(), o.Code, o.Env.Error.Detail)
	if n := strings.TrimSpace(w.src.root("grep -rc 'sitate-recv' /root/.ssh/authorized_keys /home/*/.ssh/authorized_keys 2>/dev/null | awk -F: '{s+=$2} END{print s+0}'")); n != "0" {
		t.Errorf("止めたのに元サーバーに受け口の行がある: %s", n)
	}
	if acl := w.src.root("(getfacl -p " + forbiddenRoot + " 2>/dev/null || true) | grep -c 'sitate' || true"); strings.TrimSpace(acl) != "0" {
		t.Errorf("止めたのに元サーバーの根に ACL が付いた: %s", acl)
	}
	w.src.root("rm -rf " + forbiddenRoot)
	w.g.call(w.definition()+"\n", "def", "validate", "--file", "-").ok(t, "def validate(元の定義に戻す)")
}

// afterEnroll は source enroll の後の状態(source_clusters が書かれ、同じ接続先の source_seen[] が消える)と source check の actionable = [] を確かめる。
func (w *s2) afterEnroll() {
	t := w.t
	m := w.stateItems()
	if len(m["source_clusters"]) == 0 || string(m["source_clusters"]) == "null" {
		t.Errorf("source enroll の後に source_clusters が書かれていない")
	}
	fp := accEnv(t, "SITATE_ACCEPT_SOURCE_HOST_KEY_FP")
	for _, f := range w.seenFPs() {
		if f == fp {
			t.Errorf("source enroll の後も source_seen[] に同じ接続先の指紋が残っている")
		}
	}
	c := w.g.call("", "source", "check").ok(t, "source check")
	var r struct {
		Actionable []any `json:"actionable"`
	}
	_ = json.Unmarshal(c.Env.Result, &r)
	if r.Actionable == nil || len(r.Actionable) != 0 {
		t.Errorf("source check の actionable が [] でない: %s", c.Env.Result)
	}
	t.Logf("source_clusters=%.200s actionable=%v", m["source_clusters"], r.Actionable)
}

// checkSurveyDirsSupported は source survey の dirs[] のうち /home・/srv の下(label が home・srv)がすべて supported: true で、
// fs_type が spec 3.3 の candidate の種類(ext2・ext3・ext4・xfs・btrfs。overlay は数えない)であることを確かめる(D-0185)。
// 受入試験の環境の source は /home・/srv を名前つきの volume に置く。コンテナの根(overlay)に置くと全部 supported: false になり、A2 でファイルの対象が選ばれない。
func (w *s2) checkSurveyDirsSupported(survey cliOut) {
	t := w.t
	var sr struct {
		Dirs []struct {
			Label     string `json:"label"`
			FsType    string `json:"fs_type"`
			Supported bool   `json:"supported"`
		} `json:"dirs"`
	}
	if err := json.Unmarshal(survey.Env.Result, &sr); err != nil {
		t.Fatalf("source survey の result: %v", err)
	}
	candidate := map[string]bool{"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true}
	n, bad := 0, 0
	for _, d := range sr.Dirs {
		if d.Label != "home" && d.Label != "srv" {
			continue
		}
		n++
		if !d.Supported || !candidate[d.FsType] {
			bad++
			t.Errorf("dirs[] の %s の下が supported: true の candidate の種類でない: fs_type=%q supported=%v", d.Label, d.FsType, d.Supported)
		}
	}
	if n == 0 {
		t.Errorf("dirs[] に /home・/srv の下が無い")
	}
	t.Logf("source survey: /home・/srv の下 %d 件のうち supported: true でないもの %d 件", n, bad)
}
