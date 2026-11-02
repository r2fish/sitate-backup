package cli

// 骨格(作業指示書 5.1): cli の持つ継ぎ目(Lease、Secrets、Clock、RetentionParams)は NOT_IMPLEMENTED を返し、
// Main は NOT_IMPLEMENTED の封筒(終了 9)を出す。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Main は sitate の入口(cmd/sitate が呼ぶ)。骨格は NOT_IMPLEMENTED の封筒を標準出力に出して終了 9 を返す。
func Main(args []string, stdout, _ io.Writer) int {
	env := map[string]any{
		"schema": "sitate.out/v1", "command": firstWords(args), "ok": false, "dry_run": false, "run_id": nil,
		"result": nil, "warnings": []any{},
		"error": map[string]any{
			"code": string(contract.CodeNotImplemented), "class": contract.ExitClass(9), "retryable": false,
			"detail": map[string]any{"feature": "cli.main"}, "next": []any{},
		},
	}
	b, _ := json.Marshal(env)
	fmt.Fprintln(stdout, string(b))
	return 9
}

func firstWords(args []string) []string {
	if len(args) == 0 {
		return []string{"version"}
	}
	return []string{args[0]}
}

// Lease は contract.Lease の骨格。Dir は状態ディレクトリ(run.lock と run/marker.json の置き場。spec 4.5)。
type Lease struct{ Dir string }

var _ contract.Lease = (*Lease)(nil)

// Acquire は contract.Lease を満たす。
func (*Lease) Acquire(context.Context) (bool, error) {
	return false, contract.ErrNotImplemented("cli.Lease.Acquire")
}

// WriteMarker は contract.Lease を満たす。
func (*Lease) WriteMarker(context.Context, []byte) ([]byte, error) {
	return nil, contract.ErrNotImplemented("cli.Lease.WriteMarker")
}

// Deadline は contract.Lease を満たす。
func (*Lease) Deadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(ctx)
}

// Secrets は contract.Secrets の骨格。Dir は状態ディレクトリ(secrets/<参照名>.json の置き場。spec 4.5)。
type Secrets struct{ Dir string }

var _ contract.Secrets = (*Secrets)(nil)

// Get は contract.Secrets を満たす。
func (*Secrets) Get(context.Context, string) (json.RawMessage, error) {
	return nil, contract.ErrNotImplemented("cli.Secrets.Get")
}

// Put は contract.Secrets を満たす。
func (*Secrets) Put(context.Context, string, json.RawMessage) error {
	return contract.ErrNotImplemented("cli.Secrets.Put")
}

// Delete は contract.Secrets を満たす。
func (*Secrets) Delete(context.Context, string) error {
	return contract.ErrNotImplemented("cli.Secrets.Delete")
}

// SystemClock は contract.Clock の製品ビルドの骨格(実時計)。試験ビルドの実装(オフセット)は U1f-1。
type SystemClock struct{}

var _ contract.Clock = SystemClock{}

// Now は contract.Clock を満たす。
func (SystemClock) Now(context.Context) (time.Time, error) {
	return time.Now().UTC().Truncate(time.Second), nil
}

// Retention は contract.RetentionParams の製品ビルドの実装(固定の定数)。
type Retention = contract.FixedRetention
