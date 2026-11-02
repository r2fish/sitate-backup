package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// 検査が読むデータ(製品リポジトリの中のものだけ)。
type lintData struct {
	public    map[string]string          // "def validate" -> tool|ui-only|internal|test-only
	flags     map[string]map[string]bool // サブコマンド -> 受ける --<名前>
	codes     map[string]bool            // error.code と warnings[].code の一覧
	sendWhere map[string]map[string]bool // where(サブコマンドまたは envelope) -> 送る欄の Pointer(<i> は *)
	schemas   *schemaSet                 // 結果スキーマ(完了確認の Pointer の存在の検査)
}

func readJSON(root, rel string, v any) error {
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func loadData(root string) (*lintData, error) {
	d := &lintData{public: map[string]string{}, flags: map[string]map[string]bool{}, codes: map[string]bool{}, sendWhere: map[string]map[string]bool{}, schemas: newSchemaSet(root)}
	for _, f := range []func(string) error{d.loadSubcommands, d.loadGate, d.loadCodes, d.loadAppCodes, d.loadSendFields} {
		if err := f(root); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func (d *lintData) loadSubcommands(root string) error {
	var subs struct {
		Subcommands []struct {
			Command []string `json:"command"`
			Public  string   `json:"public"`
		} `json:"subcommands"`
	}
	if err := readJSON(root, "internal/contract/schema/cli/subcommands.json", &subs); err != nil {
		return err
	}
	for _, s := range subs.Subcommands {
		d.public[strings.Join(s.Command, " ")] = s.Public
	}
	return nil
}

func (d *lintData) loadGate(root string) error {
	var gate struct {
		Common []struct{ Name string } `json:"common_flags"`
		Subs   []struct {
			Command []string                `json:"command"`
			Flags   []struct{ Name string } `json:"flags"`
		} `json:"subcommands"`
	}
	if err := readJSON(root, "internal/contract/gatedata/allowlist.json", &gate); err != nil {
		return err
	}
	for _, s := range gate.Subs {
		m := map[string]bool{}
		for _, f := range append(append([]struct{ Name string }{}, gate.Common...), s.Flags...) {
			m[f.Name] = true
		}
		d.flags[strings.Join(s.Command, " ")] = m
	}
	return nil
}

func (d *lintData) loadCodes(root string) error {
	var env struct {
		Defs struct {
			Error struct {
				Properties struct{ Code struct{ Enum []string } }
			}
			Warning struct {
				Properties struct{ Code struct{ Enum []string } }
			}
		} `json:"$defs"`
	}
	if err := readJSON(root, "internal/contract/schema/cli/envelope.json", &env); err != nil {
		return err
	}
	for _, c := range append(env.Defs.Error.Properties.Code.Enum, env.Defs.Warning.Properties.Code.Enum...) {
		d.codes[c] = true
	}
	return nil
}

// toolBareCodes は spec 3.7 のアプリの原因コードの表で、キー app.tool.<名前> に対して TOOL_ を付けない名前(TOOL_DENIED・TOOL_ARG_INVALID だけが TOOL_ を付ける)。
var toolBareCodes = map[string]string{"human_cancelled": "HUMAN_CANCELLED", "path_ref_unknown": "PATH_REF_UNKNOWN", "quote_rejected": "QUOTE_REJECTED"}

// appCodeOf はメッセージカタログのキー app.<系>.<名前> から、表のアプリの原因コードを導く。
// 既定は <系>_<名前> の大文字(app.ssh.unreachable -> SSH_UNREACHABLE、app.tool.denied -> TOOL_DENIED)。app.tool の 3 件は表のとおり TOOL_ を付けない。
func appCodeOf(key string) (string, bool) {
	rest, ok := strings.CutPrefix(key, "app.")
	if !ok {
		return "", false
	}
	if name, ok := strings.CutPrefix(rest, "tool."); ok {
		if code, bare := toolBareCodes[name]; bare {
			return code, true
		}
	}
	return strings.ToUpper(strings.ReplaceAll(rest, ".", "_")), true
}

// loadAppCodes はアプリの原因コード(spec 3.7 の表。bootstrap の SSH_UNREACHABLE など)を、メッセージカタログのキー一覧の app.* から導く。
func (d *lintData) loadAppCodes(root string) error {
	var keys map[string]json.RawMessage
	if err := readJSON(root, "app/src/i18n/keys.json", &keys); err != nil {
		return err
	}
	for k := range keys {
		if code, ok := appCodeOf(k); ok {
			d.codes[code] = true
		}
	}
	return nil
}

func (d *lintData) loadSendFields(root string) error {
	var sf struct {
		Rows []struct {
			Where  string                     `json:"where"`
			Fields []struct{ Pointer string } `json:"fields"`
		} `json:"rows"`
	}
	if err := readJSON(root, "app/src/data/send-fields.json", &sf); err != nil {
		return err
	}
	for _, r := range sf.Rows {
		if d.sendWhere[r.Where] == nil {
			d.sendWhere[r.Where] = map[string]bool{}
		}
		for _, f := range r.Fields {
			d.sendWhere[r.Where][f.Pointer] = true
		}
	}
	return nil
}
