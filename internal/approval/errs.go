package approval

import (
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// apiErr は原因コードの表(contract.CodeTable)に合わせた型付きの誤りを作る。detail は 3.1.3 のスキーマに合わせて呼び出し側が渡す。
func apiErr(code contract.Code, detail any) *contract.Error {
	info := contract.CodeTable[code]
	b, err := json.Marshal(detail)
	if err != nil || detail == nil {
		b = []byte(`{}`)
	}
	return &contract.Error{Code: code, Class: contract.ExitClass(info.Exit), Retryable: info.Retryable, Detail: b, Next: []contract.Next{}}
}

// internalErr は依存の欠けなどの結合の誤り(INTERNAL。detail.component は approval。spec 3.1.3)。
func internalErr() *contract.Error {
	return apiErr(contract.CodeInternal, map[string]any{"component": "approval", "http_status": nil, "kind": nil, "name": nil, "reason": nil})
}

func argErr(flag, reason string) *contract.Error {
	return apiErr(contract.CodeArgInvalid, map[string]any{"flag": flag, "reason": reason})
}

func stateErr(changeID string, state *string) *contract.Error {
	return apiErr(contract.CodeChangeStateInvalid, map[string]any{"change_id": changeID, "state": state})
}

func sigErr(reason string, skew *int64) *contract.Error {
	return apiErr(contract.CodeSignatureInvalid, map[string]any{"reason": reason, "skew_s": skew})
}

// hostkeyErr は指紋の食い違い(expected が空なら照らす値が無い)。
func hostkeyErr(expected, seen string) *contract.Error {
	var exp any
	if expected != "" {
		exp = expected
	}
	return apiErr(contract.CodeSourceHostkeyMismatch, map[string]any{"expected_fp": exp, "seen_fp": seen, "session_id": nil})
}
