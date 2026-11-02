package state

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const baseDef = `
version: 1
source:
  host: app01.example.internal
  targets:
    - {id: t1, files: /srv/app/data}
    - {id: t2, files: /etc/app}
    - {id: t3, postgresql: appdb, port: 5433}
storage:
  - {endpoint: "https://s3.example-provider.com", bucket: example-sitate, key_ref: storage}
restore:
  dest: {at: "ssh://restore@203.0.113.10:22", key_ref: restore-init}
notify:
  to: [ops@example.jp]
`

func issuesOf(t *testing.T, s *StateFiles, def string) map[string]string {
	t.Helper()
	_, _, raw, err := s.ValidateDef(ctx, []byte(def), false)
	if err != nil {
		t.Fatal(err)
	}
	var is []issue
	_ = json.Unmarshal(raw, &is)
	m := map[string]string{}
	for _, i := range is {
		m[i.Pointer] = i.Reason
	}
	return m
}

func TestValidateDefAcceptsExample(t *testing.T) {
	s := newFiles(t)
	norm, hash, raw, err := s.ValidateDef(ctx, []byte(baseDef), false)
	if err != nil || string(raw) != "[]" || !strings.HasPrefix(hash, "sha256:") || len(hash) != 71 {
		t.Fatalf("%s %s %v", raw, hash, err)
	}
	// 同じ内容を JSON で書いても同じ正規化と def_hash(標準入力と --file で同じ判定になる土台)。
	var generic any
	_ = json.Unmarshal(norm, &generic)
	j, _ := json.Marshal(generic)
	_, hash2, _, _ := s.ValidateDef(ctx, j, false)
	if hash2 != hash {
		t.Errorf("JSON と YAML で def_hash が違う: %s %s", hash, hash2)
	}
	// 既定値を書いた定義は def_hash が違う(既定値を補わない)。
	_, hash3, _, _ := s.ValidateDef(ctx, []byte(baseDef+"schedule: \"02:30\"\n"), false)
	if hash3 == hash {
		t.Error("既定値を書いた定義の def_hash が同じ")
	}
}

func TestValidateDefIssues(t *testing.T) {
	s := newFiles(t)
	cases := []struct {
		name, def, ptr, reason string
	}{
		{"version 欠落", strings.Replace(baseDef, "version: 1\n", "", 1), "/version", rMissing},
		{"version 値", strings.Replace(baseDef, "version: 1", "version: 2", 1), "/version", rInvalid},
		{"未知のキー", baseDef + "bogus: 1\n", "/bogus", rUnknown},
		{"復元先なし", strings.Replace(baseDef, "restore:\n  dest: {at: \"ssh://restore@203.0.113.10:22\", key_ref: restore-init}\n", "", 1), "/restore", rMissing},
		{"遅延の下限", baseDef + "approval_delay_hours: 6\n", "/approval_delay_hours", rInvalid},
		{"遅延の単位", baseDef + "approval_delay_hours: 1.5\n", "/approval_delay_hours", rUnit},
		{"時刻の単位", baseDef + "schedule: \"2:30pm\"\n", "/schedule", rUnit},
		{"参照名", strings.Replace(baseDef, "key_ref: storage", "key_ref: Bad.Name", 1), "/storage/0/key_ref", rInvalid},
		{"根 /", strings.Replace(baseDef, "/srv/app/data", "/", 1), "/source/targets/0/files", rRootForb},
		{"根 /etc", strings.Replace(baseDef, "/srv/app/data", "/etc", 1), "/source/targets/0/files", rRootForb},
		{"根 /etc/ssh/x", strings.Replace(baseDef, "/srv/app/data", "/etc/ssh/x", 1), "/source/targets/0/files", rRootForb},
		{"根 /tmp", strings.Replace(baseDef, "/srv/app/data", "/tmp", 1), "/source/targets/0/files", rRootForb},
		{"根 .. を含む", strings.Replace(baseDef, "/srv/app/data", "/srv/../x", 1), "/source/targets/0/files", rInvalid},
		{"id の形", strings.Replace(baseDef, "id: t1", "id: t01", 1), "/source/targets/0/id", rInvalid},
		{"id 重複", strings.Replace(baseDef, "id: t2", "id: t1", 1), "/source/targets/1/id", rInvalid},
		{"種別なし", strings.Replace(baseDef, "{id: t1, files: /srv/app/data}", "{id: t1}", 1), "/source/targets/0", rMissing},
		{"port 外れ", strings.Replace(baseDef, "port: 5433", "port: 70000", 1), "/source/targets/2/port", rInvalid},
	}
	for _, c := range cases {
		if got := issuesOf(t, s, c.def)[c.ptr]; got != c.reason {
			t.Errorf("%s: %s の指摘 = %q、want %q", c.name, c.ptr, got, c.reason)
		}
	}
	two := strings.Replace(baseDef, "- {endpoint:", "- {endpoint: \"https://a.example.com\", bucket: b, key_ref: k}\n  - {endpoint:", 1)
	if issuesOf(t, s, two)["/storage"] != rInvalid {
		t.Error("保管先が 2 つの定義が通った")
	}
}

