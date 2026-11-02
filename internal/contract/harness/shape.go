package harness

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Envelope は入口の結果を封筒に包む(結果のスキーマ cli/envelope.json で検証するため)。
func Envelope(words []string, r contract.Reply) []byte {
	res := r.Result
	if len(res) == 0 {
		res = json.RawMessage("null")
	}
	var warns []contract.Warning
	warns = append(warns, r.Warnings...)
	if warns == nil {
		warns = []contract.Warning{}
	}
	m := map[string]any{"schema": "sitate.out/v1", "command": words, "ok": true, "dry_run": false,
		"run_id": "r-20261018T173000Z-3f9a", "result": res, "warnings": warns, "error": nil}
	b, _ := json.Marshal(m)
	return b
}

// Shape は入口の成功の応答を封筒のスキーマ(結果は subcommands 表の語で決まる)で検証する。
// 未実装の印・誤り・空の結果・スキーマ違反はどれも失敗にする(nil を返すだけの実装を通さない)。
func Shape(t *testing.T, words []string, r contract.Reply, err error) {
	t.Helper()
	Implemented(t, err)
	if err != nil {
		t.Fatalf("%v: 成功を期待したが誤り: %v", words, err)
	}
	if len(r.Result) == 0 || string(r.Result) == "null" {
		t.Fatalf("%v: 結果が空", words)
	}
	if verr := contract.Validate("cli/envelope.json", Envelope(words, r)); verr != nil {
		t.Fatalf("%v: 封筒のスキーマに合わない: %v", words, verr)
	}
}

// ExpectCode は err が原因コード code の型付きの誤りであることを確かめる(区分は原因コードの表のとおり)。
func ExpectCode(t *testing.T, err error, code contract.Code) *contract.Error {
	t.Helper()
	Implemented(t, err)
	var ce *contract.Error
	if !errors.As(err, &ce) {
		t.Fatalf("*contract.Error を期待したが %v", err)
	}
	if ce.Code != code {
		t.Fatalf("原因コード %s を期待したが %s", code, ce.Code)
	}
	info, ok := contract.CodeTable[ce.Code]
	if !ok || !slices.Contains(contract.ErrorCodes(), ce.Code) {
		t.Fatalf("%s は error の原因コードでない", ce.Code)
	}
	if ce.Class != contract.ExitClass(info.Exit) {
		t.Errorf("%s: class %q は表の %q と違う", ce.Code, ce.Class, contract.ExitClass(info.Exit))
	}
	return ce
}

// Result は応答の結果を m に読む。
func Result(t *testing.T, r contract.Reply, m any) {
	t.Helper()
	if err := json.Unmarshal(r.Result, m); err != nil {
		t.Fatalf("結果を読めない: %v", err)
	}
}

// ShapeOrError は、準備のない状態(空の偽物)で呼んだ入口が、契約の応答(封筒のスキーマに合う結果)か
// 原因コードの表にある型付きの誤りのどちらかを返すことを確かめる。未実装の印・panic・何も返さない実装(nil, 空の結果)は失敗する。
func ShapeOrError(t *testing.T, words []string, r contract.Reply, err error) {
	t.Helper()
	Implemented(t, err)
	if err == nil {
		Shape(t, words, r, nil)
		return
	}
	var ce *contract.Error
	if !errors.As(err, &ce) {
		t.Fatalf("%v: 型付きの誤り(*contract.Error)でない: %v", words, err)
	}
	if info, ok := contract.CodeTable[ce.Code]; !ok || !slices.Contains(contract.ErrorCodes(), ce.Code) || ce.Class != contract.ExitClass(info.Exit) {
		t.Fatalf("%v: 原因コード %q・区分 %q が表に合わない", words, ce.Code, ce.Class)
	}
}

// NilDeps は、依存を埋めない骨格の構造体の操作 f を呼び、panic せず、未実装の印でも成功でもなく、
// 原因の分かる誤り(INTERNAL。detail.component が部品名、ほかは null。spec 3.1.3)を返すことを確かめる。
// 依存を注入し忘れた結合の誤りは、製品の実装が nil への参照で落ちずにこの形で返す(U1f-2 の起動の試験が全依存の充足を確かめる)。
func NilDeps(t *testing.T, component string, f func() error) {
	t.Helper()
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				if ni, ok := r.(*contract.NotImplemented); ok {
					t.Fatalf("%s %s", NotImplementedMarker, ni.Feature)
				}
				t.Fatalf("依存が nil のとき panic した: %v", r)
			}
		}()
		err = f()
	}()
	ce := ExpectCode(t, err, contract.CodeInternal)
	var d struct {
		Component *string `json:"component"`
	}
	_ = json.Unmarshal(ce.Detail, &d)
	if d.Component == nil || *d.Component != component {
		t.Errorf("detail.component は %q を期待したが %s", component, ce.Detail)
	}
	env := map[string]any{"schema": "sitate.out/v1", "command": []string{"x"}, "ok": false, "dry_run": false, "run_id": "r-20261018T173000Z-3f9a", "result": nil, "warnings": []any{},
		"error": map[string]any{"code": ce.Code, "class": ce.Class, "retryable": false, "detail": json.RawMessage(ce.Detail), "next": []any{}}}
	b, _ := json.Marshal(env)
	if verr := contract.Validate("cli/envelope.json", b); verr != nil {
		t.Errorf("INTERNAL の封筒がスキーマに合わない: %v", verr)
	}
}
