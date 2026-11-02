package adapter

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 標準出力の読み取り(spec 3.2.1「出力」、3.2.2)。

// outStatus は標準出力の読み取りの結果。
type outStatus int

const (
	outOK    outStatus = iota // 結果の行を 1 行だけ受けた(改行で終わり、その後に何も無い)
	outNone                   // 結果の行の前に出力が終わった(途中で切れた本体を含む)
	outBad                    // 契約違反(長すぎる行、改行で終わらない、後ろに続きがある、見出しの不正、取得以外のフレーム)
	outCbErr                  // onFrame がエラーを返した
)

var errLineTooLong = errors.New("line too long")

// readLine は改行までを読む(改行は含めない)。max を超えれば errLineTooLong。term は改行で終わったか。
func readLine(br *bufio.Reader, max int) (line []byte, term bool, err error) {
	for {
		chunk, rerr := br.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > max+1 {
			return nil, false, errLineTooLong
		}
		if rerr == nil {
			return line[:len(line)-1], true, nil
		}
		if rerr != bufio.ErrBufferFull {
			return line, false, rerr
		}
	}
}

// readOutput は取得アダプタではフレームを onFrame へ渡しながら(背圧: onFrame が返るまで次を読まない)、
// 結果の行を読む。ほかの kind はフレームを認めない。結果の行(改行を除く)を返す。
func readOutput(kind string, br *bufio.Reader, onFrame func(contract.Frame) error) ([]byte, outStatus, error) {
	for {
		line, term, err := readLine(br, contract.AdapterResultMax)
		if errors.Is(err, errLineTooLong) {
			return nil, outBad, nil
		}
		if !term {
			return nil, outNone, nil // 出力の終わり、または途中で切れた行。終了コードで読み替える
		}
		var probe map[string]json.RawMessage
		if json.Unmarshal(line, &probe) != nil {
			return nil, outBad, nil
		}
		if _, isFrame := probe["type"]; !isFrame {
			if rest, _ := br.Peek(1); len(rest) > 0 {
				return nil, outBad, nil
			}
			return line, outOK, nil
		}
		if kind != "acquire" {
			return nil, outBad, nil
		}
		if st, cerr := passFrame(br, line, onFrame); st != outOK {
			return nil, st, cerr
		}
	}
}

// passFrame は見出し 1 行(と data の本体)を onFrame へ渡す。Meta は見出しの生の JSON(改行なし)。
func passFrame(br *bufio.Reader, hdr []byte, onFrame func(contract.Frame) error) (outStatus, error) {
	if contract.Validate("adapter/stream.json#/$defs/frame", hdr) != nil {
		return outBad, nil
	}
	var h contract.StreamHeader
	if json.Unmarshal(hdr, &h) != nil {
		return outBad, nil
	}
	if !headerPathsOK(h) {
		return outBad, nil
	}
	f := contract.Frame{Type: h.Type, Meta: append(json.RawMessage(nil), hdr...)}
	if h.Type == contract.FrameTypeData {
		if h.Size == nil || *h.Size < 1 || *h.Size > contract.StreamFragmentMax {
			return outBad, nil
		}
		f.Data = make([]byte, *h.Size)
		if _, err := io.ReadFull(br, f.Data); err != nil {
			return outNone, nil
		}
	}
	if onFrame != nil {
		if err := onFrame(f); err != nil {
			return outCbErr, err
		}
	}
	return outOK, nil
}

// headerPathsOK は見出しのパスの規則(spec 3.2.2): NUL・空の要素・.・.. の要素(根の "." を除く)を含まない。
func headerPathsOK(h contract.StreamHeader) bool {
	if h.Path != nil && !pathOK(*h.Path) {
		return false
	}
	if h.PathB64 != nil {
		raw, err := base64.StdEncoding.DecodeString(*h.PathB64)
		if err != nil || !pathOK(string(raw)) {
			return false
		}
	}
	return true
}

func pathOK(p string) bool {
	if p == "." {
		return true
	}
	if p == "" || strings.ContainsRune(p, 0) {
		return false
	}
	for _, c := range strings.Split(p, "/") {
		if c == "" || c == "." || c == ".." {
			return false
		}
	}
	return true
}
