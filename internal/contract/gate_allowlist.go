package contract

import (
	_ "embed"
	"encoding/json"
)

// gate の許可リストのデータ(spec 3.1.5 の字句規則 4。サブコマンドごとの位置引数の値域と引数の名前)。
// 判定の実装は U1d。このファイルは埋め込んだデータの型と読み込みだけを持つ。

//go:embed gatedata/allowlist.json
var gateAllowlistJSON []byte

// GateFlag は受ける --<Name> と値の形(none / word / ref / json / base64 / int / enum / stdin_dash)。
type GateFlag struct {
	Name  string   `json:"name"`
	Value string   `json:"value"`
	Enum  []string `json:"enum,omitempty"`
}

// GatePositional は位置引数の値域。Values があればその列挙、Ref が true なら参照名の正規表現。
type GatePositional struct {
	Count  int      `json:"count"`
	Values []string `json:"values,omitempty"`
	Ref    bool     `json:"ref,omitempty"`
}

type GateCommand struct {
	Command    []string        `json:"command"`
	Positional *GatePositional `json:"positional"`
	Flags      []GateFlag      `json:"flags"`
	SecretFD   bool            `json:"secret_fd,omitempty"`
}

type GateAllowlist struct {
	CommonFlags     []GateFlag    `json:"common_flags"`
	RefPattern      string        `json:"ref_pattern"`
	RestoreToPrefix []string      `json:"restore_to_prefixes"`
	Subcommands     []GateCommand `json:"subcommands"`
	Excluded        []struct {
		Command []string `json:"command"`
		Reason  string   `json:"reason"`
	} `json:"excluded"`
}

// LoadGateAllowlist は埋め込んだ許可リストを読む。
func LoadGateAllowlist() (GateAllowlist, error) {
	var a GateAllowlist
	err := json.Unmarshal(gateAllowlistJSON, &a)
	return a, err
}
