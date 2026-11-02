package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// prevEntry は前回の索引(JSON Lines。spec 3.2.2 の previous_index)の 1 行。
type prevEntry struct {
	Path    *string `json:"path"`
	PathB64 *string `json:"path_b64"`
	Type    string  `json:"type"`
	Size    int64   `json:"size"`
	MtimeNS int64   `json:"mtime_ns"`
	Mode    int     `json:"mode"`
	UID     int64   `json:"uid"`
	GID     int64   `json:"gid"`
	raw     []byte
}

// prevReader は前回の索引を先頭から 1 行ずつ読む(メモリは 1 行分。ファイル数に比例しない)。nil のときは常に空。
type prevReader struct {
	f    *os.File
	r    *bufio.Reader
	cur  *prevEntry
	last []byte
	done bool
	id   string
}

func openPrev(path *string, targetID string) (*prevReader, error) {
	p := &prevReader{id: targetID}
	if path == nil {
		p.done = true
		return p, nil
	}
	f, err := os.Open(*path)
	if err != nil {
		return nil, err
	}
	p.f, p.r = f, bufio.NewReaderSize(f, 1<<18)
	head, err := p.r.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("previous_index の見出しが読めない")
	}
	var h struct {
		Format string `json:"format"`
		Target string `json:"target"`
	}
	if json.Unmarshal(head, &h) != nil || h.Format != "sitate.previdx/v1" || h.Target != targetID {
		return nil, fmt.Errorf("previous_index の見出しが不正")
	}
	return p, nil
}

func (p *prevReader) close() {
	if p.f != nil {
		p.f.Close()
	}
}

// advance は次の行へ進める。終端なら done が真になる。
func (p *prevReader) advance() error {
	if p.done {
		return nil
	}
	line, err := p.r.ReadBytes('\n')
	if err == io.EOF && len(line) == 0 {
		p.done, p.cur = true, nil
		return nil
	}
	if err != nil && err != io.EOF {
		return err
	}
	var e prevEntry
	if json.Unmarshal(line, &e) != nil || (e.Path == nil) == (e.PathB64 == nil) {
		return fmt.Errorf("previous_index の行が不正")
	}
	if e.Path != nil {
		e.raw = []byte(*e.Path)
	} else {
		b, derr := base64.StdEncoding.DecodeString(*e.PathB64)
		if derr != nil {
			return fmt.Errorf("previous_index の path_b64 が不正")
		}
		e.raw = b
	}
	if p.last != nil && pathCmp(p.last, e.raw) >= 0 {
		return fmt.Errorf("previous_index が並び順に反する")
	}
	p.last, p.cur = e.raw, &e
	return nil
}

// seek は path 以上の最初の行まで進め、path と等しい行があればそれを返す(無ければ nil)。path は昇順に呼ぶ。
func (p *prevReader) seek(path []byte) (*prevEntry, error) {
	if p.cur == nil && !p.done {
		if err := p.advance(); err != nil {
			return nil, err
		}
	}
	for !p.done && pathCmp(p.cur.raw, path) < 0 {
		if err := p.advance(); err != nil {
			return nil, err
		}
	}
	if !p.done && pathCmp(p.cur.raw, path) == 0 {
		return p.cur, nil
	}
	return nil, nil
}

// unchangedFile は変化の判定(spec 3.2.2): type が file で、size・mtime_ns・mode・uid・gid が全部等しければ変化なし。
func (e *prevEntry) unchangedFile(r listRec) bool {
	return e != nil && e.Type == "file" && e.Size == r.size && e.MtimeNS == r.mtimeNS && e.Mode == r.mode && e.UID == r.uid && e.GID == r.gid
}
