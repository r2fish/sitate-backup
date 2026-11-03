package approval

// 提案の節の厳密な検査(まとめレビュー F3)。source-change・restore-change・notify-add・notify-change は、提案の節を今の定義に当てはめた定義を
// 定義の検査(ValidateDef)に通す。Go の encoding/json はキーの大文字小文字を畳んで struct へ読む(`hoſt` は `host` と同じになる)ので、
// 未知のキーと畳み込みの重複を取得・登録・カードの前に止めないと、カードに出ない宛先へ登録が走る。検査を通した節の正規形だけを args に残す。

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// checkSection は節 key を raw に置き換えた定義(今の定義 cur が土台)を検査する。strip は定義に入れない項目(admin_user・host_key_fp)。
// 通ったら raw の正規形を返す。指摘があれば DEF_INVALID。参照名の解決は省く(登録の前の秘密は定義の検査の対象でない)。
func (a *Approval) checkSection(ctx context.Context, cur json.RawMessage, key string, raw json.RawMessage, strip ...string) (json.RawMessage, error) {
	var sec map[string]json.RawMessage
	if json.Unmarshal(raw, &sec) != nil || sec == nil {
		return nil, argErr("--"+key, "format")
	}
	root := map[string]json.RawMessage{}
	if len(cur) > 0 {
		if json.Unmarshal(cur, &root) != nil || root == nil {
			root = map[string]json.RawMessage{}
		}
	}
	root[key] = withoutKeys(raw, strip...)
	b, _ := json.Marshal(root)
	_, _, issues, err := a.State.ValidateDef(ctx, b, true)
	if err != nil {
		return nil, err
	}
	if len(issues) > 0 && string(issues) != "[]" && string(issues) != "null" {
		return nil, apiErr(contract.CodeDefInvalid, map[string]any{"issues": issues})
	}
	nb, _ := json.Marshal(sec)
	return contract.CanonicalJSON(nb)
}
