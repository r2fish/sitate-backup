package cli

// backup の流れの 2〜5(spec 7.5): 取得の入口(Deps.Ingest。wire が acquire.Ingest を包む)の出力から、参照の存在の照合、
// 健常性と保持区分の決定、マニフェストの書き込み、世代の一覧 snapshots[] への追記、索引の確定を行う。
// cli は acquire を import しない。取得の出力は cli の型 IngestOut で受ける。

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// IngestItem は取得の結果の errors[]・warnings[] の要素(パスは item_ref に置き換え済み)。
type IngestItem struct {
	Code    string  `json:"code"`
	ItemRef *string `json:"item_ref"`
}

// IngestTarget は対象ごとの取得の結果のうち、マニフェストに入らない分(run_end の targets[] と結果の元)。
type IngestTarget struct {
	ID              string
	ErrorCode       string // status が failed のとき
	Errors          []IngestItem
	Warnings        []IngestItem
	ErrorsOmitted   int64
	WarningsOmitted int64
}

// IngestOut は取得の入口の出力。Material は ManifestID・Targets・エントリと鍵表のチャンク・統計。残り(RepoID・CreatedAt ほか)は cli が埋める。
type IngestOut struct {
	Material           ManifestMaterial
	Outcomes           []IngestTarget // Material.Targets と同じ順
	ReferencedChunkIDs []string
	TargetsMismatch    []string
	Skipped            []SkippedRef // 直前の索引の再構成が飛ばした世代
	Duration           time.Duration
}

// SkippedRef は索引の再構成が飛ばした世代(結果の skipped_backups[])。
type SkippedRef struct {
	Ref    string `json:"ref"`
	Reason string `json:"reason"`
}

// errIndexUnreadable は取得の入口が索引を開けなかったこと(wire が acquire.IndexUnreadable から写す)。cli は再構成して 1 度だけやり直す。
var errIndexUnreadable = errors.New("index unreadable")

// ErrIndexUnreadable は wire の Ingest が索引を開けないときに返す値。
func ErrIndexUnreadable() error { return errIndexUnreadable }

// ingest は取得の入口を呼ぶ。索引を開けないときは保管先のマニフェストから再構成して 1 度だけやり直す(spec 4.3)。
func (d *Deps) ingest(ctx context.Context, c contract.Call) (IngestOut, error) {
	out, err := d.Ingest(ctx, c)
	if errors.Is(err, errIndexUnreadable) {
		if rerr := d.rebuildIndex(ctx); rerr != nil {
			return IngestOut{}, rerr
		}
		out, err = d.Ingest(ctx, c)
		if errors.Is(err, errIndexUnreadable) {
			return IngestOut{}, internalErr()
		}
	}
	return out, err
}

// resultTargets は結果と run_end の元になる対象の配列(errors[]・warnings[] つき。backupEntry が件数だけにして結果へ出す)。
func resultTargets(o IngestOut) []any {
	out := []any{}
	for i, t := range o.Material.Targets {
		oc := o.Outcomes[i]
		var code any
		if oc.ErrorCode != "" {
			code = oc.ErrorCode
		}
		out = append(out, map[string]any{"id": t.ID, "status": t.Status, "bytes": t.Bytes, "files": t.Files, "changed_during_read": t.ChangedDuringRead,
			"errors": itemsAny(oc.Errors), "warnings": itemsAny(oc.Warnings), "errors_omitted": oc.ErrorsOmitted, "warnings_omitted": oc.WarningsOmitted, "error_code": code})
	}
	return out
}

func itemsAny(l []IngestItem) []any {
	out := []any{}
	for _, it := range l {
		var ref any
		if it.ItemRef != nil {
			ref = *it.ItemRef
		}
		out = append(out, map[string]any{"code": it.Code, "item_ref": ref})
	}
	return out
}

// snapView は state.json の snapshots[] から判定に使う項目。
type snapView struct {
	Ref            string `json:"ref"`
	CreatedAt      string `json:"created_at"`
	RetentionClass string `json:"retention_class"`
	Health         string `json:"health"`
	TargetsOK      bool   `json:"targets_ok"`
	RestoreFailed  bool   `json:"restore_failed"`
}

func (d *Deps) loadSnapshots(ctx context.Context) ([]snapView, error) {
	raw, err := d.State.Get(ctx, "snapshots")
	if err != nil {
		return nil, err
	}
	var s []snapView
	if len(raw) > 0 && string(raw) != "null" {
		if json.Unmarshal(raw, &s) != nil {
			return nil, internalErr()
		}
	}
	return s, nil
}

// definitionView は承認済みの定義から backup が使う項目。
type definitionView struct {
	Timezone string `json:"timezone"`
}

// finishManifest は backup の流れの 2〜5(マニフェストを書く)。health は最終の健常性。
func (d *Deps) finishManifest(ctx context.Context, c contract.Call, o IngestOut) (ManifestWritten, error) {
	if d.State == nil || d.Object == nil || d.Codec == nil || d.KeyS == nil || d.Index == nil || d.CommitIndex == nil || d.Store == nil || d.Clock == nil || d.Retention == nil {
		return ManifestWritten{}, contract.ErrNotImplemented("cli.backup")
	}
	m := o.Material
	def, hash, err := d.State.ReadDef(ctx)
	if err != nil || len(def) == 0 {
		return ManifestWritten{}, internalErr()
	}
	repo, err := d.repoID(ctx)
	if err != nil {
		return ManifestWritten{}, err
	}
	snaps, err := d.loadSnapshots(ctx)
	if err != nil {
		return ManifestWritten{}, err
	}
	degraded, err := d.refsDegraded(ctx, o) // 欠けた参照の送り直しは未実装: 欠けたら健常性を incomplete にする
	if err != nil {
		return ManifestWritten{}, err
	}
	now, err := d.Clock.Now(ctx)
	if err != nil {
		return ManifestWritten{}, err
	}
	m.RepoID, m.CreatedAt, m.DefHash, m.Definition, m.BinaryVersion = repo, now, hash, def, Version
	m.Parent = lastRef(snaps)
	var dv definitionView
	_ = json.Unmarshal(def, &dv)
	m.RefsMissing = degraded
	healthy := manifestHealth(m) == "pass"
	class, err := d.Store.Class(contract.ClassInput{CreatedAt: now, Healthy: healthy, Timezone: tzOrUTC(dv.Timezone), Snapshots: classRefs(snaps)})
	if err != nil {
		return ManifestWritten{}, err
	}
	w, err := ManifestWriter{Codec: d.Codec, KeyS: d.KeyS, Store: d.Object}.Write(ctx, m, class)
	if err != nil {
		return ManifestWritten{}, err
	}
	if err := d.recordSnapshot(ctx, m, w, now, healthy); err != nil {
		return ManifestWritten{}, err
	}
	if err := d.CommitIndex(ctx, m.ManifestID, o.ReferencedChunkIDs); err != nil {
		return ManifestWritten{}, err
	}
	return w, nil
}

func tzOrUTC(s string) string {
	if s == "" {
		return "UTC"
	}
	return s
}

func lastRef(snaps []snapView) string {
	var best time.Time
	ref := ""
	for _, s := range snaps {
		if t, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil && !t.Before(best) {
			best, ref = t, s.Ref
		}
	}
	return ref
}

func classRefs(snaps []snapView) []contract.SnapshotClassRef {
	out := []contract.SnapshotClassRef{}
	for _, s := range snaps {
		if t, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil {
			out = append(out, contract.SnapshotClassRef{CreatedAt: t, RetentionClass: contract.RetentionClass(s.RetentionClass)})
		}
	}
	return out
}
