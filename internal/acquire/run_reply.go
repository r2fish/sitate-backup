package acquire

// 取得の入口の出力を backup の結果(schema/cli/results.json の backup)の形にする。

import (
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type backupTargetJSON struct {
	ID                string          `json:"id"`
	Status            string          `json:"status"`
	Bytes             int64           `json:"bytes"`
	Files             int64           `json:"files"`
	ChangedDuringRead int64           `json:"changed_during_read"`
	ErrorsOmitted     int64           `json:"errors_omitted"`
	WarningsOmitted   int64           `json:"warnings_omitted"`
	ErrorCode         *string         `json:"error_code"`
	Detail            json.RawMessage `json:"detail"`
	Next              []contract.Next `json:"next"`
}

// reply は backup の結果の形の応答。health は取得の結果だけで決めた値(対象が ok でない、または鍵の無いチャンクがあれば incomplete)で、
// チェックの結果と汚染疑いの引き継ぎを含めた最終の値は cli がマニフェストを書く前に決める。すべての対象が failed なら ALL_TARGETS_FAILED。
func (r IngestResult) reply() (contract.Reply, error) {
	targets := []backupTargetJSON{}
	health, failed := "pass", 0
	for _, t := range r.Targets {
		bt := backupTargetJSON{ID: t.ID, Status: t.Status, Bytes: t.Bytes, Files: t.Files, ChangedDuringRead: t.ChangedDuringRead,
			ErrorsOmitted: t.ErrorsOmitted, WarningsOmitted: t.WarningsOmitted}
		if t.ErrorCode != "" {
			c := t.ErrorCode
			bt.ErrorCode = &c
		}
		bt.Next = []contract.Next{}
		if t.ErrorCode != "" && t.Detail != nil {
			bt.Detail = t.Detail
			if t.Next != nil {
				bt.Next = t.Next
			}
		}
		if t.Status != "ok" {
			health = "incomplete"
		}
		if t.Status == "failed" {
			failed++
		}
		targets = append(targets, bt)
	}
	if len(targets) > 0 && failed == len(targets) {
		return contract.Reply{}, mkErr(contract.CodeAllTargetsFailed, map[string]any{})
	}
	if r.ChunkKeysMissing > 0 {
		health = "incomplete"
	}
	b, _ := json.Marshal(map[string]any{"snapshot_ref": r.ManifestID, "targets": targets, "checks": []any{}, "health": health,
		"uploaded_bytes": r.BytesNew, "duration_s": int64(r.Duration.Seconds()), "chunk_keys_missing": r.ChunkKeysMissing})
	return contract.Reply{Result: b}, nil
}
