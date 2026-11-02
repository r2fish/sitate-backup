package restore

// restore の (a) その場のディレクトリの入口(spec 3.1.2.4、3.5 の表の 8、作業指示書 5.6 の完了条件 7)。
// 世代を読んで書き出す判定(materialize)は U1e 自身の試験と主経路の通しで確かめる。ここは入口の前提だけを見る。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func inPlaceCall(t *testing.T, to string) contract.Call {
	t.Helper()
	var line contract.DestKeyLine
	line.SnapshotKey = "c25hcC1rZXk="
	line.ReadKey.KeyID, line.ReadKey.Secret = "kR", "sR"
	b, err := json.Marshal(line)
	if err != nil {
		t.Fatal(err)
	}
	return contract.Call{
		Words: []string{"restore"},
		Args: map[string][]string{contract.ArgBackup: {refA}, contract.ArgTo: {to}, contract.ArgEndpoint: {"https://s3.example.invalid"},
			contract.ArgBucket: {"b"}, contract.ArgPrefix: {"sitate/"}},
		RunID:         rtRun,
		SnapshotKeyIn: contract.NewSecret(b),
	}
}

// 引数の保管先を標準入力の読み取り鍵で開く。状態を持たない復元先でも入口に届き、鍵を結果と誤りに出さない。
func TestContractRestoreRestore(t *testing.T) {
	store, _, _ := fake.NewTestStore(fake.RoleRead)
	var gotAddr contract.StoreAddr
	var gotKeyID, gotSecret string
	r := &Restore{Codec: fake.Codec{Size: 4}, KeyS: fake.KeyService{}, Crypto: fake.Crypto{}, Recorder: &fake.Recorder{}, Clock: &fake.Clock{T: fake.StartTime},
		OpenStore: func(_ context.Context, addr contract.StoreAddr, keyID string, s *contract.Secret) (contract.ObjectStore, error) {
			gotAddr, gotKeyID, gotSecret = addr, keyID, string(s.Reveal())
			return store, nil
		}}
	rep, err := r.Restore(ctx, inPlaceCall(t, filepath.Join(t.TempDir(), "to")))
	harness.Implemented(t, err)
	if gotAddr != (contract.StoreAddr{Endpoint: "https://s3.example.invalid", Bucket: "b", Prefix: "sitate/"}) || gotKeyID != "kR" || gotSecret != "sR" {
		t.Errorf("引数の保管先を標準入力の読み取り鍵で開いていない: %+v %q", gotAddr, gotKeyID)
	}
	text := string(rep.Result)
	if err != nil {
		text += err.Error()
	}
	if strings.Contains(text, "sR") || strings.Contains(text, "c25hcC1rZXk=") {
		t.Errorf("鍵が結果か誤りに出た: %s", text)
	}
}

// 書き込み先は空のディレクトリに限る(spec 3.1.2 の表)。鍵を使う前に止め、既存のファイルを変えない。
func TestContractRestoreRestoreToNotEmpty(t *testing.T) {
	to := t.TempDir()
	keep := filepath.Join(to, "keep")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	opened := false
	r := &Restore{Codec: fake.Codec{Size: 4}, KeyS: fake.KeyService{}, Crypto: fake.Crypto{}, Recorder: &fake.Recorder{}, Clock: &fake.Clock{T: fake.StartTime},
		OpenStore: func(context.Context, contract.StoreAddr, string, *contract.Secret) (contract.ObjectStore, error) {
			opened = true
			return nil, os.ErrInvalid
		}}
	_, err := r.Restore(ctx, inPlaceCall(t, to))
	harness.Implemented(t, err)
	if err == nil {
		t.Fatal("空でないディレクトリへの復元が成功した")
	}
	if opened {
		t.Error("空でないディレクトリなのに保管先を開いた")
	}
	if b, rerr := os.ReadFile(keep); rerr != nil || string(b) != "keep" {
		t.Errorf("既存のファイルが変わった: %q %v", b, rerr)
	}
}
