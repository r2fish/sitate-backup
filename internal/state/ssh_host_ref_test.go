package state

// D-0162(spec 3.1.2 の def validate): ssh-host の restore.dest.key_ref は秘密ファイルで解決しない(初期資格情報はバックアップサーバーのディスクに置かない)。
// host_keys[] に role: restore_dest で restore.dest.at のホスト名の要素があり、restore_dest_enrollment が null でないとき(手順 9 の登録の記録)を解決とする。
// 別のホストの記録では解決にしない。seal の後は検査しない。

import (
	"context"
	"testing"
)

func setItems(t *testing.T, s *StateFiles, kv map[string]string) {
	t.Helper()
	h, err := s.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()
	for k, v := range kv {
		if err := s.Update(context.Background(), h, k, []byte(v)); err != nil {
			t.Fatal(err)
		}
	}
}

func destRefIssue(t *testing.T, s *StateFiles) string {
	t.Helper()
	s.Refs = func(_ context.Context, ref string) (bool, error) { return ref != "restore-init", nil } // storage は解決、restore-init の秘密ファイルは無い
	return issuesOf(t, s, baseDef)["/restore/dest/key_ref"]
}

func TestSshHostDestRefResolvesByEnrollmentRecord(t *testing.T) {
	s := newFiles(t)
	if got := destRefIssue(t, s); got != rUnresolve {
		t.Fatalf("登録の記録が無いのに解決した: %q", got)
	}
	hk := func(host string) string {
		return `[{"role":"restore_dest","host":"` + host + `","host_key_fp":"SHA256:x","session_id":null}]`
	}
	setItems(t, s, map[string]string{"host_keys": hk("198.51.100.9"), "restore_dest_enrollment": `{"egress":["192.0.2.1"]}`})
	if got := destRefIssue(t, s); got != rUnresolve {
		t.Fatalf("別のホストの記録で解決した: %q", got)
	}
	setItems(t, s, map[string]string{"host_keys": hk("203.0.113.10"), "restore_dest_enrollment": `null`})
	if got := destRefIssue(t, s); got != rUnresolve {
		t.Fatalf("restore_dest_enrollment が null なのに解決した: %q", got)
	}
	setItems(t, s, map[string]string{"restore_dest_enrollment": `{"egress":["192.0.2.1"]}`})
	if got := destRefIssue(t, s); got != "" {
		t.Fatalf("登録の記録があるのに解決しない: %q", got)
	}
}

func TestSshHostDestRefIsNotCheckedAfterSeal(t *testing.T) {
	s := newFiles(t)
	setItems(t, s, map[string]string{"sealed": "true"})
	if got := destRefIssue(t, s); got != "" {
		t.Fatalf("seal の後に検査した: %q", got)
	}
}

// ssh-host 以外のアダプタは従来どおり秘密ファイルで解決する。
func TestOtherDestAdapterStillResolvesBySecretFile(t *testing.T) {
	s := newFiles(t)
	s.Refs = func(_ context.Context, ref string) (bool, error) { return ref != "restore-init", nil }
	def := baseDef + "" // adapter を足した定義
	def = def[:len(def)-len("notify:\n  to: [ops@example.jp]\n")]
	def = replaceOnce(def, "restore:\n", "restore:\n  adapter: ec2\n") + "notify:\n  to: [ops@example.jp]\n"
	if got := issuesOf(t, s, def)["/restore/dest/key_ref"]; got != rUnresolve {
		t.Fatalf("ssh-host 以外は秘密ファイルで解決するはず: %q", got)
	}
}

func replaceOnce(s, old, new string) string {
	for i := 0; i+len(old) <= len(s); i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}
