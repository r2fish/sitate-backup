package adapter

import (
	"bytes"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// prepRequest は要求の contract の版を照合し、
// secrets のうち adapter.json の secrets[] に無い枠の名前を落とした 1 行(改行なし)と、
// 元の secrets の値(伏せ字の対象。落とした枠の値も含める)を返す。
func prepRequest(kind, name string, allowed []string, req json.RawMessage) (line []byte, secretVals []string, err error) {
	var top map[string]json.RawMessage
	if json.Unmarshal(req, &top) != nil {
		return nil, nil, internalErr(kind, name, "request")
	}
	var ver string
	if json.Unmarshal(top["contract"], &ver) != nil || ver != contract.AdapterContract[kind] {
		return nil, nil, internalErr(kind, name, "contract")
	}
	if contract.Validate("adapter/"+kind+".json#/$defs/request", req) != nil {
		return nil, nil, internalErr(kind, name, "request")
	}
	if raw, ok := top["secrets"]; ok {
		var slots map[string]json.RawMessage
		if json.Unmarshal(raw, &slots) != nil {
			return nil, nil, internalErr(kind, name, "request")
		}
		kept := map[string]json.RawMessage{}
		for k, v := range slots {
			collectStrings(v, &secretVals)
			if contains(allowed, k) {
				kept[k] = v
			}
		}
		top["secrets"], _ = json.Marshal(kept)
	}
	b, merr := json.Marshal(top)
	if merr != nil {
		return nil, nil, internalErr(kind, name, "request")
	}
	var buf bytes.Buffer
	if json.Compact(&buf, b) != nil {
		return nil, nil, internalErr(kind, name, "request")
	}
	return buf.Bytes(), secretVals, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// collectStrings は JSON の値の中の文字列(入れ子を含む)を集める。
func collectStrings(raw json.RawMessage, out *[]string) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return
	}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			*out = append(*out, t)
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
}
