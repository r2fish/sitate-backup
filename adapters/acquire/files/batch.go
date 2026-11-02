package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"encoding/hex"
	"io"

	"github.com/zeebo/blake3"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// readBodies は spool の変化したファイルを束ごとに read-batch で読んで file のフレームを出し、
// 読み取り中に変化したファイルを 1 回だけ読み直して file-retry で送り直す(spec 3.2.2)。
func readBodies(st *streamOut, o *outcome, sp *spool, recv, id string) (exitClass, string) {
	var retry *spool
	if sp.n > 0 {
		var err error
		if retry, err = newSpool(""); err != nil {
			return classBadOutput, ""
		}
		defer retry.remove()
		if cls, code := batches(st, o, sp, recv, id, false, retry); cls != classOK {
			return cls, code
		}
	}
	if retry != nil && retry.n > 0 {
		return batches(st, o, retry, recv, id, true, nil)
	}
	return classOK, ""
}

// batches は spool を束に分けて 1 つずつ読む。changedOut には変化の一覧に載ったレコードを足す(読み直しの束では nil)。
func batches(st *streamOut, o *outcome, sp *spool, recv, id string, isRetry bool, changedOut *spool) (exitClass, string) {
	lr, err := sp.reader()
	if err != nil {
		return classBadOutput, ""
	}
	var batch []listRec
	var total int64
	flush := func() (exitClass, string) {
		if len(batch) == 0 {
			return classOK, ""
		}
		cls, code := runBatch(st, o, recv, id, batch, isRetry, changedOut)
		batch, total = nil, 0
		return cls, code
	}
	for {
		r, err := lr.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return classBadOutput, ""
		}
		batch = append(batch, r)
		total += r.size
		if len(batch) >= batchFilesMax || total >= batchBytesMax || isRetry {
			if cls, code := flush(); cls != classOK {
				return cls, code
			}
		}
	}
	return flush()
}

// runBatch は 1 回の read-batch を行い、tar の各メンバーを file / file-retry のフレームにする。
// 読み直し(isRetry)は 1 ファイルずつ呼び、item_end は変化の一覧を読んだ後に status を決めて出す(spec 3.2.2)。
func runBatch(st *streamOut, o *outcome, recv, id string, batch []listRec, isRetry bool, changedOut *spool) (exitClass, string) {
	rc, err := startRecv(recv, "read-batch", id, true)
	if err != nil {
		o.lastExit = exitUnreachable
		return classUnreachable, ""
	}
	go func() {
		w := bufio.NewWriter(rc.stdin)
		for _, r := range batch {
			w.Write(r.path)
			w.WriteByte(0)
		}
		w.Flush()
		rc.stdin.Close()
	}()
	br := bufio.NewReaderSize(rc.stdout, 1<<18)
	done := map[string]bool{}
	idx, pend, tarOK := feedTar(st, o, br, batch, isRetry, done)
	var changed [][]byte
	tailOK := false
	if tarOK {
		changed, tailOK = readTail(br)
	} else {
		io.Copy(io.Discard, br)
	}
	status := rc.wait()
	o.lastExit = status
	if pend != nil {
		finishRetry(st, o, pend, changed, tarOK && status == 0)
	}
	if cls, code := classify("read-batch", status); cls != classOK {
		return cls, code
	}
	if !tarOK {
		return classFail, "EXIT_STATUS"
	}
	if !tailOK { // 変化の一覧の区切りが無い出力は契約違反(読み取り中の変化を黙って見逃さない)
		return classBadOutput, ""
	}
	if !isRetry {
		for _, r := range batch[idx:] {
			o.addError("NOT_FOUND", r.path)
		}
		markChanged(batch, changed, changedOut, done)
	}
	return classOK, ""
}

// pending は読み直しの item_end を待っているファイル。
type pending struct {
	r    listRec
	n    int64
	hash string
}

// finishRetry は読み直しのファイルの item_end を出す。変化の一覧に載れば status: changed(警告と件数)。
func finishRetry(st *streamOut, o *outcome, p *pending, changed [][]byte, ok bool) {
	status := contract.ItemOK
	if !ok {
		status = contract.ItemError
	}
	for _, c := range changed {
		if ok && bytes.Equal(c, p.r.path) {
			status = contract.ItemChanged
			o.addWarning("CHANGED_DURING_READ", p.r.path)
			o.changed++
		}
	}
	st.itemEnd(p.n, p.hash, status, true)
}

