package cli

// D-0160(spec 3.1.2 の def validate): seal の前は storage[0].key_ref(手順 5 の init-storage が置く)と restore.dest.key_ref(手順 9 の restore-dest enroll が置く)の
// 未解決だけを許し、pending_refs[] に返して候補を保存する。ほかの参照名の未解決は DEF_INVALID(unresolved_ref)のまま。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/state"
)

func pendingDeps(t *testing.T, stdin string, existing ...string) (*Deps, func(args ...string) (map[string]any, int)) {
	d, out := defDeps(t, stdin)
	have := map[string]bool{}
	for _, r := range existing {
		have[r] = true
	}
	d.State.(*state.StateFiles).Refs = func(_ context.Context, r string) (bool, error) { return have[r], nil }
	return d, func(args ...string) (map[string]any, int) { return runCLI(d, out, args...) }
}

func TestDefValidateBeforeSealReturnsPendingRefsAndStagesCandidate(t *testing.T) {
	d, run := pendingDeps(t, defYAML)
	env, code := run("def", "validate", "--file", "-")
	if code != 0 || env["ok"] != true {
		t.Fatalf("seal の前の storage・restore.dest の key_ref の未解決で止まった: %v %d", env, code)
	}
	got := env["result"].(map[string]any)["pending_refs"]
	if b, _ := json.Marshal(got); string(b) != `["/restore/dest/key_ref","/storage/0/key_ref"]` {
		t.Fatalf("pending_refs = %s", b)
	}
	if _, _, found, _ := d.State.ReadCandidate(context.Background()); !found {
		t.Fatal("候補が保存されていない")
	}
}

func TestDefValidateBeforeSealResolvedRefsGiveEmptyPending(t *testing.T) {
	d, run := pendingDeps(t, defYAML, "storage")
	h, _ := d.State.Lock(context.Background()) // ssh-host の restore.dest.key_ref は登録の記録で解決する(D-0162)
	_ = d.State.Update(context.Background(), h, "host_keys", json.RawMessage(`[{"role":"restore_dest","host":"203.0.113.10","host_key_fp":"SHA256:x","session_id":null}]`))
	_ = d.State.Update(context.Background(), h, "restore_dest_enrollment", json.RawMessage(`{"egress":["192.0.2.1"]}`))
	h.Release()
	env, code := run("def", "validate", "--file", "-")
	if code != 0 {
		t.Fatalf("%v", env)
	}
	if b, _ := json.Marshal(env["result"].(map[string]any)["pending_refs"]); string(b) != `[]` {
		t.Fatalf("pending_refs = %s", b)
	}
}

// ほかの参照名(通知の資格情報など)の未解決は seal の前でも DEF_INVALID。候補は保存しない。
func TestDefValidateBeforeSealOtherUnresolvedRefIsInvalid(t *testing.T) {
	d, run := pendingDeps(t, strings.Replace(defYAML, "notify: {to: [ops@example.jp]}", "notify: {to: [ops@example.jp], smtp_ref: smtp-main}", 1))
	env, code := run("def", "validate", "--file", "-")
	e, _ := env["error"].(map[string]any)
	if code != 2 || e["code"] != "DEF_INVALID" || !strings.Contains(string(mustJSON(e["detail"])), "unresolved_ref") {
		t.Fatalf("DEF_INVALID(unresolved_ref) のはず: %v %d", env, code)
	}
	if _, _, found, _ := d.State.ReadCandidate(context.Background()); found {
		t.Error("不正な定義が候補に入った")
	}
}
