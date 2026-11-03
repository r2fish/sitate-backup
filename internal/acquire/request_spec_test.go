package acquire

// 取得アダプタの要求の対象の形(spec 3.2.2。D-0172): files は spec.path 必須、postgresql は port だけで path を置かない。

import (
	"encoding/json"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

func requestOf(t *testing.T, ct checkTarget) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(contract.AcquireRequest{Contract: contract.AdapterContract["acquire"], Target: targetSpec(ct)})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTargetSpecRequestShapeByKind(t *testing.T) {
	port := 5432
	pg := requestOf(t, checkTarget{ID: "t2", Postgresql: "shopdb", Port: &port})
	if err := contract.Validate("adapter/acquire.json#/$defs/request", pg); err != nil {
		t.Errorf("postgresql の要求が要求のスキーマを通らない: %v %s", err, pg)
	}
	var m struct {
		Target struct {
			Spec map[string]any `json:"spec"`
		} `json:"target"`
	}
	_ = json.Unmarshal(pg, &m)
	if _, has := m.Target.Spec["path"]; has {
		t.Errorf("postgresql の要求の spec に path がある: %s", pg)
	}
	files := requestOf(t, checkTarget{ID: "t1", Files: "/srv/app"})
	if err := contract.Validate("adapter/acquire.json#/$defs/request", files); err != nil {
		t.Errorf("files の要求が要求のスキーマを通らない: %v %s", err, files)
	}
	if err := contract.Validate("adapter/acquire.json#/$defs/request", requestOf(t, checkTarget{ID: "t1"})); err == nil {
		t.Error("path の無い files の要求がスキーマを通った")
	}
}

// 対象の失敗は原因コードの detail・next を持つ(spec 3.1.2 の targets[]。D-0172)。INTERNAL では component・reason が入る。
func TestFailedTargetCarriesDetailAndNext(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	e.ad.Script("acquire", fake.Step{Outcome: fake.OutcomeError, ErrCode: "STORAGE_UNREACHABLE",
		ErrAdapter: &contract.AdapterError{Code: "STORAGE_UNREACHABLE", Detail: json.RawMessage(`{"target":"source","reason":"connect"}`)}})
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	tg := r.Targets[0]
	if tg.ErrorCode != "STORAGE_UNREACHABLE" || string(tg.Detail) != `{"target":"source","reason":"connect"}` || tg.Next == nil {
		t.Errorf("アダプタの error の detail が載らない: %+v", tg)
	}

	e = newIngestEnv(t, ingestDef)
	e.ad.Script("acquire", fake.Step{Outcome: fake.OutcomeError, ErrCode: "INTERNAL",
		ErrAdapter: &contract.AdapterError{Code: "INTERNAL", Detail: json.RawMessage(`{"component":"adapter","http_status":null,"kind":"acquire","name":"files","reason":"output"}`)}})
	r, _ = e.run(t)
	var d struct{ Component, Reason string }
	_ = json.Unmarshal(r.Targets[0].Detail, &d)
	if r.Targets[0].ErrorCode != "INTERNAL" || d.Component != "adapter" || d.Reason != "output" {
		t.Errorf("INTERNAL の component・reason が載らない: %+v", r.Targets[0])
	}

	e = newIngestEnv(t, ingestDef) // end の hash の不一致は核が見つける契約違反: INTERNAL(component: adapter、reason: output)
	e.script(newFrames().dir(".").file("a", []byte("x")).end(true), okResult("ok"))
	r, _ = e.run(t)
	d.Component, d.Reason = "", ""
	_ = json.Unmarshal(r.Targets[0].Detail, &d)
	if r.Targets[0].ErrorCode != "INTERNAL" || d.Component != "adapter" || d.Reason != "output" || r.Targets[0].Next == nil {
		t.Errorf("契約違反の detail: %+v", r.Targets[0])
	}
}
