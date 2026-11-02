package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type relayEnd struct {
	Kind    string `json:"kind"`
	Objects int64  `json:"objects"`
	Bytes   int64  `json:"bytes"`
}

// WriteRelay は start・全オブジェクト・end を書く(bodies[i] は objs[i] の本体で、len が Size と一致すること)。
func WriteRelay(w io.Writer, objs []contract.RelayObject, bodies [][]byte) error {
	if _, err := fmt.Fprintf(w, "{\"format\":%q}\n", contract.RelayFormat); err != nil {
		return err
	}
	var total int64
	for i, o := range objs {
		if int64(len(bodies[i])) != o.Size {
			return fmt.Errorf("relay: %s の大きさが見出しと違う", o.Key)
		}
		b, _ := json.Marshal(o)
		if _, err := w.Write(append(b, '\n')); err != nil {
			return err
		}
		if _, err := w.Write(bodies[i]); err != nil {
			return err
		}
		total += o.Size
	}
	b, _ := json.Marshal(relayEnd{"end", int64(len(objs)), total})
	_, err := w.Write(append(b, '\n'))
	return err
}

// ReadRelay は中継を end まで読み、オブジェクトごとに見出しと本体(Size バイト)を fn へ渡す。
// end の無いまま閉じたとき・本体が足りないとき・件数か合計が end と違うときはエラー(取り消し)。
func ReadRelay(br *bufio.Reader, fn func(contract.RelayObject, []byte) error) error {
	line, err := br.ReadBytes('\n')
	if err != nil || contract.Validate("data/relay.json#/$defs/start", line) != nil {
		return fmt.Errorf("relay: 1 行目が start でない")
	}
	var n, total int64
	for {
		line, err = br.ReadBytes('\n')
		if err != nil {
			return fmt.Errorf("relay: end の前に閉じた(取り消し)")
		}
		if contract.Validate("data/relay.json#/$defs/end", line) == nil {
			var e relayEnd
			_ = json.Unmarshal(line, &e)
			if e.Objects != n || e.Bytes != total {
				return fmt.Errorf("relay: end の件数か合計が違う")
			}
			return nil
		}
		if err := contract.Validate("data/relay.json#/$defs/object", line); err != nil {
			return fmt.Errorf("relay: 見出しが不正: %w", err)
		}
		var o contract.RelayObject
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&o); err != nil {
			return err
		}
		body := make([]byte, o.Size)
		if _, err := io.ReadFull(br, body); err != nil {
			return fmt.Errorf("relay: 本体の途中で閉じた(取り消し)")
		}
		n, total = n+1, total+o.Size
		if err := fn(o, body); err != nil {
			return err
		}
	}
}
