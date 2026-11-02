package adapter

// 骨格(作業指示書 5.1): 契約のインターフェースを満たし、NOT_IMPLEMENTED を返す。

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Runner は contract.AdapterRunner の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// 依存は公開フィールドで受ける。Root はアダプタの置き場(<Root>/<kind>/<name>/{adapter.json,adapter.json.sig,<exe>})、
// Version・Arch は trusted comment の期待値(spec 3.2.1)に使う動いている核の版とホストの <os>-<arch>。
type Runner struct {
	Root     string
	Version  string
	Arch     string
	Verifier contract.Verifier
}

var _ contract.AdapterRunner = (*Runner)(nil)

// Declaration は contract.AdapterRunner を満たす。
func (*Runner) Declaration(ctx context.Context, kind, name string) (contract.Declaration, error) {
	var r0 contract.Declaration
	return r0, contract.ErrNotImplemented("adapter.Runner.Declaration")
}

// Run は contract.AdapterRunner を満たす。
func (*Runner) Run(ctx context.Context, rc contract.RunContext, kind, name string, req json.RawMessage, onFrame func(contract.Frame) error) (json.RawMessage, error) {
	var r0 json.RawMessage
	return r0, contract.ErrNotImplemented("adapter.Runner.Run")
}
