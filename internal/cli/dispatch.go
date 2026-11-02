package cli

import (
	"context"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// entry は部品の入口 1 つ。部品が nil のときは NOT_IMPLEMENTED(feature は cli.<サブコマンド>)。
type entry func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error)

func notImpl(sub string) (contract.Reply, error) {
	return contract.Reply{}, contract.ErrNotImplemented("cli." + sub)
}

// route はサブコマンドの語から入口を返す。cli 自身が持つ入口(version、def、notify、status、report、estimate など)は routeOwn。
func route(words []string) entry {
	key := strings.Join(words, " ")
	if e, ok := routeOwn[key]; ok {
		return e
	}
	if e, ok := routeParts[key]; ok {
		return e
	}
	return func(*Deps, context.Context, contract.Call) (contract.Reply, error) {
		return notImpl(strings.Join(words, "-"))
	}
}

// routeParts は他の WP の部品へ渡す入口。部品が nil なら NOT_IMPLEMENTED。
var routeParts = map[string]entry{
	"init": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Keys == nil {
			return notImpl("init")
		}
		return d.Keys.Init(ctx, c)
	},
	"init-storage": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Keys == nil {
			return notImpl("init-storage")
		}
		return d.Keys.InitStorage(ctx, c)
	},
	"phrase verify": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Keys == nil {
			return notImpl("phrase-verify")
		}
		return d.Keys.PhraseVerify(ctx, c)
	},
	"bucket check": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Keys == nil {
			return notImpl("bucket-check")
		}
		return d.Keys.BucketCheck(ctx, c)
	},
	"kit": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Keys == nil {
			return notImpl("kit")
		}
		return d.Keys.Kit(ctx, c)
	},
	"kit confirm": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Keys == nil {
			return notImpl("kit-confirm")
		}
		return d.Keys.KitConfirm(ctx, c)
	},
	"doctor": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Keys == nil {
			return notImpl("doctor")
		}
		return d.Keys.Doctor(ctx, c)
	},
	"list": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Restore == nil {
			return notImpl("list")
		}
		return d.Restore.List(ctx, c)
	},
	"restore": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Restore == nil {
			return notImpl("restore")
		}
		return d.Restore.Restore(ctx, c)
	},
	"restore-test": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Restore == nil {
			return notImpl("restore-test")
		}
		return d.Restore.RestoreTest(ctx, c)
	},
	"restore-dest enroll": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Restore == nil {
			return notImpl("restore-dest-enroll")
		}
		return d.Restore.RestoreDestEnroll(ctx, c)
	},
	"restore-dest check": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Restore == nil {
			return notImpl("restore-dest-check")
		}
		return d.Restore.RestoreDestCheck(ctx, c)
	},
	"source enroll": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Acquire == nil {
			return notImpl("source-enroll")
		}
		return d.Acquire.SourceEnroll(ctx, c)
	},
	"source check": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Acquire == nil {
			return notImpl("source-check")
		}
		return d.Acquire.SourceCheck(ctx, c)
	},
	"source survey": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Acquire == nil {
			return notImpl("source-survey")
		}
		return d.Acquire.SourceSurvey(ctx, c)
	},
	"propose": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Approval == nil {
			return notImpl("propose")
		}
		return d.Approval.Propose(ctx, c)
	},
	"approve": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Approval == nil {
			return notImpl("approve")
		}
		return d.Approval.Approve(ctx, c)
	},
	"cancel": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Approval == nil {
			return notImpl("cancel")
		}
		return d.Approval.Cancel(ctx, c)
	},
	"attest": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Approval == nil {
			return notImpl("attest")
		}
		return d.Approval.Attest(ctx, c)
	},
	"pending": func(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
		if d.Approval == nil {
			return notImpl("pending")
		}
		return d.Approval.Pending(ctx, c)
	},
}

var noLockDeadline = map[string]bool{"cancel": true, "hold set": true, "approve": true, "status": true, "pending": true, "def validate": true, "def show": true}