func TestValidateDefDeclinedAndRefs(t *testing.T) {
	s := newFiles(t)
	withDecl := strings.Replace(baseDef, "  targets:", "  declined:\n    - {id: d1, mount: /mnt/data}\n    - {id: d2, postgresql_cluster: 5432}\n  targets:", 1)
	if is := issuesOf(t, s, withDecl); len(is) != 0 {
		t.Fatalf("source.declined が通らない: %v", is)
	}
	same := strings.Replace(baseDef, "  targets:", "  declined:\n    - {id: d1, mount: /srv/app/data}\n  targets:", 1)
	if issuesOf(t, s, same)["/source/declined/0/mount"] != rInvalid {
		t.Error("files の根と同じ mount が通った")
	}
	bad := strings.Replace(baseDef, "  targets:", "  declined:\n    - {id: d1, mount: /m, postgresql_cluster: 5432}\n    - {id: x2, mount: /n}\n  targets:", 1)
	is := issuesOf(t, s, bad)
	if is["/source/declined/0"] != rInvalid || is["/source/declined/1/id"] != rInvalid {
		t.Errorf("declined の不正: %v", is)
	}
	// 参照名の解決: Refs が false を返せば unresolved_ref、skipRefs なら省く。
	s.Refs = func(_ context.Context, ref string) (bool, error) { return ref != "storage", nil }
	if issuesOf(t, s, baseDef)["/storage/0/key_ref"] != rUnresolve {
		t.Error("未解決の参照名が指摘されない")
	}
	if _, _, raw, _ := s.ValidateDef(ctx, []byte(baseDef), true); string(raw) != "[]" {
		t.Errorf("skipRefs で参照名を検査した: %s", raw)
	}
}

func TestValidateDefTargetKindChange(t *testing.T) {
	s := newFiles(t)
	norm, _, _, _ := s.ValidateDef(ctx, []byte(baseDef), true)
	if err := s.SwitchDef(ctx, contract.SwitchInput{Normalized: norm}); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(baseDef, "{id: t3, postgresql: appdb, port: 5433}", "{id: t3, files: /srv/x}", 1)
	if issuesOf(t, s, changed)["/source/targets/2"] != rInvalid {
		t.Error("承認済みの id の種別の変更が通った")
	}
}

