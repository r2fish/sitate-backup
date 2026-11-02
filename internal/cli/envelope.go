package cli

import (
	"encoding/json"
	"io"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// SchemaOut は封筒の schema の値(spec 3.1.1)。
const SchemaOut = "sitate.out/v1"

// outcome は 1 回の実行の終わり。封筒の元になる。
type outcome struct {
	command       []string
	dryRun        bool
	runID         *string
	result        json.RawMessage
	warnings      []contract.Warning
	err           *contract.Error
	phraseEmitted bool
	phrase        *contract.Secret // 専用出力経路へ出す秘密(gate.go が書いて消す)
	secretFD      *int
}

type wireEnvelope struct {
	Schema        string             `json:"schema"`
	Command       []string           `json:"command"`
	OK            bool               `json:"ok"`
	DryRun        bool               `json:"dry_run"`
	RunID         *string            `json:"run_id"`
	Result        json.RawMessage    `json:"result"`
	Warnings      []contract.Warning `json:"warnings"`
	Error         *contract.Error    `json:"error"`
	PhraseEmitted *bool              `json:"phrase_emitted,omitempty"`
}

// render は封筒の 1 行の JSON と終了コードを返す。成功は result を持ち error が null、失敗は result が null(spec 3.1.1)。
func (o outcome) render() ([]byte, int) {
	w := wireEnvelope{Schema: SchemaOut, Command: o.command, DryRun: o.dryRun, RunID: o.runID, Warnings: o.warnings, Error: o.err}
	if w.Warnings == nil {
		w.Warnings = []contract.Warning{}
	}
	for i := range w.Warnings {
		if w.Warnings[i].Next == nil {
			w.Warnings[i].Next = []contract.Next{}
		}
		if len(w.Warnings[i].Detail) == 0 {
			w.Warnings[i].Detail = json.RawMessage("{}")
		}
	}
	code := 0
	if o.err == nil {
		w.OK = true
		w.Result = o.result
		if len(w.Result) == 0 {
			w.Result = json.RawMessage("{}")
		}
	} else {
		w.Result = json.RawMessage("null")
		code = exitOf(o.err)
	}
	if o.phraseEmitted {
		t := true
		w.PhraseEmitted = &t
	}
	b, _ := json.Marshal(w)
	return b, code
}

// writeLine は 1 行を標準出力へ書く。
func writeLine(w io.Writer, b []byte) {
	_, _ = w.Write(append(b, '\n'))
}
