package restore

import (
	"context"
	"encoding/base64"
	"errors"
	"io"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const chunkCacheMax = 2

// chunkSource はデータのチャンクを読んで開く(直前のいくつかを覚える)。壊れ・欠けは *fileFailure。
type chunkSource struct {
	ctx   context.Context
	o     *objReader
	keys  map[string]keyRow
	cache map[string][]byte
	order []string
}

func newChunkSource(ctx context.Context, o *objReader, rows []keyRow) *chunkSource {
	ks := make(map[string]keyRow, len(rows))
	for _, r := range rows {
		ks[r.ID] = r
	}
	return &chunkSource{ctx: ctx, o: o, keys: ks, cache: map[string][]byte{}}
}

func (s *chunkSource) plain(id, ver string) ([]byte, error) {
	if b, ok := s.cache[id]; ok {
		return b, nil
	}
	row, ok := s.keys[id]
	if !ok || len(id) < 2 {
		return nil, &fileFailure{ChunkID: id, VersionID: ver, Reason: "decrypt"}
	}
	key, err := base64.StdEncoding.DecodeString(row.K)
	if err != nil {
		return nil, &fileFailure{ChunkID: id, VersionID: ver, Reason: "decrypt"}
	}
	sealed, err := s.o.readAll(s.ctx, "chunks/"+id[:2]+"/"+id, ver)
	if errors.Is(err, contract.ErrMissing) {
		return nil, &fileFailure{ChunkID: id, VersionID: ver, Reason: "missing"}
	}
	if err != nil {
		return nil, err
	}
	b, err := s.o.codec.Open(sealed, key, id)
	if err != nil {
		var fe *contract.FormatError
		if isDecrypt(err) || errors.As(err, &fe) {
			return nil, &fileFailure{ChunkID: id, VersionID: ver, Reason: "decrypt"}
		}
		return nil, err
	}
	if len(s.order) >= chunkCacheMax {
		delete(s.cache, s.order[0])
		s.order = s.order[1:]
	}
	s.cache[id] = b
	s.order = append(s.order, id)
	return b, nil
}

// content は エントリの内容(ranges をつなぐ)を w に流す。範囲が平文の外なら decrypt の失敗。
func (s *chunkSource) content(e entry, w io.Writer) error {
	for _, r := range e.Ranges {
		p, err := s.plain(r.ID, r.VersionID)
		if err != nil {
			return err
		}
		if r.Offset < 0 || r.Length < 0 || r.Length > int64(len(p))-r.Offset {
			return &fileFailure{ChunkID: r.ID, VersionID: r.VersionID, Reason: "decrypt"}
		}
		if _, err := w.Write(p[r.Offset : r.Offset+r.Length]); err != nil {
			return err
		}
	}
	return nil
}
