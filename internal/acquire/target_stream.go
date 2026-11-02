package acquire

// 1 つの対象の取得のストリーム(sitate.stream/v1。spec 3.2.2)の受け取りと照合。
// フレームを順に受け、データの本体は保管の流れ(chunkPipe)へそのまま流す(ためない)。項目ごとのハッシュと大きさ、end の hash・bytes を自分で数えて照合する。

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"

	"github.com/zeebo/blake3"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// streamViolation は取得アダプタの契約違反(INTERNAL。component: adapter、reason: output)か、end の不一致・欠落(対象を failed)。
type streamViolation struct{ se contract.StreamError }

func (v *streamViolation) Error() string { return v.se.Error() }

func violation(kind, reason, msg string) error {
	return &streamViolation{contract.StreamError{Kind: kind, Reason: reason, Msg: msg}}
}

type pendingRange struct {
	idx      int
	start, n int64
}

type targetStream struct {
	a    *Acquire
	tid  string
	kind string            // files | postgresql
	prev map[string][]byte // 前回のエントリ(完全なパスのバイト列 → 写し用の JSON)
	pipe *chunkPipe

	all       *blake3.Hasher
	totalData int64
	sawEnd    bool
	endExit   json.RawMessage

	open      string
	itemHash  *blake3.Hasher
	itemBytes int64
	itemStart int64
	cur       entry

	entries    []entry
	byPath     map[string]int
	pending    []pendingRange
	files      map[string]bool
	retried    map[string]bool
	dumps      []string
	changed    int64
	dumpErr    bool
	itemErrors int // item_end が error か、dump の終了状態が 0 でない項目の数
}

func newTargetStream(a *Acquire, tid, kind string, prev []entry, pipe *chunkPipe) *targetStream {
	t := &targetStream{a: a, tid: tid, kind: kind, pipe: pipe, all: blake3.New(), byPath: map[string]int{}, files: map[string]bool{}, retried: map[string]bool{},
		prev: map[string][]byte{}}
	for _, e := range prev {
		b, _ := json.Marshal(e)
		t.prev[string(e.Path)] = b
	}
	return t
}

func blake3Tag(h *blake3.Hasher) string { return "blake3:" + hex.EncodeToString(h.Sum(nil)) }

func (t *targetStream) fullPath(h contract.StreamHeader) ([]byte, error) {
	var rel []byte
	switch {
	case h.Path != nil:
		rel = []byte(*h.Path)
	case h.PathB64 != nil:
		b, err := base64.StdEncoding.DecodeString(*h.PathB64)
		if err != nil {
			return nil, violation("contract", "path", "path_b64")
		}
		rel = b
	default:
		return nil, violation("contract", "path", "path が無い")
	}
	if string(rel) == "." {
		return []byte(t.tid), nil
	}
	if len(rel) == 0 || bytes.IndexByte(rel, 0) >= 0 {
		return nil, violation("contract", "path", "不正なパス")
	}
	for _, c := range bytes.Split(rel, []byte("/")) {
		if len(c) == 0 || string(c) == "." || string(c) == ".." {
			return nil, violation("contract", "path", "不正なパス")
		}
	}
	return append([]byte(t.tid+"/"), rel...), nil
}

// onFrame は AdapterRunner.Run のフレームの受け口。
func (t *targetStream) onFrame(f contract.Frame) error {
	if t.sawEnd {
		return violation("contract", "order", "end の後ろにフレームがある")
	}
	var h contract.StreamHeader
	if err := contract.Validate("adapter/stream.json#/$defs/frame", f.Meta); err != nil {
		return violation("contract", "header", err.Error())
	}
	dec := json.NewDecoder(bytes.NewReader(f.Meta))
	dec.DisallowUnknownFields()
	if dec.Decode(&h) != nil || h.Type != f.Type {
		return violation("contract", "header", "見出しを読めない")
	}
	if h.Type == contract.FrameTypeEnd {
		return t.end(h)
	}
	t.all.Write(f.Meta)
	t.all.Write([]byte("\n"))
	if h.Type == contract.FrameTypeData {
		return t.data(h, f.Data)
	}
	switch h.Type {
	case contract.FrameTypeItemEnd:
		return t.itemEnd(h)
	}
	if t.open != "" {
		return violation("contract", "order", h.Type+" が項目の途中に現れた")
	}
	return t.begin(h)
}

