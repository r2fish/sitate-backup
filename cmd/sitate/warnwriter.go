package main

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// envelopeSchema は封筒の schema の値(spec 3.1.1)。
const envelopeSchema = "sitate.out/v1"

// warnWriter は起動の始めに出た警告(MLOCK_FAILED)を、最初の封筒の warnings[] に足す。
// 先頭が '{' の行だけを見る。封筒の後(restore fetch の本体を含む)と、'{' で始まらない出力は素通しにする。
type warnWriter struct {
	w     io.Writer
	warns []contract.Warning
	buf   []byte
	done  bool
}

func newWarnWriter(w io.Writer, warns []contract.Warning) *warnWriter {
	return &warnWriter{w: w, warns: warns, done: len(warns) == 0}
}

func (x *warnWriter) Write(p []byte) (int, error) {
	if x.done || len(p) == 0 {
		return x.w.Write(p)
	}
	if len(x.buf) == 0 && p[0] != '{' {
		x.done = true
		return x.w.Write(p)
	}
	x.buf = append(x.buf, p...)
	for !x.done {
		i := bytes.IndexByte(x.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := x.buf[:i]
		if out, ok := injectWarnings(line, x.warns); ok {
			line = out
			x.done = true
		}
		if _, err := x.w.Write(append(append([]byte{}, line...), '\n')); err != nil {
			return 0, err
		}
		x.buf = x.buf[i+1:]
	}
	rest := x.buf
	x.buf = nil
	if len(rest) > 0 {
		if _, err := x.w.Write(rest); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Flush は溜めた端数を出す(終了の直前)。
func (x *warnWriter) Flush() {
	if len(x.buf) > 0 {
		_, _ = x.w.Write(x.buf)
		x.buf = nil
	}
}

// injectWarnings は封筒の 1 行の warnings[] の先頭に警告を足す。封筒でなければ ok=false。
func injectWarnings(line []byte, warns []contract.Warning) ([]byte, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(line, &m) != nil {
		return nil, false
	}
	var schema string
	if json.Unmarshal(m["schema"], &schema) != nil || schema != envelopeSchema {
		return nil, false
	}
	var cur []json.RawMessage
	_ = json.Unmarshal(m["warnings"], &cur)
	all := make([]json.RawMessage, 0, len(warns)+len(cur))
	for _, w := range warns {
		b, _ := json.Marshal(w)
		all = append(all, b)
	}
	all = append(all, cur...)
	m["warnings"], _ = json.Marshal(all)
	out, err := json.Marshal(m)
	return out, err == nil
}
