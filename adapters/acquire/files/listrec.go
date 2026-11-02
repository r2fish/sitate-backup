package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// listRec は受け口の `list` の 1 レコード(spec 3.3 の 9 欄)。
type listRec struct {
	typ      byte // f / d / l
	readable bool
	size     int64
	mode     int
	uid, gid int64
	mtimeNS  int64
	path     []byte
	target   []byte
}

// listReader は NUL 区切りのレコード列を 1 つずつ読む(メモリはレコード 1 つ分)。
type listReader struct {
	r *bufio.Reader
}

func newListReader(r io.Reader) *listReader { return &listReader{r: bufio.NewReaderSize(r, 1<<16)} }

func (l *listReader) field() ([]byte, error) {
	b, err := l.r.ReadSlice(0)
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			return nil, fmt.Errorf("欄が長すぎる")
		}
		return nil, err
	}
	return b[:len(b)-1], nil
}

// next は次のレコードを返す。終端なら io.EOF、途中で切れたら io.ErrUnexpectedEOF。
func (l *listReader) next() (listRec, error) {
	var f [9][]byte
	for i := range f {
		b, err := l.field()
		if err != nil {
			if i == 0 && err == io.EOF {
				return listRec{}, io.EOF
			}
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return listRec{}, err
		}
		f[i] = append([]byte(nil), b...)
	}
	return parseListRec(f)
}

func parseListRec(f [9][]byte) (listRec, error) {
	var r listRec
	if len(f[0]) != 1 || (f[0][0] != 'f' && f[0][0] != 'd' && f[0][0] != 'l') {
		return r, fmt.Errorf("type が不正")
	}
	r.typ = f[0][0]
	switch string(f[1]) {
	case "1":
		r.readable = true
	case "0":
	default:
		return r, fmt.Errorf("readable が不正")
	}
	var err error
	if r.size, err = strconv.ParseInt(string(f[2]), 10, 64); err != nil {
		return r, fmt.Errorf("size が不正")
	}
	m, err := strconv.ParseInt(string(f[3]), 8, 32)
	if err != nil || m < 0 || m > 07777 {
		return r, fmt.Errorf("mode が不正")
	}
	r.mode = int(m)
	if r.uid, err = strconv.ParseInt(string(f[4]), 10, 64); err != nil {
		return r, fmt.Errorf("uid が不正")
	}
	if r.gid, err = strconv.ParseInt(string(f[5]), 10, 64); err != nil {
		return r, fmt.Errorf("gid が不正")
	}
	if r.mtimeNS, err = parseMtime(f[6]); err != nil {
		return r, err
	}
	if len(f[7]) == 0 {
		return r, fmt.Errorf("path が空")
	}
	r.path, r.target = f[7], f[8]
	return r, nil
}

// parseMtime は `<秒>.<ナノ秒 9 桁>`(1970 年より前は先頭に -)をナノ秒の整数にする。
func parseMtime(b []byte) (int64, error) {
	s := string(b)
	neg := false
	if len(s) > 0 && s[0] == '-' {
		neg, s = true, s[1:]
	}
	i := bytes.IndexByte([]byte(s), '.')
	if i <= 0 || len(s)-i-1 != 9 {
		return 0, fmt.Errorf("mtime が不正")
	}
	sec, err1 := strconv.ParseInt(s[:i], 10, 64)
	ns, err2 := strconv.ParseInt(s[i+1:], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, fmt.Errorf("mtime が不正")
	}
	v := sec*1_000_000_000 + ns
	if neg {
		v = -v
	}
	return v, nil
}

// encode は listRec を受け口の形(9 欄を NUL で終える)に戻す(作業用ファイルへの退避に使う)。
func (r listRec) encode() []byte {
	var b bytes.Buffer
	mt := r.mtimeNS
	sign := ""
	if mt < 0 {
		sign, mt = "-", -mt
	}
	fmt.Fprintf(&b, "%c\x00%s\x00%d\x00%o\x00%d\x00%d\x00%s%d.%09d\x00", r.typ, map[bool]string{true: "1", false: "0"}[r.readable],
		r.size, r.mode, r.uid, r.gid, sign, mt/1_000_000_000, mt%1_000_000_000)
	b.Write(r.path)
	b.WriteByte(0)
	b.Write(r.target)
	b.WriteByte(0)
	return b.Bytes()
}

// pathLess は受け口の `list` と同じ順(根 "." が最初、残りはバイト順)で a < b を返す。
func pathCmp(a, b []byte) int {
	ar, br := len(a) == 1 && a[0] == '.', len(b) == 1 && b[0] == '.'
	switch {
	case ar && br:
		return 0
	case ar:
		return -1
	case br:
		return 1
	}
	return bytes.Compare(a, b)
}
