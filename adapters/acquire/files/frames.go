package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"unicode/utf8"

	"github.com/zeebo/blake3"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// streamOut は sitate.stream/v1 のフレームを書く。end の hash は最初の見出しの先頭から end の見出しの直前までのバイト列の BLAKE3-256(spec 3.2.2)。
type streamOut struct {
	w     *bufio.Writer
	all   *blake3.Hasher
	bytes int64 // data の本体の合計
	err   error
	open  bool // item_end を待っている項目がある
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

// header は見出し 1 行(JSON と改行)を書く。HTML 用のエスケープはしない。
func (s *streamOut) header(h contract.StreamHeader) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(h); err != nil && s.err == nil {
		s.err = err
		return
	}
	s.raw(buf.Bytes())
}

func (s *streamOut) data(b []byte) {
	n := int64(len(b))
	s.header(contract.StreamHeader{Type: contract.FrameTypeData, Size: &n})
	s.raw(b)
	s.bytes += n
}

// end は end の見出しを書き、hash を数え終えた値で出す(end 自身は hash の対象に入れない)。
func (s *streamOut) end(exit int) error {
	if s.err != nil {
		return s.err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	b := s.bytes
	ex := json.RawMessage(itoa(exit))
	_ = enc.Encode(contract.StreamHeader{Type: contract.FrameTypeEnd, Bytes: &b, Hash: "blake3:" + hex.EncodeToString(s.all.Sum(nil)), ExitStatus: ex})
	if _, err := s.w.Write(buf.Bytes()); err != nil {
		return err
	}
	return nil
}

func (s *streamOut) flush() error {
	if s.err != nil {
		return s.err
	}
	return s.w.Flush()
}

// pathFields は生のバイト列を path か path_b64 のどちらか 1 つに置く(UTF-8 として正しければ path)。
func pathFields(p []byte) (path, b64 *string) {
	if utf8.Valid(p) {
		s := string(p)
		return &s, nil
	}
	e := base64.StdEncoding.EncodeToString(p)
	return nil, &e
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func i64(v int64) *int64 { return &v }

// dir・symlink・fileLike は list のレコードから見出しを作って書く(値は list の値を写す。spec 3.2.2 のフレームの規則)。
func (s *streamOut) dir(r listRec) {
	p, b := pathFields(r.path)
	m := r.mode
	s.header(contract.StreamHeader{Type: contract.FrameTypeDir, Path: p, PathB64: b, Mode: &m, UID: i64(r.uid), GID: i64(r.gid), MtimeNS: i64(r.mtimeNS)})
}

func (s *streamOut) symlink(r listRec) {
	p, b := pathFields(r.path)
	t, tb := pathFields(r.target)
	s.header(contract.StreamHeader{Type: contract.FrameTypeSymlink, Path: p, PathB64: b, Target: t, TargetB64: tb, UID: i64(r.uid), GID: i64(r.gid), MtimeNS: i64(r.mtimeNS)})
}

// fileLike は file・file-retry・unchanged の見出し(同じ項目・同じ値)を書く。file と file-retry は item_end を待つ。
func (s *streamOut) fileLike(typ string, r listRec) {
	p, b := pathFields(r.path)
	m := r.mode
	s.header(contract.StreamHeader{Type: typ, Path: p, PathB64: b, Size: i64(r.size), Mode: &m, UID: i64(r.uid), GID: i64(r.gid), MtimeNS: i64(r.mtimeNS)})
	if typ != contract.FrameTypeUnchg {
		s.open = true
	}
}

// itemEnd は開いている項目を閉じる。hash は本体のハッシュ(空の本体も BLAKE3 のハッシュ)。
func (s *streamOut) itemEnd(n int64, hash, status string, nullExit bool) {
	if hash == "" {
		hash = "blake3:" + hex.EncodeToString(blake3.New().Sum(nil))
	}
	h := contract.StreamHeader{Type: contract.FrameTypeItemEnd, Bytes: i64(n), Hash: hash, Status: status}
	if nullExit {
		h.ExitStatus = json.RawMessage("null")
	}
	s.header(h)
	s.open = false
}
