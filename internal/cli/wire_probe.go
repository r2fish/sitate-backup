package cli

// 結線: bucket check の試し(contract.StoreProbe)。store.StoreProbe の版指定なしの削除・リーガルホールドは実物の *store.ObjectStore だけを受けるため、
// 呼び出しごとに所在と鍵を解決した実物を役割ごとに渡す(dynStore を直接渡すと type assertion が通らず INTERNAL になる)。

import (
	"context"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/store"
)

// dynProbe は contract.StoreProbe。直近の PutProbe の印(キーと版)は内側の store.StoreProbe が持つ。
type dynProbe struct {
	write, read *dynStore
	inner       *store.StoreProbe
}

var _ contract.StoreProbe = (*dynProbe)(nil)

func newDynProbe(write, read *dynStore) *dynProbe {
	return &dynProbe{write: write, read: read, inner: &store.StoreProbe{}}
}

// bind は呼び出しの所在と鍵で、役割ごとの実物を内側に入れる。
func (p *dynProbe) bind(ctx context.Context) error {
	w, err := p.write.resolve(ctx)
	if err != nil {
		return err
	}
	r, err := p.read.resolve(ctx)
	if err != nil {
		return err
	}
	p.inner.Keys = map[string]contract.ObjectStore{"write": w, "read": r}
	return nil
}

// PutProbe は contract.StoreProbe を満たす。
func (p *dynProbe) PutProbe(ctx context.Context, key string, body []byte) (string, error) {
	if err := p.bind(ctx); err != nil {
		return "", err
	}
	return p.inner.PutProbe(ctx, key, body)
}

// Try は contract.StoreProbe を満たす。
func (p *dynProbe) Try(ctx context.Context, role, op string) (bool, error) {
	if err := p.bind(ctx); err != nil {
		return false, err
	}
	return p.inner.Try(ctx, role, op)
}
