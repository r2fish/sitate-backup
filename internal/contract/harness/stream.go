package harness

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/r2fish/sitate-backup/internal/contract"
	"io"

	"github.com/zeebo/blake3"
)

func streamErr(kind, reason, msg string) *contract.StreamError {
	return &contract.StreamError{Kind: kind, Reason: reason, Msg: msg}
}

// StreamPathBytes は見出しの path / path_b64 の生のバイト列を返す。
func StreamPathBytes(h contract.StreamHeader) ([]byte, bool) {
	if h.Path != nil {
		return []byte(*h.Path), true
	}
	if h.PathB64 != nil {
		b, err := base64.StdEncoding.DecodeString(*h.PathB64)
		return b, err == nil
	}
	return nil, false
}

// StreamPathOK は取得アダプタのパスの契約(spec 3.2.2): NUL・空の要素・.・..(根の "." を除く)を含まない。
func StreamPathOK(p []byte) bool {
	if string(p) == "." {
		return true
	}
	if len(p) == 0 || bytes.IndexByte(p, 0) >= 0 {
		return false
	}
	for _, c := range bytes.Split(p, []byte("/")) {
		if len(c) == 0 || string(c) == "." || string(c) == ".." {
			return false
		}
	}
	return true
}

type streamState struct {
	open      string // 開いている項目の type(file / file-retry / dump)。無ければ ""
	itemHash  *blake3.Hasher
	itemBytes int64
	files     map[string]bool // 先に送った file の path
	retried   map[string]bool
	dumps     map[string]bool
	total     int64
}

// ReadStream は sitate.stream/v1 を end の見出しまで読み、各フレームを onFrame へ渡す(onFrame は nil 可)。
// end の hash は最初の見出しの先頭から end の見出しの直前までのバイト列の BLAKE3-256。読み終えた時点で br は
// end の見出しの改行の直後にあり、続く最終行の結果 JSON は呼び出し側が読む。違反は *contract.StreamError。
func ReadStream(br *bufio.Reader, onFrame func(contract.Frame) error) (contract.StreamHeader, error) {
	all := blake3.New()
	st := &streamState{files: map[string]bool{}, retried: map[string]bool{}, dumps: map[string]bool{}}
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return contract.StreamHeader{}, streamErr("no_end", "truncated", "end の前にストリームが終わった")
		}
		raw := line[:len(line)-1]
		h, serr := decodeHeader(raw)
		if serr != nil {
			return h, serr
		}
		if h.Type == contract.FrameTypeEnd {
			if e := endCheck(h, st, all); e != nil {
				return h, e
			}
			return h, nil
		}
		all.Write(line)
		var body []byte
		if h.Type == contract.FrameTypeData {
			if body, serr = readBody(br, h, st); serr != nil {
				return h, serr
			}
			all.Write(body)
			st.total += int64(len(body))
		}
		if serr = st.step(h, body); serr != nil {
			return h, serr
		}
		if onFrame != nil {
			if err := onFrame(contract.Frame{Type: h.Type, Meta: json.RawMessage(raw), Data: body}); err != nil {
				return h, err
			}
		}
	}
}

func decodeHeader(raw []byte) (contract.StreamHeader, *contract.StreamError) {
	var h contract.StreamHeader
	if err := contract.Validate("adapter/stream.json#/$defs/frame", raw); err != nil {
		return h, streamErr("contract", "header", err.Error())
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil {
		return h, streamErr("contract", "header", err.Error())
	}
	if p, ok := StreamPathBytes(h); (h.Path != nil || h.PathB64 != nil) && (!ok || !StreamPathOK(p)) {
		return h, streamErr("contract", "path", "パスが契約に合わない")
	}
	return h, nil
}

func readBody(br *bufio.Reader, h contract.StreamHeader, st *streamState) ([]byte, *contract.StreamError) {
	if st.open == "" {
		return nil, streamErr("contract", "order", "項目の外の data")
	}
	if *h.Size > contract.StreamFragmentMax {
		return nil, streamErr("contract", "size", "断片が最大長を超える")
	}
	body := make([]byte, *h.Size)
	if _, err := io.ReadFull(br, body); err != nil {
		return nil, streamErr("no_end", "truncated", "data の本体の途中でストリームが終わった")
	}
	return body, nil
}

func (st *streamState) step(h contract.StreamHeader, body []byte) *contract.StreamError {
	switch h.Type {
	case contract.FrameTypeData:
		st.itemHash.Write(body)
		st.itemBytes += int64(len(body))
		return nil
	case contract.FrameTypeItemEnd:
		return st.closeItem(h)
	}
	if st.open != "" {
		return streamErr("contract", "order", h.Type+" が項目の途中に現れた")
	}
	p, _ := StreamPathBytes(h)
	switch h.Type {
	case contract.FrameTypeFile:
		st.files[string(p)] = true
	case contract.FrameTypeRetry:
		if !st.files[string(p)] || st.retried[string(p)] {
			return streamErr("contract", "order", "file-retry の前の file が無いか 2 回目")
		}
		st.retried[string(p)] = true
	case contract.FrameTypeDump:
		if st.dumps[h.Format] || (h.Format == contract.DumpFormatCustom) != st.dumps[contract.DumpFormatGlobals] {
			return streamErr("contract", "order", "dump は globals の後に custom を 1 回ずつ")
		}
		st.dumps[h.Format] = true
	default:
		return nil
	}
	st.open, st.itemHash, st.itemBytes = h.Type, blake3.New(), 0
	return nil
}

func (st *streamState) closeItem(h contract.StreamHeader) *contract.StreamError {
	if st.open == "" {
		return streamErr("contract", "order", "開いた項目が無い item_end")
	}
	if *h.Bytes != st.itemBytes || h.Hash != "blake3:"+hex.EncodeToString(st.itemHash.Sum(nil)) {
		return streamErr("contract", "item", "item_end の bytes か hash が本体と違う")
	}
	if hasExit := string(h.ExitStatus) != "null"; (st.open == contract.FrameTypeDump) != hasExit {
		return streamErr("contract", "item", "exit_status は dump だけが持つ")
	}
	if h.Status == contract.ItemChanged && st.open != contract.FrameTypeRetry {
		return streamErr("contract", "item", "changed は file-retry だけ")
	}
	st.open = ""
	return nil
}

func endCheck(h contract.StreamHeader, st *streamState, all *blake3.Hasher) *contract.StreamError {
	if st.open != "" {
		return streamErr("contract", "order", "item_end の前に end")
	}
	if *h.Bytes != st.total {
		return streamErr("end_mismatch", "bytes", "end の bytes が data の合計と違う")
	}
	if h.Hash != "blake3:"+hex.EncodeToString(all.Sum(nil)) {
		return streamErr("end_mismatch", "hash", "end の hash が違う")
	}
	return nil
}

// Blake3Tag は "blake3:" と BLAKE3-256 の小文字 16 進 64 桁(エントリ・フレームの hash の形。spec 4.4)。
func Blake3Tag(b []byte) string {
	s := blake3.Sum256(b)
	return "blake3:" + hex.EncodeToString(s[:])
}

// WriteFrame は見出し 1 行(JSON)と本体を書く(試験ベクトルの検証用)。見出しは encoding/json の出力のまま。
func WriteFrame(w io.Writer, h contract.StreamHeader, body []byte) error {
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return err
	}
	if len(body) > 0 {
		_, err = w.Write(body)
	}
	return err
}
