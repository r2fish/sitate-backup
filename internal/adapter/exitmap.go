package adapter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// interpret は終了 0・1 の結果の行を読む(spec 3.2.1「終了コード」「版」「error の形」)。
// 終了 0 は結果をそのまま返す。終了 1 は、NOT_IMPLEMENTED・INTERNAL・adapter:<name>:<CODE> を核の原因コードに読み替え、
// それ以外(kind 固有の code を含む)は ADAPTER_ERROR で、Error.Adapter に error の全体を載せる(呼び出し側が 3.2.1 の表で写す)。
func interpret(kind, name string, exit int, line []byte) (json.RawMessage, error) {
	var top map[string]json.RawMessage
	if json.Unmarshal(line, &top) != nil {
		return nil, internalErr(kind, name, "output")
	}
	var ver string
	if json.Unmarshal(top["contract"], &ver) == nil && ver != contract.AdapterContract[kind] {
		return nil, internalErr(kind, name, "contract")
	}
	if contract.Validate("adapter/"+kind+".json#/$defs/response", line) != nil {
		return nil, internalErr(kind, name, "output")
	}
	errRaw, hasErr := top["error"]
	if exit == 0 {
		if hasErr {
			return nil, internalErr(kind, name, "output")
		}
		return json.RawMessage(line), nil
	}
	if !hasErr {
		return nil, internalErr(kind, name, "output")
	}
	var ae contract.AdapterError
	if json.Unmarshal(errRaw, &ae) != nil {
		return nil, internalErr(kind, name, "output")
	}
	return mapAdapterError(kind, name, ae)
}

func mapAdapterError(kind, name string, ae contract.AdapterError) (json.RawMessage, error) {
	var e *contract.Error
	switch {
	case ae.Code == "NOT_IMPLEMENTED" && kind != "check":
		var d struct {
			Feature string `json:"feature"`
		}
		_ = json.Unmarshal(ae.Detail, &d)
		e = newErr(contract.CodeNotImplemented, fmt.Sprintf(`{"feature":%q}`, d.Feature))
	case ae.Code == "INTERNAL":
		var d struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(ae.Detail, &d)
		e = internalErr(kind, name, d.Reason) // 原文は Error.Adapter に残し、detail は 3.1.3 の形で組み直す
	default:
		if strings.HasPrefix(ae.Code, "adapter:") && !strings.HasPrefix(ae.Code, "adapter:"+name+":") {
			return nil, internalErr(kind, name, "output") // adapter:<name>:<CODE> の <name> はそのアダプタの名前
		}
		// kind 固有の code は ADAPTER_ERROR に載せ、写す先は呼び出し側が Error.Adapter から決める
		e = newErr(contract.CodeAdapterError, fmt.Sprintf(`{"kind":%q,"name":%q,"adapter_code":%q}`, kind, name, ae.Code))
	}
	e.Adapter = &ae
	return nil, e
}
