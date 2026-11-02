package cli

import (
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// newErr は原因コードの表(spec 3.1.3)から class・retryable を引いた型付きの誤りを作る。
func newErr(code contract.Code, detail map[string]any) *contract.Error {
	info := contract.CodeTable[code]
	if detail == nil {
		detail = map[string]any{}
	}
	b, _ := json.Marshal(detail)
	return &contract.Error{Code: code, Class: contract.ExitClass(info.Exit), Retryable: info.Retryable, Detail: b, Next: []contract.Next{}}
}

// argErr は ARG_INVALID(spec 3.1.3。flag は `--` 付き、reason は unknown|missing|format|reused|mismatch|unsupported_port)。
func argErr(flag, reason string) *contract.Error {
	return newErr(contract.CodeArgInvalid, map[string]any{"flag": flag, "reason": reason})
}

// internalErr は内部の失敗(INTERNAL の detail は全項目を持つ。原因の文はパスを含み得るので入れない)。
func internalErr() *contract.Error {
	return newErr(contract.CodeInternal, map[string]any{"component": "cli", "http_status": nil, "kind": nil, "name": nil, "reason": nil})
}

// toError は部品が返した誤りを封筒の error にする。型付きでない誤りと NOT_IMPLEMENTED は専用の形、そのほかは INTERNAL。
func toError(err error) *contract.Error {
	switch e := err.(type) {
	case *contract.Error:
		if e.Next == nil {
			c := *e
			c.Next = []contract.Next{}
			return &c
		}
		return e
	case *contract.NotImplemented:
		return newErr(contract.CodeNotImplemented, map[string]any{"feature": e.Feature})
	}
	return internalErr()
}

// exitOf は error の終了コード(原因コードの表。spec 3.1.1)。
func exitOf(e *contract.Error) int { return contract.CodeTable[e.Code].Exit }
