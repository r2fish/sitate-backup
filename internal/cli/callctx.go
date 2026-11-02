package cli

// 呼び出し(contract.Call)を文脈に載せる。保管先の所在(引数の --endpoint/--bucket/--prefix か定義の storage[0])と
// apply の時計は、部品が Call を受けない口(ObjectStore・Clock)から引くため、この文脈の値を使う。

import (
	"context"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type callKey struct{}

// withCall は呼び出しを文脈に載せる。
func withCall(ctx context.Context, c contract.Call) context.Context {
	return context.WithValue(ctx, callKey{}, c)
}

// callOf は文脈の呼び出し。載っていなければ ok=false。
func callOf(ctx context.Context) (contract.Call, bool) {
	c, ok := ctx.Value(callKey{}).(contract.Call)
	return c, ok
}

// approvalClock は approval に渡す時計(作業指示書 5.17 の完了条件 24): apply は保管先の Date、
// propose・approve・cancel などは保管先へ出ない手元の時計。
type approvalClock struct{ d *Deps }

var _ contract.Clock = approvalClock{}

// Now は contract.Clock を満たす。
func (c approvalClock) Now(ctx context.Context) (time.Time, error) {
	if call, ok := callOf(ctx); ok && len(call.Words) > 0 && call.Words[0] == "apply" {
		return NewApplyClock(c.d).Now(ctx)
	}
	return c.d.Clock.Now(ctx)
}
