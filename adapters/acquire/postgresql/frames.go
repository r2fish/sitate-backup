package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/zeebo/blake3"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// streamOut は sitate.stream/v1 のフレームを書く。end の hash は最初の見出しの先頭から end の見出しの直前までのバイト列の BLAKE3-256(spec 3.2.2)。
type streamOut struct {
	w     *bufio.Writer
	all   *blake3.Hasher
	bytes int64 // data の本体の合計
	err   error
}

func newStreamOut(w io.Writer) *streamOut {
	return &streamOut{w: bufio.NewWriterSize(w, 1<<18), all: blake3.New()}
}

func (s *streamOut) raw(b []byte) {
	if s.err != nil {
		return
	}
	s.all.Write(b)
	_, s.err = s.w.Write(b)
}

func encodeHeader(h contract.StreamHeader) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	err := enc.Encode(h)
	return buf.Bytes(), err
}

func (s *streamOut) header(h contract.StreamHeader) {
	b, err := encodeHeader(h)
	if err != nil && s.err == nil {
		s.err = err
		return
	}
	s.raw(b)
}

// dump は dump フレームの見出しを書く。
func (s *streamOut) dump(format string) {
	s.header(contract.StreamHeader{Type: contract.FrameTypeDump, Format: format})
}

func (s *streamOut) data(b []byte) {
	n := int64(len(b))
	s.header(contract.StreamHeader{Type: contract.FrameTypeData, Size: &n})
	s.raw(b)
	s.bytes += n
}

// itemEnd は dump の終わり。hash は本体の BLAKE3-256、exit_status は受け口の呼び出しの終了状態、status は 0 なら ok、そうでなければ error。
func (s *streamOut) itemEnd(n int64, h *blake3.Hasher, exit int) {
	status := contract.ItemOK
	if exit != 0 {
		status = contract.ItemError
	}
	s.header(contract.StreamHeader{Type: contract.FrameTypeItemEnd, Bytes: &n, Hash: "blake3:" + hex.EncodeToString(h.Sum(nil)), Status: status, ExitStatus: exitJSON(exit)})
}

func exitJSON(n int) json.RawMessage {
	b, _ := json.Marshal(n)
	return b
}

// end は end の見出しを書く(hash は end 自身を含めない)。
func (s *streamOut) end(exit int) error {
	if s.err != nil {
		return s.err
	}
	b := s.bytes
	buf, err := encodeHeader(contract.StreamHeader{Type: contract.FrameTypeEnd, Bytes: &b, Hash: "blake3:" + hex.EncodeToString(s.all.Sum(nil)), ExitStatus: exitJSON(exit)})
	if err != nil {
		return err
	}
	_, err = s.w.Write(buf)
	return err
}

func (s *streamOut) flush() error {
	if s.err != nil {
		return s.err
	}
	return s.w.Flush()
}
