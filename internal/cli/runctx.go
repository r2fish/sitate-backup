package cli

import (
	"context"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// runContext は AdapterRunner に渡す実行の文脈。子は必ず自分のプロセスグループで起動し、PR_SET_PDEATHSIG を掛ける(spec 3.1.1 の規則 13)。
// Deadline は実行の文脈の締め切り(無ければ零値)。
func runContext(ctx context.Context, runID string) contract.RunContext {
	rc := contract.RunContext{RunID: runID, ProcessGroup: true, PdeathSig: true}
	if dl, ok := ctx.Deadline(); ok {
		rc.Deadline = dl
	}
	return rc
}
