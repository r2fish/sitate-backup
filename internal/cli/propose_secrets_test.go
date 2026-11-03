package cli

// まとめレビュー F6: propose notify-add・notify-change の --secrets-stdin は、--notify の to・smtp_ref・heartbeat_ref に出る新しい参照名だけを受け、
// `sitate-` の接頭辞(製品の予約名)を拒む。拒んだときは秘密ファイルを増やさない。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func TestProposeSecretsRefMustBeInNotify(t *testing.T) {
	cases := []struct {
		name, notify, ref string
		ok                bool
	}{
		{"to に出る参照名", `{"to":["hook-2"]}`, "hook-2", true},
		{"smtp_ref に出る参照名", `{"to":["a@example.test"],"smtp_ref":"mail-2"}`, "mail-2", true},
		{"heartbeat_ref に出る参照名", `{"to":["a@example.test"],"heartbeat_ref":"hb-2"}`, "hb-2", true},
		{"--notify に出ない参照名", `{"to":["hook-2"]}`, "unused-name", false},
		{"sitate- の接頭辞", `{"to":["sitate-restore"]}`, "sitate-restore", false},
		{"--notify が無い", ``, "hook-2", false},
	}
	for _, c := range cases {
		d, _ := notifyDeps(t, "")
		d.Approval = &proposeStub{}
		args := map[string][]string{}
		if c.notify != "" {
			args["notify"] = []string{c.notify}
		}
		call := contract.Call{Words: []string{"propose"}, Positional: []string{"notify-add"}, Args: args, Secrets: contract.SecretInput{c.ref: []byte(`"https://hooks.example.test/2"`)}}
		_, err := proposeEntry(d, bg, call)
		_, statErr := os.Stat(filepath.Join(d.StateDir, "secrets", c.ref+".json"))
		if c.ok != (err == nil) || c.ok != (statErr == nil) {
			t.Errorf("%s: err=%v 秘密ファイル=%v", c.name, err, statErr == nil)
		}
	}
}
