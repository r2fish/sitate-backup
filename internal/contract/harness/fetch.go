package harness

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// ReadFetch は restore fetch の標準出力(sitate.fetch/v1。spec 3.1.4)を読む。形(spec が U0 に委ねた断片の形式。U0 が決めた値):
// 封筒の JSON 1 行(改行で終わる)に続けて、断片を並べる。断片 = 長さ(4 バイトのビッグエンディアンの符号なし整数)∥ その長さの本体。
// 長さ 0 の断片が終わりの印で、その後ろには何も続かない。断片の最大長は FetchFragmentMax で、超える断片は拒否する。
// 戻り値は封筒の行(改行なし)と、つなげた本体(tar ストリーム)。
func ReadFetch(r io.Reader) (envelope, body []byte, err error) {
	br := bufio.NewReader(r)
	line, err := br.ReadBytes('\n')
	if err != nil {
		return nil, nil, errors.New("封筒の行が改行で終わらない")
	}
	envelope = bytes.TrimRight(line, "\n")
	var probe map[string]any
	if json.Unmarshal(envelope, &probe) != nil {
		return nil, nil, errors.New("封筒の行が JSON のオブジェクトでない")
	}
	for {
		var n [4]byte
		if _, err := io.ReadFull(br, n[:]); err != nil {
			return nil, nil, errors.New("断片の長さが読めない(終わりの印が無い)")
		}
		size := binary.BigEndian.Uint32(n[:])
		if size > contract.FetchFragmentMax {
			return nil, nil, fmt.Errorf("断片の長さ %d が最大長を超える", size)
		}
		if size == 0 {
			if _, err := br.ReadByte(); err != io.EOF {
				return nil, nil, errors.New("終わりの印の後ろに続きがある")
			}
			return envelope, body, nil
		}
		frag := make([]byte, size)
		if _, err := io.ReadFull(br, frag); err != nil {
			return nil, nil, errors.New("断片の本体が途中で切れた")
		}
		body = append(body, frag...)
	}
}
