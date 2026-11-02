package adapter

// 完了条件 5(結果の contract の版)、6(終了コードの読み替え)、10(出力の規則)。

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func errLine(code, detail string) string {
	return `{"contract":"sitate.notify/v1","error":{"code":"` + code + `","detail":` + detail + `}}`
}

func TestRunResultContractMismatch(t *testing.T) {
	bad := strings.Replace(okResult, "sitate.notify/v1", "sitate.notify/v2", 1)
	b := build(t, notifySpec("cat >/dev/null; echo '"+bad+"'"))
	_, err := runNotify(b)
	internalWith(t, err, "contract")
}

func TestRunExitCodeMapping(t *testing.T) {
	cases := map[string]struct {
		script string
		reason string
	}{
		"終了 2":           {"cat >/dev/null; exit 2", "exit_code"},
		"終了 127":         {"cat >/dev/null; exit 127", "exit_code"},
		"終了 0 で出力なし":     {"cat >/dev/null; exit 0", "output"},
		"終了 1 で出力なし":     {"cat >/dev/null; exit 1", "output"},
		"核が送っていないシグナル":   {"cat >/dev/null; kill -9 $$", "exit_code"},
		"終了 2 でも結果の行を出す": {"cat >/dev/null; echo '" + okResult + "'; exit 2", "exit_code"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := build(t, notifySpec(c.script))
			_, err := runNotify(b)
			internalWith(t, err, c.reason)
		})
	}
}

func TestRunExit1NotImplemented(t *testing.T) {
	b := build(t, notifySpec("cat >/dev/null; echo '"+errLine("NOT_IMPLEMENTED", `{"feature":"digest"}`)+"'; exit 1"))
	_, err := runNotify(b)
	ce := errDetail(t, err, contract.CodeNotImplemented)
	if string(ce.Detail) != `{"feature":"digest"}` {
		t.Errorf("detail.feature: %s", ce.Detail)
	}
}

func TestRunExit1InternalAndAdapterCode(t *testing.T) {
	b := build(t, notifySpec("cat >/dev/null; echo '"+errLine("INTERNAL", `{"component":"adapter","reason":"contract"}`)+"'; exit 1"))
	_, err := runNotify(b)
	internalWith(t, err, "contract")

	b = build(t, notifySpec("cat >/dev/null; echo '"+errLine("adapter:webhook:TLS_FAILED", `{}`)+"'; exit 1"))
	_, err = runNotify(b)
	ce := errDetail(t, err, contract.CodeAdapterError)
	var d map[string]string
	_ = json.Unmarshal(ce.Detail, &d)
	if d["adapter_code"] != "adapter:webhook:TLS_FAILED" || d["kind"] != "notify" || d["name"] != "webhook" {
		t.Errorf("ADAPTER_ERROR の detail: %s", ce.Detail)
	}
	// 他のアダプタの名前を名乗る code は契約違反
	b = build(t, notifySpec("cat >/dev/null; echo '"+errLine("adapter:email:TLS_FAILED", `{}`)+"'; exit 1"))
	_, err = runNotify(b)
	internalWith(t, err, "output")
}

func TestRunExit1KindSpecificCodeIsAdapterError(t *testing.T) {
	line := `{"contract":"sitate.acquire/v1","error":{"code":"STORAGE_UNREACHABLE","detail":{"target":"source","reason":"connect"}}}`
	s := spec{kind: "acquire", name: "files", script: "cat >/dev/null; echo '" + line + "'; exit 1"}
	b := build(t, s)
	res, err := b.r.Run(ctx, contract.RunContext{}, "acquire", "files", acquireReq(), nil)
	if res != nil {
		t.Errorf("終了 1 は結果を返さない: %s", res)
	}
	ce := errDetail(t, err, contract.CodeAdapterError)
	if ce.Adapter == nil || ce.Adapter.Code != "STORAGE_UNREACHABLE" || string(ce.Detail) != `{"kind":"acquire","name":"files","adapter_code":"STORAGE_UNREACHABLE"}` {
		t.Errorf("Adapter・detail: %+v %s", ce.Adapter, ce.Detail)
	}
}

func TestRunExit1AdapterCarriesWholeError(t *testing.T) {
	line := `{"contract":"sitate.provision/v1","error":{"code":"BUCKET_MISMATCH","detail":{},"orphaned_key_ids":["k1","k2"]}}`
	b := build(t, spec{kind: "provision", name: "b2", script: "cat >/dev/null; echo '" + line + "'; exit 1", secrets: []string{"admin"}})
	req := []byte(`{"contract":"sitate.provision/v1","op":"check","endpoint":"https://s3.example.invalid","bucket":"b","prefix":"","key_role":null,"secrets":{"admin":{"key_id":"k","secret":"s"}}}`)
	_, err := b.r.Run(ctx, contract.RunContext{}, "provision", "b2", req, nil)
	ce := errDetail(t, err, contract.CodeAdapterError)
	if ce.Adapter == nil || len(ce.Adapter.OrphanedKeyIDs) != 2 {
		t.Errorf("orphaned_key_ids が載っていない: %+v", ce.Adapter)
	}
}

