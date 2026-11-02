package store

// 依存を埋めない骨格の構造体を呼んでも panic せず、原因の分かる誤り(INTERNAL)を返す(監督の決定。U1f-2 の結合の誤りを実行時に読める形にする)。

import (
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

var _ = contract.CodeInternal

func TestContractNilDeps(t *testing.T) {
	harness.NilDeps(t, "store", func() error { _, err := (&Retention{}).Verify(ctx, nil); return err })
	harness.NilDeps(t, "store", func() error { _, err := (&StoreProbe{}).PutProbe(ctx, "probe/x", nil); return err })
}
