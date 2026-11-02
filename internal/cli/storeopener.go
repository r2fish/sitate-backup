package cli

import (
	"context"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/store"
)

// NewStoreOpener は contract.StoreOpener の実物(restore の (a): 状態を持たない復元先が引数の所在と読み取り鍵で保管先を開く)。
// store の実物を注入する結線(作業指示書 5.17 の完了条件 24 の例外)なので、結線の時に wire.go が Deps.StoreOpener へ入れる。
// 所在と鍵の欠けは ARG_INVALID(missing)。
func NewStoreOpener(d *Deps) contract.StoreOpener {
	return func(_ context.Context, addr contract.StoreAddr, keyID string, secret *contract.Secret) (contract.ObjectStore, error) {
		switch {
		case addr.Endpoint == "":
			return nil, argErr("--endpoint", "missing")
		case addr.Bucket == "":
			return nil, argErr("--bucket", "missing")
		case keyID == "" || secret == nil:
			return nil, argErr("--snapshot-key-stdin", "missing")
		}
		prefix := addr.Prefix
		if prefix != "" && !strings.HasSuffix(prefix, "/") {
			prefix += "/" // spec 4.2 の <p> は末尾に / を持つ
		}
		var off contract.Offsetter
		if o, ok := d.Clock.(contract.Offsetter); ok {
			off = o
		}
		// secret は呼び出しの後に呼び出し側が消してよいので、store には複製を渡す。
		s := string(secret.Reveal())
		return &store.ObjectStore{Endpoint: addr.Endpoint, Bucket: addr.Bucket, AccessKey: keyID, SecretKey: s, Prefix: prefix, Params: d.Retention, Offset: off, Region: regionOf(addr.Endpoint), Clock: d.Clock}, nil
	}
}
