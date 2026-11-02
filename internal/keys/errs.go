package keys

// 誤りと警告の組み立て(spec 3.1.3 の detail の形。schema/cli/detail.json)と、状態ファイル・時刻の小さな道具。

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func mkErr(code contract.Code, detail any) *contract.Error {
	b, err := json.Marshal(detail)
	if err != nil || len(b) == 0 {
		b = []byte("{}")
	}
	return &contract.Error{Code: code, Class: contract.ExitClass(contract.CodeTable[code].Exit), Retryable: contract.CodeTable[code].Retryable, Detail: b, Next: []contract.Next{}}
}

func mkWarn(code contract.Code, detail any) contract.Warning {
	b, _ := json.Marshal(detail)
	return contract.Warning{Code: code, Detail: b, Next: []contract.Next{}}
}

type empty struct{}

// errArg は ARG_INVALID(flag は "--name" の形。reason: unknown / missing / format / reused / mismatch)。
func errArg(flag, reason string) error {
	return mkErr(contract.CodeArgInvalid, map[string]string{"flag": flag, "reason": reason})
}

// errInternal は部品の組み立ての誤りや想定外の失敗(INTERNAL。detail.component は keys)。
func errInternal() error {
	return mkErr(contract.CodeInternal, map[string]any{"component": "keys", "http_status": nil, "kind": nil, "name": nil, "reason": nil})
}

func errPhraseInvalid(reason string) error {
	return mkErr(contract.CodePhraseInvalid, map[string]string{"reason": reason})
}

// errNeedDeps は依存が注入されていないときの INTERNAL。
func errNeedDeps(ok ...bool) error {
	for _, b := range ok {
		if !b {
			return errInternal()
		}
	}
	return nil
}

// adapterCode は誤りが ADAPTER_ERROR なら detail.adapter_code を返す(無ければ誤りの原因コード)。
func adapterCode(err error) string {
	var ce *contract.Error
	if !errors.As(err, &ce) {
		return string(contract.CodeAdapterError)
	}
	var d struct {
		AdapterCode string `json:"adapter_code"`
	}
	if json.Unmarshal(ce.Detail, &d) == nil && d.AdapterCode != "" {
		return d.AdapterCode
	}
	return string(ce.Code)
}

const timeFmt = "2006-01-02T15:04:05Z"

func fmtTime(t time.Time) string { return t.UTC().Format(timeFmt) }

func (k *Keys) now(ctx context.Context) (time.Time, error) {
	if k.Clock == nil {
		return time.Time{}, errInternal()
	}
	t, err := k.Clock.Now(ctx)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC().Truncate(time.Second), nil
}

// withLock は state.lock を取って f を実行し、必ず解放する(短い排他。保管先への書き込みの間は持たない)。
func (k *Keys) withLock(ctx context.Context, f func(h contract.StateLockHandle) error) error {
	h, err := k.State.Lock(ctx)
	if err != nil {
		return err
	}
	defer h.Release()
	return f(h)
}

// getItem は状態ファイルの項目を v に読む(無ければ false)。
func (k *Keys) getItem(ctx context.Context, item string, v any) (bool, error) {
	raw, err := k.State.Get(ctx, item)
	if err != nil {
		return false, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return false, nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, errInternal()
	}
	return true, nil
}

// setItems は状態ファイルの項目を 1 回の排他の中で置き換える(順に書く)。
func (k *Keys) setItems(ctx context.Context, kv ...any) error {
	return k.withLock(ctx, func(h contract.StateLockHandle) error {
		for i := 0; i+1 < len(kv); i += 2 {
			b, err := json.Marshal(kv[i+1])
			if err != nil {
				return errInternal()
			}
			if err := k.State.Update(ctx, h, kv[i].(string), b); err != nil {
				return err
			}
		}
		return nil
	})
}

func reply(res any, warns ...contract.Warning) (contract.Reply, error) {
	b, err := json.Marshal(res)
	if err != nil {
		return contract.Reply{}, errInternal()
	}
	if warns == nil {
		warns = []contract.Warning{}
	}
	return contract.Reply{Result: b, Warnings: warns}, nil
}

func argOne(c contract.Call, name string) (string, bool) {
	v := c.Args[name]
	if len(v) == 0 {
		return "", false
	}
	return v[0], true
}

func argFlag(c contract.Call, name string) bool {
	v, ok := c.Args[name]
	if !ok {
		return false
	}
	return len(v) == 0 || (v[0] != "false" && v[0] != "0")
}
