package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

var testWarn = []contract.Warning{{Code: contract.CodeMlockFailed, Detail: json.RawMessage(`{"reason":"enomem"}`), Next: []contract.Next{}}}

func TestWarnWriterInjectsOnlyIntoEnvelope(t *testing.T) {
	var buf bytes.Buffer
	w := newWarnWriter(&buf, testWarn)
	// gate の先頭の秘密のレコード(封筒でない)は素通し、次の封筒に足す。
	_, _ = w.Write([]byte("{\"schema\":\"sitate.secret/v1\",\"x\":1}\n"))
	_, _ = w.Write([]byte(`{"schema":"sitate.out/v1","warnings":[{"code":"X","detail":{},"next":[]}],"result":{"warnings":5}}` + "\n"))
	_, _ = w.Write([]byte("BODY{not json\n"))
	w.Flush()
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	if string(lines[0]) != `{"schema":"sitate.secret/v1","x":1}` || string(lines[2]) != "BODY{not json" {
		t.Fatalf("素通しが崩れた: %q", buf.String())
	}
	var env struct {
		Warnings []struct{ Code string }
		Result   map[string]int
	}
	if err := json.Unmarshal(lines[1], &env); err != nil || len(env.Warnings) != 2 || env.Warnings[0].Code != "MLOCK_FAILED" || env.Warnings[1].Code != "X" || env.Result["warnings"] != 5 {
		t.Fatalf("封筒: %s (%v)", lines[1], err)
	}
}

func TestWarnWriterPassesNonJSONAndNoWarnings(t *testing.T) {
	var a, b bytes.Buffer
	_, _ = newWarnWriter(&a, testWarn).Write([]byte("x"))
	_, _ = newWarnWriter(&b, nil).Write([]byte(`{"schema":"sitate.out/v1","warnings":[]}` + "\n"))
	if a.String() != "x" || b.String() != `{"schema":"sitate.out/v1","warnings":[]}`+"\n" {
		t.Fatalf("a=%q b=%q", a.String(), b.String())
	}
}
