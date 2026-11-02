package acquire

// 索引の再構成(spec 4.3「索引の再構成」、4.5)。マニフェストの本体(復号した JSON)と保管先のエントリ・鍵表のチャンクから、
// index/chunks と index/files/<id> を作り直す。新しい索引は別に作り、すべての世代を読み終えてから置き換える。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// SkippedBackup は再構成で飛ばした世代(理由は missing / header / tag / format_too_new / format_too_old)。
type SkippedBackup struct{ Ref, Reason string }

type manifestBody struct {
	ManifestID string `json:"manifest_id"`
	CreatedAt  string `json:"created_at"`
	Definition json.RawMessage
	Targets    []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"targets"`
	Entries struct {
		Chunks []rebuildChunk `json:"chunks"`
	} `json:"entries"`
	ChunkKeys struct {
		Chunks []rebuildChunk `json:"chunks"`
	} `json:"chunk_keys"`
}

type rebuildChunk struct {
	ID, VersionID, Key string
}

func (c *rebuildChunk) UnmarshalJSON(b []byte) error {
	var j struct {
		ID  string `json:"id"`
		V   string `json:"version_id"`
		K   string `json:"key"`
		Alt string `json:"v"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	*c = rebuildChunk{ID: j.ID, VersionID: j.V, Key: j.K}
	return nil
}

// Skipped は直前の Rebuild が飛ばした世代。
func (ix *Index) Skipped() []SkippedBackup { return append([]SkippedBackup(nil), ix.skipped...) }

// Rebuild は contract.Index を満たす。manifests は保持中・保護中のマニフェストの復号した本体(sitate.manifest/v1 の JSON。復号は keys)、
// against は比べる定義(index/files/<id> は、その定義と根・元サーバーの所在が同じ世代のエントリからだけ作る)。
// 読めない世代は飛ばして続け(Skipped)、読める世代が 1 つも無ければ誤りで、古い索引を残す。
func (ix *Index) Rebuild(ctx context.Context, manifests [][]byte, against []byte) error {
	if err := ix.ready(); err != nil {
		return err
	}
	if ix.Store == nil {
		return errInternal("")
	}
	var bodies []manifestBody
	for _, m := range manifests {
		var b manifestBody
		if json.Unmarshal(m, &b) == nil && b.ManifestID != "" {
			bodies = append(bodies, b)
		}
	}
	if len(bodies) == 0 {
		return errInternal("output")
	}
	sort.SliceStable(bodies, func(i, j int) bool { return bodies[i].CreatedAt < bodies[j].CreatedAt })
	want, err := parseDefForRebuild(against)
	if err != nil {
		return err
	}
	st := &rebuildState{ix: ix, m: map[string]*idxRec{}, files: map[string][]entry{}, want: want}
	ix.skipped = nil
	for _, b := range bodies {
		if e := ctxErr(ctx); e != nil {
			return e
		}
		if why := st.add(ctx, b); why != "" {
			ix.skipped = append(ix.skipped, SkippedBackup{Ref: b.ManifestID, Reason: why})
		}
	}
	if len(st.m) == 0 {
		return errInternal("output")
	}
	return st.commit(ctx)
}

func parseDefForRebuild(against []byte) (checkDef, error) {
	if len(against) == 0 {
		return checkDef{}, nil
	}
	return parseCheckDef(against)
}

type rebuildState struct {
	ix    *Index
	m     map[string]*idxRec
	files map[string][]entry // 対象 id → 最新の合う世代のエントリ
	want  checkDef
}

// read は保管先のチャンクを読んで開く。理由は missing / tag / header / format_too_new / format_too_old。
func (s *rebuildState) read(ctx context.Context, c rebuildChunk) ([]byte, string) {
	rc, err := s.ix.Store.Get(ctx, chunkKeyOf(c.ID), c.VersionID)
	if err != nil {
		return nil, "missing"
	}
	defer rc.Close()
	sealed, err := io.ReadAll(rc)
	key, kerr := base64.StdEncoding.DecodeString(c.Key)
	if err != nil || kerr != nil {
		return nil, "missing"
	}
	plain, err := s.ix.Codec.Open(sealed, key, c.ID)
	if err != nil {
		var fe *contract.FormatError
		if errors.As(err, &fe) {
			return nil, map[string]string{"header": "header", "too_new": "format_too_new", "too_old": "format_too_old"}[fe.Reason] + map[bool]string{true: "tag", false: ""}[fe.Reason == "tag"]
		}
		return nil, "tag"
	}
	return plain, ""
}