// feedTar は tar を読み、束の順にメンバーを突き合わせる。tar に現れなかった束の要素は NOT_FOUND(読み直しでは何もしない)。
// 返す idx は処理済みの束の位置(以降は tar に現れなかった要素)。tarOK が偽なら tar が壊れた。
// 読み直しでは、最後のメンバーの item_end を出さずに pending で返す。
func feedTar(st *streamOut, o *outcome, br *bufio.Reader, batch []listRec, isRetry bool, done map[string]bool) (int, *pending, bool) {
	tr := tar.NewReader(br)
	cur := 0
	buf := make([]byte, contract.StreamFragmentMax)
	var pend *pending
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return cur, pend, true
		}
		if err != nil {
			if st.open && pend == nil {
				st.itemEnd(0, "", contract.ItemError, true)
			}
			return cur, pend, false
		}
		i := cur
		for i < len(batch) && !bytes.Equal(batch[i].path, []byte(hdr.Name)) {
			i++
		}
		if i == len(batch) || hdr.Typeflag != tar.TypeReg {
			continue // 束に無いメンバー、通常のファイルでないメンバーは使わない
		}
		if !isRetry {
			for _, r := range batch[cur:i] {
				o.addError("NOT_FOUND", r.path)
			}
		}
		cur = i + 1
		done[string(batch[i].path)] = true
		n, hash, ok := emitBody(st, o, tr, batch[i], isRetry, buf)
		if !ok {
			return cur, nil, false
		}
		if isRetry {
			pend = &pending{batch[i], n, hash}
		} else {
			st.itemEnd(n, hash, contract.ItemOK, true)
		}
	}
}

// emitBody は 1 ファイルの本体を file / file-retry の見出しと data の断片にする(item_end は呼び出し側)。壊れたら item_end(error)で閉じて偽。
func emitBody(st *streamOut, o *outcome, tr io.Reader, r listRec, isRetry bool, buf []byte) (int64, string, bool) {
	typ := contract.FrameTypeFile
	if isRetry {
		typ = contract.FrameTypeRetry
	}
	st.fileLike(typ, r)
	h := blake3.New()
	var n int64
	for {
		k, err := readFull(tr, buf)
		if k > 0 {
			st.data(buf[:k])
			h.Write(buf[:k])
			n += int64(k)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			st.itemEnd(n, "blake3:"+hex.EncodeToString(h.Sum(nil)), contract.ItemError, true)
			return n, "", false
		}
	}
	o.bytes += n
	return n, "blake3:" + hex.EncodeToString(h.Sum(nil)), true
}

// readFull は buf が埋まるか本体が終わる(io.EOF)まで読む。途中の切れ(tar の壊れ)は io.ErrUnexpectedEOF のまま返す。
func readFull(r io.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		k, err := r.Read(buf[n:])
		n += k
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// readTail は tar の終端の後ろ(ゼロ埋め、区切りの行、NUL 区切りの変化の一覧)を読む。壊れていれば ok が偽。
func readTail(br *bufio.Reader) ([][]byte, bool) {
	for {
		b, err := br.Peek(1)
		if err != nil {
			return nil, false
		}
		if b[0] != 0 {
			break
		}
		_, _ = br.ReadByte()
	}
	line, err := br.ReadString('\n')
	if err != nil || line != contract.ReadBatchChangedMarker {
		return nil, false
	}
	rest, _ := io.ReadAll(br)
	if len(rest) == 0 {
		return nil, true
	}
	if rest[len(rest)-1] != 0 {
		return nil, false
	}
	return bytes.Split(rest[:len(rest)-1], []byte{0}), true
}

// markChanged は最初の読みで変化の一覧に載ったファイルを、読み直しの spool へ足す。
func markChanged(batch []listRec, changed [][]byte, changedOut *spool, done map[string]bool) {
	if changedOut == nil {
		return
	}
	set := map[string]bool{}
	for _, c := range changed {
		set[string(c)] = true
	}
	for _, r := range batch {
		if set[string(r.path)] && done[string(r.path)] { // 先に file を送ったものだけ読み直す
			_ = changedOut.add(r)
		}
	}
}
