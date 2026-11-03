package acquire

import (
	"context"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// runDefinition は取得に使う定義: 承認済みの定義。seal の前(NOT_SEALED)は候補の定義(def validate に通った最新の内容)を使う。
// 取得は seal の前も止めない(spec 3.1.3 の NOT_SEALED、D-0025 の 5)。候補も無ければ NOT_SEALED のまま返す。
func (a *Acquire) runDefinition(ctx context.Context) ([]byte, error) {
	if err := a.requirePhraseConfirmed(ctx); err != nil {
		return nil, err
	}
	raw, _, err := a.State.ReadDef(ctx)
	if e, ok := err.(*contract.Error); !ok || e.Code != contract.CodeNotSealed {
		return raw, err
	}
	if sealed, serr := getBool(ctx, a.State, "sealed"); serr != nil {
		return nil, serr
	} else if sealed {
		return nil, err // seal の後に approved.json だけが失われた状態では、候補へ開かない
	}
	cand, _, found, cerr := a.State.ReadCandidate(ctx)
	if cerr != nil {
		return nil, cerr
	}
	if !found {
		return nil, err
	}
	return cand, nil
}

// requirePhraseConfirmed は確定(phrase verify)を終えるまで取得を始めない(spec 2902 行、3.1.3 の PHRASE_UNCONFIRMED)。
// seal の前も取得は止めないため(D-0025 の 5)、NOT_SEALED では止まらなくなった入口が確定の前に開かないようここで止める。
func (a *Acquire) requirePhraseConfirmed(ctx context.Context) error {
	ok, err := getBool(ctx, a.State, "phrase_confirmed")
	if err != nil {
		return err
	}
	if !ok {
		return mkErr(contract.CodePhraseUnconfirmed, map[string]any{}, nextOf(contract.CodePhraseUnconfirmed, false, "phrase", "verify"))
	}
	return nil
}
