package cli

// backup の流れの補助: 参照の存在の照合、状態ファイル(repo_id・snapshots[]・protected[])の読み書き、索引の再構成(spec 4.3、4.5)。

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// refsDegraded は参照するチャンクの存在を Retention.MissingRefs で照合し、欠けがあれば true(spec 7.5 backup の流れ 2)。
func (d *Deps) refsDegraded(ctx context.Context, o IngestOut) (bool, error) {
	refs := []contract.VersionRef{}
	for _, id := range o.ReferencedChunkIDs {
		ref, err := d.Index.Lookup(ctx, id)
		if err != nil {
			return false, err
		}
		if ref == nil || ref.VersionID == "" || len(id) < 2 {
			continue
		}
		refs = append(refs, contract.VersionRef{Key: "chunks/" + id[:2] + "/" + id, VersionID: ref.VersionID})
	}
	if len(refs) == 0 {
		return false, nil
	}
	missing, err := d.Store.MissingRefs(ctx, refs)
	if err != nil {
		return false, err
	}
	return len(missing) > 0, nil
}

func (d *Deps) repoID(ctx context.Context) (string, error) {
	raw, err := d.State.Get(ctx, "repo_id")
	if err != nil {
		return "", err
	}
	var s string
	if json.Unmarshal(raw, &s) != nil || s == "" {
		return "", internalErr()
	}
	return s, nil
}

func ts(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// recordSnapshot は state.json の snapshots[] に世代を足し、健全なら protected[] の latest_healthy を新しい世代にする(spec 4.2 の世代の一覧)。
func (d *Deps) recordSnapshot(ctx context.Context, m ManifestMaterial, w ManifestWritten, now time.Time, healthy bool) error {
	held, err := d.State.Lock(ctx)
	if err != nil {
		return err
	}
	defer held.Release()
	var snaps []json.RawMessage
	if raw, gerr := d.State.Get(ctx, "snapshots"); gerr != nil {
		return gerr
	} else if len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, &snaps) != nil {
		return internalErr()
	}
	until := now.AddDate(0, 0, d.Retention.DailyDays())
	if w.Class == contract.RetentionMonthly {
		until = now.AddDate(0, d.Retention.MonthlyMonths(), 0)
	}
	rec, _ := json.Marshal(map[string]any{"ref": w.Ref, "version_id": w.Put.VersionID, "size": w.Put.Size, "etag": w.Put.ETag,
		"retain_until": ts(w.Put.RetainUntil), "created_at": ts(m.CreatedAt), "stored_at": ts(now), "retention_class": string(w.Class),
		"retention_until": ts(until), "health": w.Health, "targets_ok": healthy, "restore_verified": nil, "refs_verified": nil,
		"restore_failed": false, "def_copy_mismatch": false, "baseline": nil, "format_version": contract.FormatVersion})
	b, _ := json.Marshal(append(snaps, rec))
	if err := d.State.Update(ctx, held, "snapshots", b); err != nil {
		return err
	}
	if healthy && w.Health == "pass" {
		return d.setLatestHealthy(ctx, held, w.Ref)
	}
	return nil
}

// setLatestHealthy は protected[] の latest_healthy の理由を ref に付け替える(last_restore_test の理由は残す)。保護中の世代は高々 2 つ。
func (d *Deps) setLatestHealthy(ctx context.Context, held contract.StateLockHandle, ref string) error {
	type prot struct {
		Ref     string   `json:"ref"`
		Reasons []string `json:"reasons"`
	}
	var cur []prot
	if raw, err := d.State.Get(ctx, "protected"); err != nil {
		return err
	} else if len(raw) > 0 && string(raw) != "null" {
		_ = json.Unmarshal(raw, &cur)
	}
	next := []prot{}
	for _, p := range cur {
		rs := []string{}
		for _, r := range p.Reasons {
			if r != "latest_healthy" {
				rs = append(rs, r)
			}
		}
		if len(rs) > 0 && p.Ref != ref {
			next = append(next, prot{p.Ref, rs})
		} else if len(rs) > 0 {
			next = append(next, prot{p.Ref, rs})
		}
	}
	merged := false
	for i := range next {
		if next[i].Ref == ref {
			next[i].Reasons = append(next[i].Reasons, "latest_healthy")
			merged = true
		}
	}
	if !merged {
		next = append(next, prot{ref, []string{"latest_healthy"}})
	}
	b, _ := json.Marshal(next)
	return d.State.Update(ctx, held, "protected", b)
}

// rebuildIndex は索引を開けないときに、保管先の保持中のマニフェストを開いて Index.Rebuild を呼ぶ(spec 4.3)。
// マニフェストは snapshots[] の記録した版 ID で読む。開けない世代は飛ばし(Index が Skipped に数える)、読めるものが無ければ誤り。
func (d *Deps) rebuildIndex(ctx context.Context) error {
	if d.State == nil || d.Object == nil || d.Codec == nil || d.KeyS == nil || d.Index == nil {
		return contract.ErrNotImplemented("cli.backup")
	}
	var snaps []struct {
		Ref       string `json:"ref"`
		VersionID string `json:"version_id"`
	}
	if raw, err := d.State.Get(ctx, "snapshots"); err != nil {
		return err
	} else if len(raw) == 0 || json.Unmarshal(raw, &snaps) != nil {
		return internalErr()
	}
	bodies := [][]byte{}
	for _, s := range snaps {
		if b, err := d.openManifest(ctx, s.Ref, s.VersionID); err == nil {
			bodies = append(bodies, b)
		}
	}
	def, _, err := d.State.ReadDef(ctx)
	if err != nil {
		return err
	}
	return d.Index.Rebuild(ctx, bodies, def)
}

// openManifest はマニフェストを記録した版 ID で読み、見出しの包んだ鍵を開いて復号した本体を返す。
func (d *Deps) openManifest(ctx context.Context, ref, version string) ([]byte, error) {
	rc, err := d.Object.Get(ctx, "manifests/"+ref, version)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	sealed, err := io.ReadAll(rc)
	if err != nil {
		return nil, internalErr()
	}
	h, err := d.Codec.Header(sealed)
	if err != nil || len(h.Mark) != 4 {
		return nil, internalErr()
	}
	n := uint32(h.Mark[0])<<24 | uint32(h.Mark[1])<<16 | uint32(h.Mark[2])<<8 | uint32(h.Mark[3])
	key, err := d.KeyS.OpenSnapshotKey(ctx, h.Wrapped, "k"+uitoa(n), ref)
	if err != nil {
		return nil, err
	}
	defer wipe(key)
	return d.Codec.Decode(contract.MagicManifest, strings.TrimPrefix(ref, "m-"), sealed, key)
}

func uitoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
