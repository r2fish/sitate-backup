package cli

// 結線: acquire.Ingest の出力を cli の IngestOut に写す(cli の他のファイルは acquire を import しない)。

import (
	"context"
	"errors"

	"github.com/r2fish/sitate-backup/internal/acquire"
	"github.com/r2fish/sitate-backup/internal/contract"
)

func ingestOut(ctx context.Context, acq *acquire.Acquire, idx *acquire.Index, c contract.Call) (IngestOut, error) {
	r, err := acq.Ingest(ctx, c)
	var iu *acquire.IndexUnreadable
	if errors.As(err, &iu) {
		return IngestOut{}, ErrIndexUnreadable()
	}
	if err != nil {
		return IngestOut{}, err
	}
	m := ManifestMaterial{ManifestID: r.ManifestID, EntriesCount: r.EntriesCount, EntriesChunks: chunkRefs(r.EntriesChunks),
		KeyTableCount: r.KeyTableCount, KeyTableChunks: chunkRefs(r.KeyTableChunks), ChunksReferenced: r.ChunksReferenced,
		ChunksNew: r.ChunksNew, BytesNew: r.BytesNew, ChunkKeysMissing: r.ChunkKeysMissing}
	o := IngestOut{ReferencedChunkIDs: r.ReferencedChunkIDs, TargetsMismatch: r.TargetsMismatch, Duration: r.Duration}
	for _, t := range r.Targets {
		m.Targets = append(m.Targets, TargetMaterial{ID: t.ID, Kind: t.Kind, Status: t.Status, Files: t.Files, Bytes: t.Bytes,
			ChangedDuringRead: t.ChangedDuringRead, Dumps: t.Dumps})
		o.Outcomes = append(o.Outcomes, IngestTarget{ID: t.ID, ErrorCode: t.ErrorCode, Errors: items(t.Errors), Warnings: items(t.Warnings),
			ErrorsOmitted: t.ErrorsOmitted, WarningsOmitted: t.WarningsOmitted})
	}
	o.Material = m
	for _, s := range idx.Skipped() {
		o.Skipped = append(o.Skipped, SkippedRef{Ref: s.Ref, Reason: s.Reason})
	}
	return o, nil
}

func chunkRefs(l []acquire.ChunkInfo) []ChunkRefMaterial {
	out := []ChunkRefMaterial{}
	for _, c := range l {
		out = append(out, ChunkRefMaterial{ID: c.ID, VersionID: c.VersionID, Key: c.Key})
	}
	return out
}

func items(l []acquire.ItemRef) []IngestItem {
	out := []IngestItem{}
	for _, i := range l {
		out = append(out, IngestItem{Code: i.Code, ItemRef: i.ItemRef})
	}
	return out
}
