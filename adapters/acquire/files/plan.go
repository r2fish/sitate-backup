package main

import (
	"bufio"
	"io"
	"os"
	"path/filepath"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// acquire は 1 対象の取得の全体: list(1 回)→ 変化の判定と dir・symlink・unchanged のフレーム → read-batch(束ごと)→ 読み直し → end と結果。
func acquire(out io.Writer, recv string, req contract.AcquireRequest) int {
	st := newStreamOut(out)
	o := &outcome{}
	prev, err := openPrev(req.PreviousIndex, req.Target.ID)
	if err != nil {
		return failJSON(out, "INTERNAL", map[string]any{"component": "adapter", "reason": "request"})
	}
	defer prev.close()
	dir := ""
	if req.PreviousIndex != nil {
		dir = filepath.Dir(*req.PreviousIndex)
	}
	spool, err := newSpool(dir)
	if err != nil {
		return failJSON(out, "INTERNAL", map[string]any{"component": "adapter", "reason": "request"})
	}
	defer spool.remove()

	cls, code := planList(st, o, prev, spool, recv, req.Target.ID)
	if cls == classOK {
		cls, code = readBodies(st, o, spool, recv, req.Target.ID)
	}
	return finish(out, st, o, cls, code, req.Target.ID)
}

// finish は分類に応じて end と結果(終了 0)か error(終了 1)を出す。
func finish(out io.Writer, st *streamOut, o *outcome, cls exitClass, code, id string) int {
	switch cls {
	case classMismatch:
		_ = st.flush()
		return failJSON(out, "RECEIVER_TARGETS_MISMATCH", map[string]any{"target_ids": []string{id}})
	case classUnreachable:
		_ = st.flush()
		return failJSON(out, "STORAGE_UNREACHABLE", map[string]any{"target": "source", "reason": "connect"})
	case classBadOutput:
		_ = st.flush()
		return failJSON(out, "INTERNAL", map[string]any{"component": "adapter", "reason": "output"})
	case classFail:
		o.failed = true
		o.addError(code, nil)
	}
	if st.open {
		st.itemEnd(0, "", "error", true)
	}
	if err := st.end(o.lastExit); err != nil {
		return failJSON(out, "INTERNAL", map[string]any{"component": "adapter", "reason": "output"})
	}
	if _, err := st.w.Write(o.resultJSON()); err != nil {
		return 1
	}
	if st.flush() != nil {
		return 1
	}
	return 0
}

const classBadOutput exitClass = 100 // 受け口の出力が契約に反する(INTERNAL の output)

// planList は受け口の list を読み、変化の判定と dir・symlink・unchanged のフレームを出し、変化したファイルを spool へ退避する。
func planList(st *streamOut, o *outcome, prev *prevReader, spool *spool, recv, id string) (exitClass, string) {
	rc, err := startRecv(recv, "list", id, false)
	if err != nil {
		o.lastExit = exitUnreachable
		return classUnreachable, ""
	}
	lr := newListReader(rc.stdout)
	var last []byte
	broken := false
	for {
		r, err := lr.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			broken = true
			break
		}
		if (last == nil && !(len(r.path) == 1 && r.path[0] == '.' && r.typ == 'd')) || (last != nil && pathCmp(last, r.path) >= 0) {
			rc.kill()
			return classBadOutput, ""
		}
		last = r.path
		if e := handleRec(st, o, prev, spool, r); e != nil {
			rc.kill()
			return classBadOutput, ""
		}
	}
	if broken { // 途中で切れた出力: 終了状態で分類する(255 は届かなかった、126 は拒否)
		io.Copy(io.Discard, rc.stdout)
		status := rc.wait()
		o.lastExit = status
		if cls, code := classify("list", status); cls != classOK {
			return cls, code
		}
		return classBadOutput, ""
	}
	status := rc.wait()
	o.lastExit = status
	cls, code := classify("list", status)
	return cls, code
}

func handleRec(st *streamOut, o *outcome, prev *prevReader, spool *spool, r listRec) error {
	switch r.typ {
	case 'd':
		st.dir(r)
		if !r.readable {
			o.addError("PERMISSION", r.path)
		}
	case 'l':
		st.symlink(r)
	case 'f':
		if !r.readable {
			o.addError("PERMISSION", r.path)
			return nil
		}
		o.files++
		pe, err := prev.seek(r.path)
		if err != nil {
			return err
		}
		if pe.unchangedFile(r) {
			st.fileLike(contract.FrameTypeUnchg, r)
			return nil
		}
		return spool.add(r)
	}
	return nil
}

// spool は変化したファイルの list レコードを退避する作業用ファイル(メモリをファイル数に比例させない)。
type spool struct {
	f *os.File
	w *bufio.Writer
	n int64
}

func newSpool(dir string) (*spool, error) {
	if dir != "" {
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			dir = ""
		}
	}
	f, err := os.CreateTemp(dir, "sitate-files-spool-")
	if err != nil {
		f, err = os.CreateTemp("", "sitate-files-spool-")
		if err != nil {
			return nil, err
		}
	}
	return &spool{f: f, w: bufio.NewWriterSize(f, 1<<18)}, nil
}

func (s *spool) add(r listRec) error {
	s.n++
	_, err := s.w.Write(r.encode())
	return err
}

// reader は書いた内容を先頭から読む。
func (s *spool) reader() (*listReader, error) {
	if err := s.w.Flush(); err != nil {
		return nil, err
	}
	if _, err := s.f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return newListReader(s.f), nil
}

func (s *spool) remove() {
	name := s.f.Name()
	s.f.Close()
	_ = os.Remove(name)
}

// 束の上限(1 回の read-batch に渡すファイル数と、一覧の時点の大きさの合計)。
const (
	batchFilesMax = 2000
	batchBytesMax = 256 << 20
)
