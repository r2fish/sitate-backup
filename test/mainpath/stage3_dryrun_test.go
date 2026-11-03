//go:build mainpath

package mainpath

// 第 3 段: 導入の手順 2・3(spec 3.8)の dry-run を、定義を組み立てる前に通す(D-0165)。候補の無い状態で
// `init-storage --dry-run` と `restore-dest enroll --dry-run` が /result/plan/errors = [] を返し、
// 保管先にも復元先にも状態ディレクトリにも何も作らず、固定値を書かない。

import (
	"encoding/json"
	"strings"
)

func (w *s2) stateSnapshot() string {
	return w.bak.root(`cd ` + stateDir + ` && (ls -A secrets def 2>/dev/null; grep -c . state.json 2>/dev/null; grep -o '"host_keys":[^]]*]' state.json 2>/dev/null; grep -o '"restore_dest_enrollment":[^}]*}' state.json 2>/dev/null; true) | sha256sum`)
}

func (w *s2) planErrors(label string, o cliOut) {
	t := w.t
	var r struct {
		Plan struct {
			Errors []json.RawMessage `json:"errors"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(o.Env.Result, &r); err != nil || r.Plan.Errors == nil {
		t.Fatalf("%s: /result/plan/errors が無い: %.400s", label, o.Env.Result)
	}
	if len(r.Plan.Errors) != 0 {
		t.Fatalf("%s: /result/plan/errors = %s", label, r.Plan.Errors)
	}
}

func (w *s2) dryRunsBeforeDefinition() {
	t := w.t
	w.bak.provisionDouble(t, provisionAdapter, w.s3)
	w.bak.provisionDouble(t, "b2", w.s3)
	before := w.stateSnapshot()
	is := w.g.call(storageAdmin(), "init-storage", "--dry-run", "--endpoint", w.s3.endpoint, "--bucket", w.s3.bucket, "--prefix", "sitate/",
		"--adapter", provisionAdapter, "--admin-ref", "admin", "--secrets-stdin").ok(t, "init-storage --dry-run")
	w.planErrors("init-storage --dry-run", is)
	t.Logf("init-storage --dry-run: %.500s", is.Env.Result)
	de := w.g.call(w.sourceAdmin(), "restore-dest", "enroll", "--dry-run", "--dest", w.destAt(), "--adapter", "ssh-host", "--admin-ref", "admin", "--secrets-stdin").ok(t, "restore-dest enroll --dry-run")
	w.planErrors("restore-dest enroll --dry-run", de)
	t.Logf("restore-dest enroll --dry-run: %.500s", de.Env.Result)
	if got, _ := de.resultMap(t)["host_key_fp"].(string); got != accEnv(t, "SITATE_ACCEPT_DEST_HOST_KEY_FP") {
		t.Errorf("dry-run の host_key_fp が起動時の記録と違う: %s", got)
	}
	if after := w.stateSnapshot(); after != before {
		t.Errorf("dry-run が状態ディレクトリ(secrets・def・host_keys・登録の記録)を変えた")
	}
	if r := w.dst.rootTry(`id sitate-restore`); r.Code == 0 {
		t.Errorf("dry-run が復元先に専用ユーザーを作った")
	}
	if out := strings.TrimSpace(w.bak.root(`ls ` + stateDir + `/secrets 2>/dev/null | wc -l`)); out != "0" {
		t.Errorf("dry-run が秘密ファイルを置いた: %s 件", out)
	}
}
