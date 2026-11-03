package adapter

import (
	"encoding/json"
	"fmt"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// newErr は原因コードの表から class と retryable を引いた *contract.Error を作る。
// detail は固定の項目だけで組み、アダプタの標準エラーや要求の秘密を入れない。
func newErr(code contract.Code, detail string) *contract.Error {
	info := contract.CodeTable[code]
	return &contract.Error{Code: code, Class: contract.ExitClass(info.Exit), Retryable: info.Retryable,
		Detail: json.RawMessage(detail), Next: []contract.Next{}}
}

// internalErr は INTERNAL(3.1.3 の detail: component・http_status・kind・name・reason の 5 項目)。
// component は常に adapter、http_status は null。kind・name・reason は列挙(reason は exit_code / output / contract / signature)や
// 識別子の形に合うものだけ入れ、合わないもの(要求の不正など)は null にする。
func internalErr(kind, name, reason string) *contract.Error {
	m := map[string]any{"component": "adapter", "http_status": nil, "kind": nil, "name": nil, "reason": nil}
	if _, ok := contract.AdapterContract[kind]; ok {
		m["kind"] = kind
	}
	if reName.MatchString(name) {
		m["name"] = name
	}
	switch reason {
	case "exit_code", "output", "contract", "signature":
		m["reason"] = reason
	}
	b, _ := json.Marshal(m)
	return newErr(contract.CodeInternal, string(b))
}

func timeoutErr(kind, name string, limit int64) *contract.Error {
	return newErr(contract.CodeAdapterTimeout, fmt.Sprintf(`{"kind":%q,"name":%q,"timeout_s":%d}`, kind, name, limit))
}

func abortedErr(reason string) *contract.Error {
	return newErr(contract.CodeRunAborted, fmt.Sprintf(`{"reason":%q}`, reason))
}
