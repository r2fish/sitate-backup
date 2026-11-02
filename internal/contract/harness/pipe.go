package harness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func pipeLine(br *bufio.Reader, def string) ([]byte, error) {
	line, err := br.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("pipe: 行の途中で閉じた")
	}
	return line, contract.Validate("receiver/pipe.json#/$defs/"+def, line)
}

// ReadPipeRequest は補助コマンド→核の方向を読み、要求と入力の本体(in の本体の連結)を返す。
func ReadPipeRequest(br *bufio.Reader) (contract.PipeRequest, []byte, error) {
	var rq contract.PipeRequest
	line, err := pipeLine(br, "request")
	if err != nil {
		return rq, nil, err
	}
	_ = json.Unmarshal(line, &rq)
	var in []byte
	for {
		line, err := pipeLine(br, "in")
		if err != nil {
			return rq, nil, err
		}
		var f struct{ In int }
		_ = json.Unmarshal(line, &f)
		if f.In == 0 {
			return rq, in, nil
		}
		b := make([]byte, f.In)
		if _, err := io.ReadFull(br, b); err != nil {
			return rq, nil, fmt.Errorf("pipe: in の本体の途中で閉じた")
		}
		in = append(in, b...)
	}
}

// ReadPipeReply は核→補助コマンドの方向を読み、出力の本体の連結と終了状態を返す。exit が無いまま閉じたときは
// 終了状態 contract.RecvExitUnreachable とエラーを返す。
func ReadPipeReply(br *bufio.Reader) ([]byte, int, error) {
	var out []byte
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return out, contract.RecvExitUnreachable, fmt.Errorf("pipe: exit の前に閉じた")
		}
		if contract.Validate("receiver/pipe.json#/$defs/exit", line) == nil {
			var f struct{ Exit int }
			_ = json.Unmarshal(line, &f)
			return out, f.Exit, nil
		}
		if err := contract.Validate("receiver/pipe.json#/$defs/out", line); err != nil {
			return out, contract.RecvExitUnreachable, err
		}
		var f struct{ Out int }
		_ = json.Unmarshal(line, &f)
		b := make([]byte, f.Out)
		if _, err := io.ReadFull(br, b); err != nil {
			return out, contract.RecvExitUnreachable, fmt.Errorf("pipe: out の本体の途中で閉じた")
		}
		out = append(out, b...)
	}
}