func (s *rebuildState) readAll(ctx context.Context, l []rebuildChunk) ([]byte, string) {
	var out []byte
	for _, c := range l {
		b, why := s.read(ctx, c)
		if why != "" {
			return nil, why
		}
		out = append(out, b...)
	}
	return out, ""
}

// add は 1 つの世代を取り込む(読めなければ理由を返し、何も足さない)。
func (s *rebuildState) add(ctx context.Context, b manifestBody) string {
	kt, why := s.readAll(ctx, b.ChunkKeys.Chunks)
	if why != "" {
		return why
	}
	en, why := s.readAll(ctx, b.Entries.Chunks)
	if why != "" {
		return why
	}
	rows := []rebuildChunk{}
	rows = append(rows, b.Entries.Chunks...)
	rows = append(rows, b.ChunkKeys.Chunks...)
	sc := bufio.NewScanner(bytes.NewReader(kt))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var r struct{ ID, V, K string }
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			return "tag"
		}
		rows = append(rows, rebuildChunk{ID: r.ID, VersionID: r.V, Key: r.K})
	}
	var ents []entry
	sc = bufio.NewScanner(bytes.NewReader(en))
	sc.Buffer(nil, 16<<20)
	for sc.Scan() {
		var e entry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			return "tag"
		}
		ents = append(ents, e)
	}
	for _, r := range rows {
		if err := s.addChunk(b.ManifestID, r); err != nil {
			return "tag"
		}
	}
	s.addFiles(b, ents)
	return ""
}

func (s *rebuildState) addChunk(mid string, r rebuildChunk) error {
	if _, ok := s.m[r.ID]; ok {
		return nil // first_seen は最初に参照した世代(作成時刻の古い順に取り込む)
	}
	key, err := base64.StdEncoding.DecodeString(r.Key)
	if err != nil {
		return err
	}
	w, err := s.ix.KeyS.WrapChunkKey(key, r.ID)
	if err != nil {
		return err
	}
	s.m[r.ID] = &idxRec{Op: "reserve", ChunkID: r.ID, VersionID: r.VersionID, WrappedKey: w, FirstSeen: mid, FormatVersion: contract.FormatVersion}
	return nil
}

// addFiles は、比べる定義と根・所在が同じ対象のエントリを、新しい世代のもので置き換えて取っておく。判定できない世代からは作らない。
func (s *rebuildState) addFiles(b manifestBody, ents []entry) {
	if len(b.Definition) == 0 || s.want.Host == "" {
		return
	}
	theirs, err := parseCheckDef(wrapDefinition(b.Definition))
	if err != nil || theirs.Host != s.want.Host {
		return
	}
	for _, t := range s.want.Targets {
		if t.Files == "" || !hasFilesTarget(theirs, t) || !targetOK(b, t.ID) { // 最後に取り終えた(status: ok の)世代のエントリだけ
			continue
		}
		var l []entry
		for _, e := range ents {
			if e.Target == t.ID && e.Type != "dump" {
				l = append(l, e)
			}
		}
		s.files[t.ID] = l
	}
}

func wrapDefinition(d json.RawMessage) []byte { return d }

func hasFilesTarget(d checkDef, t checkTarget) bool {
	for _, x := range d.Targets {
		if x.ID == t.ID && x.Files == t.Files {
			return true
		}
	}
	return false
}

// commit は作った索引を別のファイルに書いてから置き換える。
func (s *rebuildState) commit(ctx context.Context) error {
	ix := s.ix
	key, err := ix.KeyS.DerivedKey("state")
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(s.m))
	for id := range s.m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var buf bytes.Buffer
	for _, id := range ids {
		plain, _ := json.Marshal(s.m[id])
		sealed, err := ix.Codec.Encode(contract.MagicState, indexRel, ix.header(), plain, key)
		if err != nil {
			return err
		}
		buf.Write(binary.BigEndian.AppendUint32(nil, uint32(len(sealed))))
		buf.Write(sealed)
	}
	if e := ctxErr(ctx); e != nil { // 取り消されたら古い索引を残す
		return e
	}
	fi := fileIndex{dir: ix.Dir, codec: ix.Codec, keys: ix.KeyS}
	for tid, l := range s.files {
		if err := fi.store(tid, l); err != nil {
			return err
		}
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(ix.path()), 0o700); err != nil {
		return err
	}
	if err := atomicWrite(ix.path(), buf.Bytes()); err != nil {
		return err
	}
	ix.loaded = false
	return nil
}

func targetOK(b manifestBody, id string) bool {
	for _, t := range b.Targets {
		if t.ID == id {
			return t.Status == "ok"
		}
	}
	return false
}
