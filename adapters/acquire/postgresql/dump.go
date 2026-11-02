package main

import (
	"io"

	"github.com/zeebo/blake3"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 項目は globals(dump-pg-globals)が本体(dump-pg)より先。受け口は項目ごとに 1 回ずつ呼ぶ(spec 3.2.2)。
var items = []struct{ format, op string }{
	{contract.DumpFormatGlobals, "dump-pg-globals"},
	{contract.DumpFormatCustom, "dump-pg"},
}

// acquire は 1 対象の取得の全体: 項目ごとに受け口を呼んで dump フレームへ流し、end と結果を出す。
func acquire(out io.Writer, recv, id string) int {
	st := newStreamOut(out)
	o := &outcome{}
	for _, it := range items {
		cls, n, code := dumpOne(st, recv, id, it.op, it.format)
		o.lastExit = code
		o.items++
		switch cls {
		case dumpMismatch:
			_ = st.flush()
			return failJSON(out, "RECEIVER_TARGETS_MISMATCH", map[string]any{"target_ids": []string{id}})
		case dumpUnreachable:
			_ = st.flush()
			return failJSON(out, "STORAGE_UNREACHABLE", map[string]any{"target": "source", "reason": "connect"})
		case dumpOK:
			o.okItems++
			o.bytes += n
		case dumpItemFail:
			o.failedN++
			ref := "d:" + id + ":" + it.format
			o.errs = append(o.errs, resultItem{"EXIT_STATUS", &ref})
		case dumpTargetFail:
			o.failed = true
			o.errs = append(o.errs, resultItem{"EXIT_STATUS", nil})
		}
		if o.failed {
			break
		}
	}
	if err := st.end(o.lastExit); err != nil {
		return failJSON(out, "INTERNAL", map[string]any{"component": "adapter", "reason": "output"})
	}
	if _, err := st.w.Write(o.resultJSON()); err != nil || st.flush() != nil {
		return 1
	}
	return 0
}

// dumpOne は受け口を 1 回呼び、標準出力を StreamFragmentMax 以下の断片にして dump フレームへ流す。
// 受け口の終了状態を得てから item_end を書く(0 以外でも、それまでに出た本体はそのまま item_end で閉じる)。
// 126・255 では item_end を書かない(呼び出し側が error を出して終わる)。
func dumpOne(st *streamOut, recv, id, op, format string) (dumpClass, int64, int) {
	rc, err := startRecv(recv, op, id, false)
	if err != nil {
		return dumpUnreachable, 0, exitUnreachable
	}
	st.dump(format)
	h := blake3.New()
	var n int64
	buf := make([]byte, contract.StreamFragmentMax)
	var readErr error
	for {
		k, rerr := io.ReadFull(rc.stdout, buf)
		if k > 0 {
			st.data(buf[:k])
			h.Write(buf[:k])
			n += int64(k)
		}
		if rerr != nil {
			if rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
				readErr = rerr
			}
			break
		}
	}
	status := rc.wait()
	if readErr != nil && status == 0 {
		status = exitUnreachable // 読み取りの途中で切れたのに終了状態が 0 のままの成功にはしない
	}
	cls := classifyDump(status)
	if cls == dumpMismatch || cls == dumpUnreachable {
		return cls, n, status
	}
	st.itemEnd(n, h, status)
	return cls, n, status
}
