package cli

// 骨格の継ぎ目の契約試験(作業指示書 5.1 の表で実物の担当が U1f-1 のもの): Lease(run.lock の flock と run/marker.json。spec 3.1.1 の規則 1・7)と
// Secrets(secrets/<参照名>.json。spec 4.5 の「参照名と秘密ファイル」)。実際の一時ディレクトリで確かめ、未実装の印を返す骨格と
// 何もしない実装(nil・空)は値の比較で落ちる。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

var bg = context.Background()

func TestContractLeaseAcquire(t *testing.T) {
	dir := t.TempDir()
	a := &Lease{Dir: dir}
	ok, err := a.Acquire(bg)
	harness.Implemented(t, err)
	if err != nil || !ok {
		t.Fatalf("空いているロックを取れない: %v %v", ok, err)
	}
	if st, err := os.Stat(filepath.Join(dir, "run.lock")); err != nil || st.IsDir() {
		t.Errorf("run.lock を作らない(O_CREAT): %v", err)
	}
	// 同じ run.lock への 2 つ目の取得は、持ち主が生きている間は取れない(待たず、誤りにもしない)。LOCKED への写しは呼び出し側。
	if ok, err := (&Lease{Dir: dir}).Acquire(bg); err != nil || ok {
		t.Errorf("別の取得者が取れた(非待機で false を期待): %v %v", ok, err)
	}
	// 持ち主の再取得は同じ記述子を保つ(誤りにならない)。
	if ok, err := a.Acquire(bg); err != nil || !ok {
		t.Errorf("持ち主の再取得: %v %v", ok, err)
	}
	// run.lock は消さない。
	if _, err := os.Stat(filepath.Join(dir, "run.lock")); err != nil {
		t.Error("run.lock が消えた")
	}
}

func TestContractLeaseWriteMarker(t *testing.T) {
	dir := t.TempDir()
	m1 := []byte(`{"run_id":"r-20261018T173000Z-3f9a","command":["backup"]}`)
	m2 := []byte(`{"run_id":"r-20261018T180000Z-4a0b","command":["verify"]}`)
	l := &Lease{Dir: dir}
	// 印はロックの持ち主だけが書く(規則 7)。
	_, err := l.WriteMarker(bg, m1)
	harness.Implemented(t, err)
	if err == nil {
		t.Fatal("ロックを持たない実行が印を書けた")
	}
	if _, err := os.Stat(filepath.Join(dir, "run", "marker.json")); err == nil {
		t.Error("ロックを持たない実行が印のファイルを作った")
	}
	if ok, err := l.Acquire(bg); err != nil || !ok {
		t.Fatalf("取得: %v %v", ok, err)
	}
	prev, err := l.WriteMarker(bg, m1)
	if err != nil || len(prev) != 0 {
		t.Fatalf("最初の印: 前の印は無い: %q %v", prev, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "run", "marker.json"))
	if err != nil || string(got) != string(m1) {
		t.Errorf("印のファイル: %q %v", got, err)
	}
	if st, _ := os.Stat(filepath.Join(dir, "run", "marker.json")); st == nil || st.Mode().Perm() != 0o600 {
		t.Errorf("印のファイルの権限は 0600: %v", st)
	}
	if st, _ := os.Stat(filepath.Join(dir, "run")); st == nil || st.Mode().Perm() != 0o700 {
		t.Errorf("run/ の権限は 0700: %v", st)
	}
	// 次の印は前の印を返して置き換える(前の印の処理 → 自分の印。規則 7)。
	prev, err = l.WriteMarker(bg, m2)
	if err != nil || string(prev) != string(m1) {
		t.Errorf("前の印: %q %v", prev, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "run", "marker.json")); string(got) != string(m2) {
		t.Errorf("印が置き換わっていない: %q", got)
	}
	if ents, _ := os.ReadDir(filepath.Join(dir, "run")); len(ents) != 1 {
		t.Errorf("run/ に一時ファイルが残った: %d 件", len(ents))
	}
}

func secretsDir(t *testing.T) (*Secrets, string) {
	t.Helper()
	d := t.TempDir()
	return &Secrets{Dir: d}, d
}

