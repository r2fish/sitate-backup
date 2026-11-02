package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/state"
)

type baseRestore = contract.Restore

type stubRestore struct {
	baseRestore
	called int
}

func (s *stubRestore) Restore(context.Context, contract.Call) (contract.Reply, error) {
	s.called++
	return contract.Reply{}, nil
}

// 規則 6: 復元先と宛先で動く restore は状態ディレクトリもロックも持たず、状態ディレクトリが無くても動く。
func TestRestoreOnDestTakesNoLockAndLeavesNoState(t *testing.T) {
	for _, extra := range [][]string{{"--snapshot-key-stdin"}} {
		dir := filepath.Join(t.TempDir(), "var-lib-sitate") // 存在しない状態ディレクトリ
		pr, pw := io.Pipe()
		go func() { _, _ = pw.Write([]byte("{}\n")) }()
		sr := &stubRestore{}
		d := &Deps{StateDir: dir, Stdin: pr, Limits: DefaultLimits(), State: &state.StateFiles{Dir: dir}, Lease: &Lease{Dir: dir}, Restore: sr}
		args := append([]string{"restore"}, extra...)
		args = append(args, "--to", "/restore", "--endpoint", "https://s3.example.invalid", "--bucket", "b")
		p, _, perr := parseArgs(args)
		if perr != nil {
			t.Fatalf("%v: %v", args, perr)
		}
		if takesRunLock(p) {
			t.Errorf("%v はロックを取らない", extra)
		}
		o := execute(context.Background(), d, args, secretNone)
		pw.Close()
		if o.err != nil || sr.called != 1 {
			t.Errorf("%v: err=%v called=%d", extra, o.err, sr.called)
		}
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("%v: 状態ディレクトリを作った", extra)
		}
		if o.runID != nil {
			t.Errorf("%v: run_id を作った", extra)
		}
	}
}

// バックアップサーバー側の restore(--to 宛先)は従来どおりロックを取る。
func TestRestoreOnBackupServerTakesLock(t *testing.T) {
	p, _, perr := parseArgs([]string{"restore", "--backup", "b", "--to", "restore-dest:x:/r"})
	if perr != nil {
		t.Fatal(perr)
	}
	if !takesRunLock(p) {
		t.Error("バックアップサーバー側の restore はロックを取る")
	}
}
