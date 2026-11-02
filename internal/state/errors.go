package state

import (
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// newErr は原因コードの表(spec 3.1.3)から class・retryable を引いた型付きの誤りを作る。detail が nil なら空のオブジェクト。
func newErr(code contract.Code, detail map[string]any) *contract.Error {
	info := contract.CodeTable[code]
	if detail == nil {
		detail = map[string]any{}
	}
	b, _ := json.Marshal(detail)
	return &contract.Error{Code: code, Class: contract.ExitClass(info.Exit), Retryable: info.Retryable, Detail: b, Next: []contract.Next{}}
}

// internalErr は I/O の失敗などを INTERNAL(component: state)にする。detail は INTERNAL のスキーマの全項目を持つ(原因の文はパスを含み得るため入れない)。
func internalErr() *contract.Error {
	return newErr(contract.CodeInternal, map[string]any{"component": "state", "http_status": nil, "kind": nil, "name": nil, "reason": nil})
}

// lockedErr は state.lock を締め切りまでに取れなかった LOCKED(spec 3.1.3、7.5 StateFiles の補足)。
func lockedErr() *contract.Error {
	return newErr(contract.CodeLocked, map[string]any{"lock": "state", "holder_command": nil, "holder_started_at": nil})
}