func (t *targetStream) data(h contract.StreamHeader, body []byte) error {
	if t.open == "" {
		return violation("contract", "order", "項目の外の data")
	}
	if h.Size == nil || int64(len(body)) != *h.Size || *h.Size > contract.StreamFragmentMax {
		return violation("contract", "size", "断片の大きさ")
	}
	t.all.Write(body)
	t.totalData += int64(len(body))
	t.itemHash.Write(body)
	t.itemBytes += int64(len(body))
	return t.pipe.Write(body)
}

func (t *targetStream) end(h contract.StreamHeader) error {
	if t.open != "" {
		return violation("contract", "order", "item_end の前に end")
	}
	if h.Bytes == nil || *h.Bytes != t.totalData {
		return violation("end_mismatch", "bytes", "end の bytes")
	}
	if h.Hash != blake3Tag(t.all) {
		return violation("end_mismatch", "hash", "end の hash")
	}
	t.sawEnd, t.endExit = true, h.ExitStatus
	return nil
}

func (t *targetStream) begin(h contract.StreamHeader) error {
	if (h.Type == contract.FrameTypeDump) != (t.kind == "postgresql") {
		return violation("contract", "order", "対象の種別に合わないフレーム "+h.Type)
	}
	switch h.Type {
	case contract.FrameTypeFile, contract.FrameTypeRetry:
		return t.beginFile(h)
	case contract.FrameTypeDump:
		return t.beginDump(h)
	case contract.FrameTypeUnchg:
		return t.unchanged(h)
	case contract.FrameTypeDir, contract.FrameTypeSymlink:
		return t.meta(h)
	}
	return violation("contract", "header", "知らない type")
}

func (t *targetStream) newEntry(h contract.StreamHeader, typ string) (entry, error) {
	p, err := t.fullPath(h)
	if err != nil {
		return entry{}, err
	}
	e := entry{Ref: t.a.KeyS.EntryRef(string(p)), Target: t.tid, Path: p, Type: typ, Mode: h.Mode, UID: h.UID, GID: h.GID, MtimeNS: h.MtimeNS}
	return e, nil
}

func (t *targetStream) beginFile(h contract.StreamHeader) error {
	e, err := t.newEntry(h, "file")
	if err != nil {
		return err
	}
	k := string(e.Path)
	if h.Type == contract.FrameTypeRetry {
		if !t.files[k] || t.retried[k] {
			return violation("contract", "order", "file-retry の前の file が無いか 2 回目")
		}
		t.retried[k] = true
	} else {
		t.files[k] = true
	}
	t.open, t.cur, t.itemHash, t.itemBytes, t.itemStart = h.Type, e, blake3.New(), 0, t.pipe.written
	return nil
}

func (t *targetStream) beginDump(h contract.StreamHeader) error {
	fixed := map[string]string{contract.DumpFormatGlobals: "globals.sql", contract.DumpFormatCustom: "db.dump"}[h.Format]
	if fixed == "" || len(t.dumps) > 1 || (h.Format == contract.DumpFormatCustom) != (len(t.dumps) == 1) {
		return violation("contract", "order", "dump は globals の後に custom を 1 回ずつ")
	}
	p := []byte(t.tid + "/" + fixed)
	t.cur = entry{Ref: "d:" + t.tid + ":" + h.Format, Target: t.tid, Path: p, Type: "dump", Format: h.Format}
	t.open, t.itemHash, t.itemBytes, t.itemStart = contract.FrameTypeDump, blake3.New(), 0, t.pipe.written
	t.dumps = append(t.dumps, h.Format)
	return nil
}

func (t *targetStream) unchanged(h contract.StreamHeader) error {
	p, err := t.fullPath(h)
	if err != nil {
		return err
	}
	raw, ok := t.prev[string(p)]
	if !ok {
		return violation("contract", "order", "前回に無いパスの unchanged")
	}
	var e entry
	if json.Unmarshal(raw, &e) != nil {
		return violation("contract", "order", "前回のエントリ")
	}
	t.put(e)
	return nil
}