func mkIndex(t *testing.T, s *StateFiles, ids ...string) {
	t.Helper()
	if err := os.MkdirAll(s.path("index", "files"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err := os.WriteFile(s.path("index", "files", id), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func hasIndex(s *StateFiles, id string) bool {
	_, err := os.Stat(s.path("index", "files", id))
	return err == nil
}

func normOf(t *testing.T, s *StateFiles, def string) []byte {
	t.Helper()
	n, _, raw, err := s.ValidateDef(ctx, []byte(def), true)
	if err != nil || n == nil {
		t.Fatalf("定義が通らない: %s %v", raw, err)
	}
	return n
}

func TestSwitchDefSealAndIndexRules(t *testing.T) {
	s := newFiles(t)
	if err := s.StageDef(ctx, normOf(t, s, baseDef)); err != nil {
		t.Fatal(err)
	}
	if err := s.SwitchDef(ctx, contract.SwitchInput{Normalized: normOf(t, s, baseDef)}); err != nil { // seal
		t.Fatal(err)
	}
	checkSealed(t, s)
	fp := func(v string) *string { return &v }
	// 根が変わっていない切り替え(ホスト鍵の一覧が空)は索引を消さない。
	mkIndex(t, s, "t1", "t2")
	same := strings.Replace(baseDef, "bucket: example-sitate", "bucket: other", 1)
	if err := s.SwitchDef(ctx, contract.SwitchInput{Normalized: normOf(t, s, same)}); err != nil {
		t.Fatal(err)
	}
	if !hasIndex(s, "t1") || !hasIndex(s, "t2") {
		t.Error("根も所在も変わらないのに索引が消えた")
	}
	// 初めてのホスト鍵の固定(role: source)。固定値が無いので索引は消え、host_keys[] に入る。
	hk := []contract.HostKeyUpdate{{Role: "source", Host: "app01.example.internal", HostKeyFP: fp("SHA256:aaa")}}
	if err := s.SwitchDef(ctx, contract.SwitchInput{Normalized: normOf(t, s, same), HostKeys: hk}); err != nil {
		t.Fatal(err)
	}
	mkIndex(t, s, "t1", "t2")
	// 同じ指紋なら消えない(冪等)。
	if err := s.SwitchDef(ctx, contract.SwitchInput{Normalized: normOf(t, s, same), HostKeys: hk}); err != nil {
		t.Fatal(err)
	}
	if !hasIndex(s, "t1") {
		t.Error("同じ指紋で索引が消えた")
	}
	// 定義が同じで指紋だけが変わる source-change: 索引が消え、host_keys[] が更新される。
	hk2 := []contract.HostKeyUpdate{{Role: "source", Host: "app01.example.internal", HostKeyFP: fp("SHA256:bbb")}}
	if err := s.SwitchDef(ctx, contract.SwitchInput{Normalized: normOf(t, s, same), HostKeys: hk2}); err != nil {
		t.Fatal(err)
	}
	if hasIndex(s, "t1") || hasIndex(s, "t2") {
		t.Error("指紋が変わったのに索引が残った")
	}
	st, _ := s.readState()
	if !strings.Contains(string(st["host_keys"]), "SHA256:bbb") || strings.Contains(string(st["host_keys"]), "SHA256:aaa") {
		t.Errorf("host_keys: %s", st["host_keys"])
	}
	// 根が変わった id だけ消え、restore_dest の更新は索引を消さない。
	mkIndex(t, s, "t1", "t2")
	moved := strings.Replace(same, "/srv/app/data", "/srv/app/data2", 1)
	rd := []contract.HostKeyUpdate{{Role: "restore_dest", Host: "203.0.113.10", HostKeyFP: fp("SHA256:ccc")}}
	if err := s.SwitchDef(ctx, contract.SwitchInput{Normalized: normOf(t, s, moved), HostKeys: rd}); err != nil {
		t.Fatal(err)
	}
	if hasIndex(s, "t1") || !hasIndex(s, "t2") {
		t.Errorf("根が変わった id だけが消えるはず: t1=%v t2=%v", hasIndex(s, "t1"), hasIndex(s, "t2"))
	}
	testEnrollIdempotent(t, s, moved)
}

// 登録の結果は同じ置き換えで書かれ、再実行しても同じ(冪等)。
func testEnrollIdempotent(t *testing.T, s *StateFiles, moved string) {
	en := &contract.EnrollResult{ReceiverHash: "sha256:" + strings.Repeat("a", 64), SourceClusters: json.RawMessage(`[{"port":5432}]`)}
	for i := 0; i < 2; i++ {
		if err := s.SwitchDef(ctx, contract.SwitchInput{Normalized: normOf(t, s, moved), Enroll: en}); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := s.readState()
	if string(st["source_clusters"]) != `[{"port":5432}]` || !strings.Contains(string(st["receiver_hash"]), "aaaa") {
		t.Errorf("登録の結果: %s %s", st["source_clusters"], st["receiver_hash"])
	}
}

func checkSealed(t *testing.T, s *StateFiles) {
	t.Helper()
	if _, _, found, _ := s.ReadCandidate(ctx); found {
		t.Error("seal の後に候補が残った")
	}
	st, _ := s.readState()
	if string(st["sealed"]) != "true" || string(st["approved_def"]) == "" {
		t.Errorf("seal の状態: %v", st)
	}
	if _, _, err := s.ReadDef(ctx); err != nil {
		t.Fatal(err)
	}
}
