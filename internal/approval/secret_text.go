package approval

import (
	"encoding/json"
	"strings"
)

// secretText は秘密の JSON 値から鍵の文字列を取り出す(文字列か、private_key を持つ object。引用符付きの字面のまま渡さない)。
func secretText(raw json.RawMessage) []byte {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []byte(s)
	}
	var o struct {
		PrivateKey string `json:"private_key"`
	}
	if json.Unmarshal(raw, &o) == nil && o.PrivateKey != "" {
		return []byte(o.PrivateKey)
	}
	return []byte(strings.TrimSpace(string(raw)))
}
