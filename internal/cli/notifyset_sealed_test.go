package cli

// seal の後の notify set は提案を要する(spec 3.1.2 の notify set の行): 宛先の追加は notify-add、削除・差し替えと資格情報の差し替えは notify-change。
// seal の前は従来どおり承認なし。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sealState(t *testing.T, d *Deps, approvedNotifyTo string) {
	t.Helper()
	h, err := d.State.Lock(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := d.State.Update(t.Context(), h, "sealed", []byte("true")); err != nil {
		t.Fatal(err)
	}
	h.Release()
	if approvedNotifyTo != "" {
		def := `{"version":1,"notify":{"to":[` + approvedNotifyTo + `]}}`
		b, _ := json.Marshal(map[string]any{"def_hash": "sha256:" + strings.Repeat("a", 64), "normalized": json.RawMessage(def)})
		if err := os.MkdirAll(filepath.Join(d.StateDir, "def"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d.StateDir, "def", "approved.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNotifySetAfterSealNeedsProposal(t *testing.T) {
	for _, c := range []struct{ name, approvedTo, kind string }{
		{"追加(定義に無い宛先)", `"other-hook"`, "notify-add"},
		{"差し替え(定義にある宛先)", `"hook-a"`, "notify-change"},
		{"定義を読めない", "", "notify-change"},
	} {
		d, _ := notifyDeps(t, `{"hook-a":"https://hooks.example.test/x"}`+"\n")
		sealState(t, d, c.approvedTo)
		o, code := runCLI(d, newBuf(), "notify", "set", "--to", "hook-a", "--secret-ref", "hook-a", "--secrets-stdin")
		e, _ := o["error"].(map[string]any)
		if code != 4 || e["code"] != "APPROVAL_REQUIRED" || !strings.Contains(string(mustJSON(e["detail"])), `"kind":"`+c.kind+`"`) {
			t.Errorf("%s: %v (code %d)", c.name, o, code)
		}
		if _, err := os.Stat(filepath.Join(d.StateDir, "secrets", "hook-a.json")); err == nil {
			t.Errorf("%s: 拒否のはずが秘密ファイルを置いた", c.name)
		}
	}
}
