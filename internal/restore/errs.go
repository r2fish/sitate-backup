package restore

import (
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// newErr は型付きの誤りを作る(class・retryable は原因コードの表から)。detail が nil なら {}。
func newErr(code contract.Code, detail any) *contract.Error {
	info := contract.CodeTable[code]
	b := []byte("{}")
	if detail != nil {
		b, _ = json.Marshal(detail)
	}
	return &contract.Error{Code: code, Class: contract.ExitClass(info.Exit), Retryable: info.Retryable, Detail: b, Next: []contract.Next{}}
}

// internalErr は INTERNAL(detail.component: restore)。reason は exit_code / output / contract か空(null)。
func internalErr(reason string) *contract.Error {
	var rs any
	if reason != "" {
		rs = reason
	}
	return newErr(contract.CodeInternal, map[string]any{"component": "restore", "http_status": nil, "kind": nil, "name": nil, "reason": rs})
}

func argInvalid(flag, reason string) *contract.Error {
	return newErr(contract.CodeArgInvalid, map[string]any{"flag": flag, "reason": reason})
}

func reply(v any) (contract.Reply, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return contract.Reply{}, internalErr("")
	}
	return contract.Reply{Result: b}, nil
}

func warn(code contract.Code, detail any) contract.Warning {
	b, _ := json.Marshal(detail)
	return contract.Warning{Code: code, Detail: b, Next: []contract.Next{}}
}

// arg は Call.Args の最初の値(無ければ空)。
func arg(c contract.Call, k string) string {
	if v := c.Args[k]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func hasArg(c contract.Call, k string) bool { _, ok := c.Args[k]; return ok }