// detail に核が補う項目を持たせない規則: スキーマの detail に無い項目を持つ error は INTERNAL(output)。
func TestRunErrorDetailWithCoreFilledItemRejected(t *testing.T) {
	line := `{"contract":"sitate.acquire/v1","error":{"code":"STORAGE_UNREACHABLE","detail":{"target":"source","reason":"connect","run_id":"r-1"}}}`
	b := build(t, spec{kind: "acquire", name: "files", script: "cat >/dev/null; echo '" + line + "'; exit 1"})
	_, err := b.r.Run(ctx, contract.RunContext{}, "acquire", "files", acquireReq(), nil)
	internalWith(t, err, "output")

	b = build(t, notifySpec("cat >/dev/null; echo '"+errLine("INTERNAL", `{"component":"adapter","reason":"contract","exit":3}`)+"'; exit 1"))
	_, err = runNotify(b)
	internalWith(t, err, "output")
}

func TestRunOutputRules(t *testing.T) {
	cases := map[string]string{
		"改行で終わらない":     "cat >/dev/null; printf '%s' '" + okResult + "'",
		"結果の後に別の行":     "cat >/dev/null; echo '" + okResult + "'; echo extra",
		"結果の前に別の行":     "cat >/dev/null; echo junk; echo '" + okResult + "'",
		"結果が 2 行":      "cat >/dev/null; echo '" + okResult + "'; echo '" + okResult + "'",
		"JSON でない":     "cat >/dev/null; echo not-json",
		"スキーマに合わない":    "cat >/dev/null; echo '{\"contract\":\"sitate.notify/v1\",\"delivered\":\"yes\"}'",
		"表に無いキー":       "cat >/dev/null; echo '" + strings.Replace(okResult, `"delivered":true`, `"delivered":true,"x":1`, 1) + "'",
		"終了 0 に error": "cat >/dev/null; echo '" + errLine("INTERNAL", `{"component":"adapter","reason":"output"}`) + "'",
		"終了 1 に結果":     "cat >/dev/null; echo '" + okResult + "'; exit 1",
		"フレームを出す":      "cat >/dev/null; echo '{\"type\":\"end\",\"bytes\":0,\"hash\":\"blake3:" + strings.Repeat("0", 64) + "\",\"exit_status\":0}'; echo '" + okResult + "'",
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			b := build(t, notifySpec(script))
			_, err := runNotify(b)
			internalWith(t, err, "output")
		})
	}
}

// error の全体(resources、seen_fp)を Error.Adapter に載せる(restore-dest)。
func TestRunExit1RestoreDestCarriesResourcesAndSeenFP(t *testing.T) {
	line := `{"contract":"sitate.restore-dest/v1","error":{"code":"hostkey_mismatch","detail":{},"seen_fp":"SHA256:abcDEF123","resources":[{"type":"instance","id":"i-0abc"}]}}`
	b := build(t, spec{kind: "restore-dest", name: "ssh-host", script: "cat >/dev/null; echo '" + line + "'; exit 1", secrets: []string{"dest_key"},
		mod: func(m map[string]any) { m["pair"] = "start_stop" }})
	req := []byte(`{"contract":"sitate.restore-dest/v1","op":"destroy","session_id":"r-20261103T010203Z-3f9a","dest":{"at":"dest.example.invalid","key_ref":"dest-key"},"host_key_fp":null,"resources":[{"type":"instance","id":"i-0abc"}],"secrets":{"dest_key":"x"}}`)
	_, err := b.r.Run(ctx, contract.RunContext{}, "restore-dest", "ssh-host", req, nil)
	ce := errDetail(t, err, contract.CodeAdapterError)
	a := ce.Adapter
	if a == nil || a.Code != "hostkey_mismatch" || a.SeenFP != "SHA256:abcDEF123" || len(a.Resources) != 1 || a.Resources[0].ID != "i-0abc" {
		t.Errorf("Error.Adapter: %+v", a)
	}
}

// error.resources[] は、どの読み替えでも Error.Adapter.Resources に届く(start の resources[] が空に見えると資源が残る。3.2.4)。
func TestRunExit1ResourcesReachAdapterForEveryCode(t *testing.T) {
	cases := map[string]struct {
		code, detail string
		want         contract.Code
	}{
		"INTERNAL":        {"INTERNAL", `{"component":"adapter","reason":"contract"}`, contract.CodeInternal},
		"NOT_IMPLEMENTED": {"NOT_IMPLEMENTED", `{"feature":"digest"}`, contract.CodeNotImplemented},
		"アダプタ固有":          {"adapter:ssh-host:BOOM", `{}`, contract.CodeAdapterError},
		"kind 固有":         {"quota", `{}`, contract.CodeAdapterError},
	}
	req := []byte(`{"contract":"sitate.restore-dest/v1","op":"destroy","session_id":"r-20261103T010203Z-3f9a","dest":{"at":"dest.example.invalid","key_ref":"dest-key"},"host_key_fp":null,"resources":[{"type":"instance","id":"i-0abc"}],"secrets":{"dest_key":"x"}}`)
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			line := `{"contract":"sitate.restore-dest/v1","error":{"code":"` + c.code + `","detail":` + c.detail + `,"resources":[{"type":"instance","id":"i-0abc"}]}}`
			b := build(t, spec{kind: "restore-dest", name: "ssh-host", script: "cat >/dev/null; echo '" + line + "'; exit 1", secrets: []string{"dest_key"},
				mod: func(m map[string]any) { m["pair"] = "start_stop" }})
			_, err := b.r.Run(ctx, contract.RunContext{}, "restore-dest", "ssh-host", req, nil)
			ce := errDetail(t, err, c.want)
			if ce.Adapter == nil || len(ce.Adapter.Resources) != 1 || ce.Adapter.Resources[0].ID != "i-0abc" {
				t.Errorf("Error.Adapter.Resources: %+v", ce.Adapter)
			}
		})
	}
}