func TestContractSecretsPut(t *testing.T) {
	s, d := secretsDir(t)
	v := json.RawMessage(`{"kind":"s3-key","key_id":"K1","secret":"zzz"}`)
	err := s.Put(bg, "b2-write", v)
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(d, "secrets", "b2-write.json")
	b, err := os.ReadFile(f)
	if err != nil || !json.Valid(b) || !strings.Contains(string(b), `"zzz"`) {
		t.Fatalf("秘密ファイル: %q %v", b, err)
	}
	if st, _ := os.Stat(f); st == nil || st.Mode().Perm() != 0o600 {
		t.Errorf("ファイルの権限は 0600: %v", st)
	}
	if st, _ := os.Stat(filepath.Join(d, "secrets")); st == nil || st.Mode().Perm() != 0o700 {
		t.Errorf("secrets/ の権限は 0700: %v", st)
	}
	// 上書きは新しい値にし、一時ファイルを残さない。核が生成する予約名(sitate-pull)も置ける。
	if err := s.Put(bg, "b2-write", json.RawMessage(`{"kind":"s3-key","key_id":"K2","secret":"yyy"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(bg, "sitate-pull", json.RawMessage(`{"kind":"ssh-key"}`)); err != nil {
		t.Errorf("核が生成する参照名: %v", err)
	}
	if b, _ := os.ReadFile(f); !strings.Contains(string(b), `"yyy"`) || strings.Contains(string(b), `"zzz"`) {
		t.Errorf("上書きされていない: %q", b)
	}
	if ents, _ := os.ReadDir(filepath.Join(d, "secrets")); len(ents) != 2 {
		t.Errorf("secrets/ のファイルは 2 件(一時ファイルなし): %d", len(ents))
	}
}

func TestContractSecretsGet(t *testing.T) {
	s, _ := secretsDir(t)
	_, err := s.Get(bg, "nope")
	ce := harness.ExpectCode(t, err, contract.CodeSecretMissing)
	var d struct {
		Ref string `json:"ref"`
	}
	if json.Unmarshal(ce.Detail, &d); d.Ref != "nope" {
		t.Errorf("detail.ref = %s", ce.Detail)
	}
	v := json.RawMessage(`{"kind":"smtp","password":"pw-value"}`)
	if err := s.Put(bg, "smtp-main", v); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(bg, "smtp-main")
	var a, b map[string]any
	if err != nil || json.Unmarshal(got, &a) != nil || json.Unmarshal(v, &b) != nil || a["password"] != b["password"] || a["kind"] != "smtp" {
		t.Errorf("往復: %s %v", got, err)
	}
	// 参照名の形(spec 4.5): 正規表現 ^[a-z0-9-]{1,27}(-next)?$。外れるものは置き場の外を指せない。
	for _, bad := range []string{"../x", "a/b", "A", "x.y", "", strings.Repeat("a", 28), strings.Repeat("a", 33) + "-next"} {
		_, err := s.Get(bg, bad)
		ce := harness.ExpectCode(t, err, contract.CodeArgInvalid)
		if !strings.Contains(string(ce.Detail), "format") {
			t.Errorf("%q: detail.reason が format でない: %s", bad, ce.Detail)
		}
		if err := s.Put(bg, bad, v); err == nil {
			t.Errorf("%q を置けた", bad)
		}
	}
	// -next は 32 文字まで。
	if err := s.Put(bg, strings.Repeat("a", 27)+"-next", v); err != nil {
		t.Errorf("27 文字 + -next: %v", err)
	}
}

func TestContractSecretsDelete(t *testing.T) {
	s, d := secretsDir(t)
	if err := s.Put(bg, "tmp-key", json.RawMessage(`{"kind":"x"}`)); err != nil {
		harness.Implemented(t, err)
		t.Fatal(err)
	}
	err := s.Delete(bg, "tmp-key")
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "secrets", "tmp-key.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ファイルが残った: %v", err)
	}
	_, err = s.Get(bg, "tmp-key")
	harness.ExpectCode(t, err, contract.CodeSecretMissing)
	// 秘密を消すのは冪等(失効後の再実行で落ちない)。
	if err := s.Delete(bg, "tmp-key"); err != nil {
		t.Errorf("無い参照名の削除: %v", err)
	}
	if err := s.Delete(bg, "../x"); err == nil {
		t.Error("不正な参照名の削除が成功した")
	}
}