func (t *targetStream) meta(h contract.StreamHeader) error {
	e, err := t.newEntry(h, h.Type)
	if err != nil {
		return err
	}
	if h.Type == contract.FrameTypeSymlink {
		e.Mode = nil
		switch {
		case h.Target != nil:
			e.LinkTo, e.HasLink = []byte(*h.Target), true
		case h.TargetB64 != nil:
			b, err := base64.StdEncoding.DecodeString(*h.TargetB64)
			if err != nil {
				return violation("contract", "path", "target_b64")
			}
			e.LinkTo, e.HasLink = b, true
		default:
			return violation("contract", "header", "symlink に target が無い")
		}
	}
	t.put(e)
	return nil
}

// put はエントリを置く(同じパスは置き換える)。
func (t *targetStream) put(e entry) int {
	if i, ok := t.byPath[string(e.Path)]; ok {
		t.entries[i] = e
		return i
	}
	t.entries = append(t.entries, e)
	t.byPath[string(e.Path)] = len(t.entries) - 1
	return len(t.entries) - 1
}

func (t *targetStream) itemEnd(h contract.StreamHeader) error {
	if t.open == "" {
		return violation("contract", "order", "開いた項目が無い item_end")
	}
	if h.Bytes == nil || *h.Bytes != t.itemBytes || h.Hash != blake3Tag(t.itemHash) {
		return violation("contract", "item", "item_end の bytes か hash が本体と違う")
	}
	hasExit := len(h.ExitStatus) > 0 && string(h.ExitStatus) != "null"
	if hasExit != (t.open == contract.FrameTypeDump) {
		return violation("contract", "item", "exit_status は dump だけが持つ")
	}
	if h.Status == contract.ItemChanged && t.open != contract.FrameTypeRetry {
		return violation("contract", "item", "changed は file-retry だけ")
	}
	open := t.open
	t.open = ""
	if h.Status == contract.ItemError || (open == contract.FrameTypeDump && string(h.ExitStatus) != "0") {
		t.itemErrors++
		if open == contract.FrameTypeDump {
			t.dumpErr = true
		}
		if open == contract.FrameTypeRetry { // 読み直しが失敗: 先の読みの内容も正しいと言えないのでエントリから外す
			t.drop(t.cur.Path)
		}
		return nil // 読めなかった項目はエントリにしない(結果の errors[] に出る)
	}
	e := t.cur
	e.Size, e.Hash = i64p(t.itemBytes), h.Hash
	if h.Status == contract.ItemChanged {
		e.Flags = []string{"changed_during_read"}
		t.changed++
	}
	i := t.put(e)
	t.setPending(i, t.itemStart, t.itemBytes)
	return nil
}

func (t *targetStream) setPending(idx int, start, n int64) {
	for k, p := range t.pending {
		if p.idx == idx {
			t.pending[k] = pendingRange{idx, start, n}
			return
		}
	}
	t.pending = append(t.pending, pendingRange{idx, start, n})
}

// setRanges は保管したチャンクの並びから各項目の ranges を埋める。
func (t *targetStream) setRanges(chunks []chunkOut) {
	for _, p := range t.pending {
		r := rangesFor(chunks, p.start, p.n)
		t.entries[p.idx].Ranges = &r
	}
}

// drop はパスのエントリを外す(後の置き換えに備えて印を付け、集めるときに除く)。
func (t *targetStream) drop(path []byte) {
	i, ok := t.byPath[string(path)]
	if !ok {
		return
	}
	t.entries[i].Type = ""
	delete(t.byPath, string(path))
	for k, p := range t.pending {
		if p.idx == i {
			t.pending = append(t.pending[:k], t.pending[k+1:]...)
			break
		}
	}
}

// liveEntries は外したものを除いたエントリ。
func (t *targetStream) liveEntries() []entry {
	out := make([]entry, 0, len(t.entries))
	for _, e := range t.entries {
		if e.Type != "" {
			out = append(out, e)
		}
	}
	return out
}
