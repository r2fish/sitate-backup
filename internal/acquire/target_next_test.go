package acquire

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

func nx(code contract.Code, argv ...string) contract.Next { return nextOf(code, false, argv...) }

// failed の対象の next[] は 3.1.3 の表のその原因コードの行、errors[].code 由来・ADAPTER_ERROR・ok・partial では [](D-0179)。
func TestTargetNextByCause(t *testing.T) {
	a := &Acquire{State: &fake.StateFiles{Items: map[string]json.RawMessage{"sealed": json.RawMessage("true")}}}
	pre := &Acquire{State: &fake.StateFiles{Items: map[string]json.RawMessage{"sealed": json.RawMessage("false")}}}
	empty := []contract.Next{}
	cases := []struct {
		name string
		a    *Acquire
		out  TargetOutcome
		want []contract.Next
	}{
		{"STORAGE_UNREACHABLE", a, TargetOutcome{Status: "failed", ErrorCode: "STORAGE_UNREACHABLE"}, []contract.Next{nx("STORAGE_UNREACHABLE", "sitate", "backup"), nx("STORAGE_UNREACHABLE", "sitate", "doctor")}},
		{"MISMATCH seal 後", a, TargetOutcome{Status: "failed", ErrorCode: "RECEIVER_TARGETS_MISMATCH"}, []contract.Next{nx("RECEIVER_TARGETS_MISMATCH", "sitate", "propose", "source-change")}},
		{"MISMATCH seal 前", pre, TargetOutcome{Status: "failed", ErrorCode: "RECEIVER_TARGETS_MISMATCH"}, []contract.Next{nx("RECEIVER_TARGETS_MISMATCH", "sitate", "source", "enroll")}},
		{"INTERNAL acquire", a, TargetOutcome{Status: "failed", ErrorCode: "INTERNAL", FailInfo: FailInfo{Detail: json.RawMessage(`{"component":"acquire"}`)}}, []contract.Next{nx("INTERNAL", "sitate", "status")}},
		{"INTERNAL adapter", a, TargetOutcome{Status: "failed", ErrorCode: "INTERNAL", FailInfo: FailInfo{Detail: json.RawMessage(`{"component":"adapter"}`)}}, empty},
		{"ADAPTER_ERROR", a, TargetOutcome{Status: "failed", ErrorCode: "ADAPTER_ERROR"}, empty},
		{"NOT_FOUND", a, TargetOutcome{Status: "failed", ErrorCode: "NOT_FOUND"}, empty},
		{"PERMISSION", a, TargetOutcome{Status: "failed", ErrorCode: "PERMISSION"}, empty},
		{"EXIT_STATUS", a, TargetOutcome{Status: "failed", ErrorCode: "EXIT_STATUS"}, empty},
		{"partial", a, TargetOutcome{Status: "partial", ErrorCode: "STORAGE_UNREACHABLE"}, empty},
		{"ok", a, TargetOutcome{Status: "ok"}, empty},
	}
	for _, c := range cases {
		got := c.a.withNext(t.Context(), c.out).Next
		if got == nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: next = %+v, want %+v", c.name, got, c.want)
		}
	}
}

// ADAPTER_ERROR の detail は閉じた形(kind・name・adapter_code)、型の無い誤りは INTERNAL の component・reason が null(3.1.3。D-0180)。
func TestFailDetailClosedShapes(t *testing.T) {
	keys := func(b json.RawMessage) map[string]any {
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	withAdapter := &contract.Error{Code: contract.CodeAdapterError, Adapter: &contract.AdapterError{Code: "adapter:x:Boom", Detail: json.RawMessage(`{"extra":1}`)}, Detail: json.RawMessage(`{}`)}
	onlyDetail := &contract.Error{Code: contract.CodeAdapterError, Detail: json.RawMessage(`{"adapter_code":"adapter:x:Boom"}`)}
	for name, e := range map[string]error{"Adapter あり": withAdapter, "detail だけ": onlyDetail} {
		d, next := failDetail(e, "files")
		m := keys(d)
		if len(m) != 3 || m["kind"] != "acquire" || m["name"] != "files" || m["adapter_code"] != "adapter:x:Boom" || len(next) != 0 {
			t.Errorf("%s: ADAPTER_ERROR の detail = %s", name, d)
		}
	}
	d, _ := failDetail(errors.New("plain"), "files")
	m := keys(d)
	if v, ok := m["component"]; !ok || v != nil {
		t.Errorf("型の無い誤りの component は null: %s", d)
	}
	if v, ok := m["reason"]; !ok || v != nil {
		t.Errorf("型の無い誤りの reason は null: %s", d)
	}
}

// STORAGE_UNREACHABLE の next は 3.1.3 の一般の行: 同じ操作の再実行と doctor を、この順で列挙する(D-0183)。
func TestStorageUnreachableNextEnumeratesRetryThenDoctor(t *testing.T) {
	a := &Acquire{State: &fake.StateFiles{Items: map[string]json.RawMessage{}}}
	got := a.withNext(t.Context(), TargetOutcome{Status: "failed", ErrorCode: "STORAGE_UNREACHABLE"}).Next
	if len(got) != 2 || !reflect.DeepEqual(got[0].Argv, []string{"sitate", "backup"}) || !reflect.DeepEqual(got[1].Argv, []string{"sitate", "doctor"}) {
		t.Fatalf("next = %+v", got)
	}
	for _, n := range got {
		if n.RequiresHuman {
			t.Errorf("requires_human が true: %+v", n)
		}
	}
}
