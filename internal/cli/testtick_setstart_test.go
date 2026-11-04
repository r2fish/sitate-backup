//go:build sitate_testbuild

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 完了条件 10: --set-start は共有の値を置くだけ(タイマーの単位を起動しない)。置いた値は、別に起動した核のプロセスが
// ファイルから読み直して同じ起点と経過(実時間の進み)で読める。壊れた値は --days を ARG_INVALID(format)で止める。
func TestTestTickSetStartPlacesSharedValueOnlyAndOtherReaderSeesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sim")
	t.Setenv(contract.EnvSimTimeFile, path)
	// Deps は空: 単位の起動(Timers・Object など)に触れれば nil で落ちる。
	rep, err := testTick(context.Background(), &Deps{}, []string{contract.TestTickFlagSetStart, "2027-03-01T00:00:00Z"})
	if err != nil {
		t.Fatalf("--set-start: %v", err)
	}
	if string(rep.Result) != `{"days":0,"now":"2027-03-01T00:00:00Z"}` {
		t.Errorf("結果: %s", rep.Result)
	}
	other, err := contract.ReadSimTime(path) // 別プロセスの代わり: 呼び出し側の状態を共有せず、ファイルだけから読む
	if err != nil {
		t.Fatal(err)
	}
	real0, _ := time.Parse(time.RFC3339, other.RealAt)
	for _, d := range []time.Duration{0, 90 * time.Minute} {
		got, err := other.Now(real0.Add(d))
		if want := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC).Add(d); err != nil || !got.Equal(want) {
			t.Errorf("実時間 +%v の模擬時間: %v (want %v) %v", d, got, want, err)
		}
	}
	// 壊れた値のとき --days は終了 2 の ARG_INVALID(format)。
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = testTick(context.Background(), &Deps{}, []string{contract.TestTickFlagDays, "1"})
	if ce, ok := err.(*contract.Error); !ok || ce.Code != contract.CodeArgInvalid || ce.Detail == nil {
		t.Fatalf("壊れた値: %v", err)
	}
}
