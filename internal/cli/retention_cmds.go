package cli

// verify・extend・prune(spec 3.1.2。作業指示書 5.18)。保持の判断(何を延ばす・何を消す・何が欠けているか)は store(U1b)が持ち、
// cli は結果の JSON の形、通知(spec 4.8)、警告への写しだけを持つ。prune に任意の版を指定する引数は無い(gate からも渡せない)。

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func init() {
	routeOwn["verify"] = verifyEntry
	routeOwn["extend"] = extendEntry
	routeOwn["prune"] = pruneEntry
}

func timeStr(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func objverList(refs []contract.VersionRef) []map[string]any {
	out := make([]map[string]any, 0, len(refs))
	for _, r := range refs {
		out = append(out, map[string]any{"key": r.Key, "version_id": r.VersionID})
	}
	return out
}

func diffList(ms []contract.RetentionMismatch) []map[string]any {
	out := make([]map[string]any, 0, len(ms))
	for _, m := range ms {
		out = append(out, map[string]any{"key": m.Key, "version_id": m.VersionID, "recorded": timeStr(m.Recorded), "actual": timeStr(m.Actual)})
	}
	return out
}

// eventRunID は通知の run_id。ロックを取らない verify は --run-id が無ければここで作る。
func (d *Deps) eventRunID(ctx context.Context, c contract.Call) string {
	if c.RunID != "" {
		return c.RunID
	}
	id, _ := d.newRunID(ctx)
	return id
}

func verifyEntry(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
	if d.Store == nil || d.State == nil || d.Index == nil || d.Recorder == nil {
		return notImpl("verify")
	}
	runID := d.eventRunID(ctx, c)
	warns := d.pendingChainWarnings(ctx)
	recorded, err := d.recordedRefs(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	res, err := d.Store.Verify(ctx, recorded)
	if err != nil {
		return contract.Reply{}, err
	}
	chain, err := d.journalChain(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	warns = append(warns, d.verifySignals(ctx, runID, res, recorded, chain)...)
	if len(res.UnexpectedVersions) > 0 {
		_ = d.emit(ctx, "storage.unexpected_version", map[string]any{"run_id": runID, "count": len(res.UnexpectedVersions), "previous": nil})
		return contract.Reply{}, newErr(contract.CodeUnexpectedVersion, map[string]any{"objects": objverList(res.UnexpectedVersions)})
	}
	if len(res.Missing) == 0 && len(res.RetentionShort) == 0 {
		d.markRefsVerified(ctx, runID, recorded)
	}
	b, _ := json.Marshal(map[string]any{
		"missing": objverList(res.Missing), "retention_short": diffList(res.RetentionShort), "retention_long": diffList(res.RetentionLong),
		"unexpected_versions": objverList(res.UnexpectedVersions), "noncurrent_versions": res.NoncurrentVersions, "delete_markers": res.DeleteMarkers,
		"bytes_written_vs_bucket": res.BytesWrittenVsBucket, "journal_chain_ok": chain.ok, "sampled": res.Sampled,
	})
	return contract.Reply{Result: b, Warnings: warns}, nil
}

// verifySignals は通知(spec 4.8)と警告(spec 3.1.3)を出す。
func (d *Deps) verifySignals(ctx context.Context, runID string, res contract.VerifyResult, recorded []contract.VersionRef, chain journalChainState) []contract.Warning {
	warns := []contract.Warning{}
	if len(res.Missing) > 0 || len(res.RetentionShort) > 0 || res.DeleteMarkers > 0 {
		_ = d.emit(ctx, "verify.failed", map[string]any{"run_id": runID, "missing_count": len(res.Missing), "retention_short_count": len(res.RetentionShort), "delete_markers": res.DeleteMarkers})
	}
	if res.DeleteMarkers > 0 {
		warns = append(warns, contract.Warning{Code: contract.CodeDeleteMarkerFound, Detail: mustMarshal(map[string]any{"count": res.DeleteMarkers}), Next: []contract.Next{}})
	}
	if n := len(res.RetentionLong); n > 0 {
		var latest time.Time
		for _, m := range res.RetentionLong {
			if m.Actual.After(latest) {
				latest = m.Actual
			}
		}
		_ = d.emit(ctx, "storage.retention_long", map[string]any{"count": n, "latest_actual": timeStr(latest)})
		warns = append(warns, contract.Warning{Code: contract.CodeRetentionLong, Detail: mustMarshal(map[string]any{"retention_long": diffList(res.RetentionLong)}), Next: []contract.Next{}})
	}
	var recBytes float64
	for _, r := range recorded {
		recBytes += float64(r.Size)
	}
	if res.BytesWrittenVsBucket > 1+contract.SizeDriftRatio {
		f := map[string]any{"written_bytes": int64(recBytes), "bucket_bytes": int64(math.Round(recBytes * res.BytesWrittenVsBucket)), "threshold_bytes": int64(math.Floor(recBytes * (1 + contract.SizeDriftRatio)))}
		_ = d.emit(ctx, "storage.size_drift", f)
		warns = append(warns, contract.Warning{Code: contract.CodeStorageSizeDrift, Detail: mustMarshal(f), Next: []contract.Next{}})
	}
	if !chain.ok {
		_ = d.emit(ctx, "journal.chain_broken", map[string]any{"chain": "journal", "command": []string{"verify"}, "kind": chain.kind, "seq": chain.seq, "discarded": []string{}})
		warns = append(warns, contract.Warning{Code: contract.CodeJournalChainBroken, Detail: mustMarshal(map[string]any{"kind": chain.kind, "seq": chain.seq, "keys": []string{}}), Next: []contract.Next{}})
	}
	return warns
}

func extendEntry(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
	if c.DryRun { // --dry-run の plan(延長の見込み)は Retention の契約に無い。実行はしない
		return notImpl("extend.dry-run")
	}
	if d.Store == nil || d.State == nil || d.Recorder == nil {
		return notImpl("extend")
	}
	lastOK := d.lastExtendOK(ctx)
	var total contract.ExtendResult
	failed := []map[string]any{}
	var first contract.Code
	for _, target := range []string{"A", "B"} { // 保持中の参照(目標 A)→ 保護中の世代と共有物(目標 B)
		r, err := d.Store.Extend(ctx, target)
		if err != nil {
			return contract.Reply{}, err
		}
		total.Extended += r.Extended
		if !r.MinRetainUntil.IsZero() && (total.MinRetainUntil.IsZero() || r.MinRetainUntil.Before(total.MinRetainUntil)) {
			total.MinRetainUntil = r.MinRetainUntil
		}
		for _, f := range r.Failed {
			if first == "" {
				first = f.Code
			}
			failed = append(failed, map[string]any{"key": f.Key, "version_id": f.VersionID, "code": f.Code})
		}
	}
	if len(failed) > 0 {
		d.emitExtendFailed(ctx, d.eventRunID(ctx, c), len(failed), first, total.MinRetainUntil, lastOK)
	}
	b, _ := json.Marshal(map[string]any{"extended": total.Extended, "failed": failed, "min_retain_until": timeStr(total.MinRetainUntil)})
	return contract.Reply{Result: b}, nil
}

// lastExtendOK は今回より前に最後に成功した extend の時刻(無ければゼロ)。store が state.json の last_extend_ok_at に書く。
func (d *Deps) lastExtendOK(ctx context.Context) time.Time {
	raw, err := d.State.Get(ctx, "last_extend_ok_at")
	var s string
	if err != nil || json.Unmarshal(raw, &s) != nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func (d *Deps) emitExtendFailed(ctx context.Context, runID string, n int, code contract.Code, minRetain, lastOK time.Time) {
	if minRetain.IsZero() {
		minRetain, _ = d.Clock.Now(ctx)
	}
	var ends any
	if !lastOK.IsZero() {
		ends = timeStr(lastOK.AddDate(0, 0, contract.RetentionDailyDays))
	}
	_ = d.emit(ctx, "retention.extension_failed", map[string]any{"run_id": runID, "failed_count": n, "error_code": code,
		"min_retain_until": timeStr(minRetain), "last_extend_ok_at": timeStr(lastOK), "protection_ends_at": ends})
}

func pruneEntry(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
	if c.DryRun { // --dry-run の plan(消す見込み)は Retention の契約に無い。実行はしない
		return notImpl("prune.dry-run")
	}
	if d.Store == nil {
		return notImpl("prune")
	}
	res, err := d.Store.Prune(ctx)
	var warns []contract.Warning
	if err != nil {
		var ce *contract.Error
		if !errors.As(err, &ce) || ce.Code != contract.CodeNotSealed {
			return contract.Reply{}, err
		}
		// seal の前は何も消さず警告 NOT_SEALED(spec 3.1.2 の prune の行)
		res, warns = contract.PruneResult{}, []contract.Warning{{Code: contract.CodeNotSealed, Detail: json.RawMessage(`{}`), Next: []contract.Next{}}}
	}
	b, _ := json.Marshal(map[string]any{"deleted": res.Deleted, "skipped_locked": res.SkippedLocked, "protected": res.Protected, "held": res.Held})
	return contract.Reply{Result: b, Warnings: warns}, nil
}
