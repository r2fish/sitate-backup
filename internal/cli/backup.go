package cli

// backup の入口(spec 7.5 の backup の流れの 1: 自己診断 → 取得の入口)と、取得の結果の対象ごとの
// errors[]・warnings[] を journal/ の run_end の targets[] へ移す処理(spec 3.2.2、4.5。作業指示書 5.17 の完了条件 17・19)。
// マニフェストの書き込み(流れの 2〜5)は取得の入口の結線(完了条件 24)で足す。

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func init() { routeOwn["backup"] = backupEntry }

type extraKey struct{}

// runExtra は本体の入口から run_end の項目へ渡す追加の欄(backup の targets[])。
type runExtra struct {
	mu      sync.Mutex
	targets []map[string]any
}

func (x *runExtra) setTargets(t []map[string]any) { x.mu.Lock(); x.targets = t; x.mu.Unlock() }
func (x *runExtra) get() []map[string]any         { x.mu.Lock(); defer x.mu.Unlock(); return x.targets }

func extraOf(ctx context.Context) *runExtra {
	x, _ := ctx.Value(extraKey{}).(*runExtra)
	return x
}

// ackItem は run_end の targets[] の errors[]・warnings[] の要素。code と item_ref だけを写す(path・path_b64 は写さない)。
func ackItems(v any) ([]map[string]any, int) {
	arr, _ := v.([]any)
	out := []map[string]any{}
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		ref, has := m["item_ref"]
		if !has {
			ref = nil
		}
		out = append(out, map[string]any{"code": m["code"], "item_ref": ref})
	}
	if len(out) > contract.AcquireErrorsMax {
		out = out[:contract.AcquireErrorsMax]
	}
	return out, len(arr)
}

func intOf(v any) int { f, _ := v.(float64); return int(f) }

// backupEntry は backup の入口。取得・保管の失敗はすべて backup.failed で知らせる(spec 4.8)。
// 対象がすべて failed の ALL_TARGETS_FAILED は finishResult が発行済みなので重ねない。
func backupEntry(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
	if c.DryRun { // --dry-run は実行しない(spec 3.1.1)。取得も保管も通知も行わない
		return notImpl("backup.dry-run")
	}
	if rep, skip := d.skipTimerSetup(ctx, c); skip {
		return rep, nil
	}
	rep, err := backupBody(d, ctx, c)
	if err != nil {
		if e := toError(err); notifiableBackupFailure(e.Code) {
			d.emitBackup(ctx, c.RunID, "backup.failed", string(e.Code), []string{}, nil, 0, 0, nil)
		}
	}
	return rep, err
}

// notifiableBackupFailure は backup.failed で知らせる失敗か(発行済みの ALL_TARGETS_FAILED と、実行に入る前の呼び方の誤りを除く)。
func notifiableBackupFailure(c contract.Code) bool {
	switch c {
	case contract.CodeAllTargetsFailed, contract.CodeNotImplemented, contract.CodeArgInvalid, contract.CodeLocked:
		return false
	}
	return true
}

func backupBody(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
	if d.Ingest != nil {
		return d.backupIngest(ctx, c)
	}
	if d.Acquire == nil {
		return notImpl("backup")
	}
	st, err := d.Acquire.Selftest(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	rep, err := d.Acquire.Run(ctx, c)
	if err != nil {
		return contract.Reply{}, err
	}
	var res map[string]any
	if json.Unmarshal(rep.Result, &res) != nil || res == nil {
		return contract.Reply{}, internalErr()
	}
	rep.Warnings = append(append([]contract.Warning{}, st.Warnings...), rep.Warnings...)
	return d.finishResult(ctx, c, rep, res, nil)
}

// finishResult は取得の結果 res の対象を件数にして run_end へ errors[]・warnings[] を渡し、失敗の通知を発行する。
// manifest が非 nil なら、すべて failed でなければ呼んでマニフェストを書き、結果の snapshot_ref・health を埋める。
func (d *Deps) finishResult(ctx context.Context, c contract.Call, rep contract.Reply, res map[string]any, manifest func() (ManifestWritten, error)) (contract.Reply, error) {
	targets, _ := res["targets"].([]any)
	journal := make([]map[string]any, 0, len(targets))
	failed := 0
	for i, t := range targets {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		je, failedT := normalizeTarget(m)
		journal = append(journal, je)
		if failedT {
			failed++
		}
		targets[i] = m
	}
	if x := extraOf(ctx); x != nil {
		x.setTargets(journal)
	}
	warnCodes := []string{}
	for _, w := range rep.Warnings {
		warnCodes = append(warnCodes, string(w.Code))
	}
	if len(targets) > 0 && failed == len(targets) {
		d.emitBackup(ctx, c.RunID, "backup.failed", string(contract.CodeAllTargetsFailed), warnCodes, nil, len(targets), failed, res)
		return contract.Reply{}, newErr(contract.CodeAllTargetsFailed, map[string]any{})
	}
	if manifest != nil {
		w, err := manifest()
		if err != nil {
			return contract.Reply{}, err
		}
		res["snapshot_ref"], res["health"] = w.Ref, w.Health
		if failed == 0 && w.Health == "pass" {
			d.clearSuppressed(ctx) // 健常性 pass かつ全対象 ok の世代ができたら、hold-release の再発動の抑止を終える
		}
	}
	if failed > 0 || (manifest != nil && res["health"] != "pass") || lenOf(res["skipped_backups"]) > 0 { // 飛ばした世代があれば全対象 ok でも送る(spec 4.8)
		d.emitBackup(ctx, c.RunID, "backup.partial", "", warnCodes, res["snapshot_ref"], len(targets), failed, res)
	}
	res["targets"] = targets
	out, _ := json.Marshal(res)
	rep.Result = out
	return rep, nil
}

// backupIngest は取得の入口(自己診断を含む)から、マニフェストの書き込みと世代の記録までを行う(spec 7.5 backup の流れ)。
func (d *Deps) backupIngest(ctx context.Context, c contract.Call) (contract.Reply, error) {
	o, err := d.ingest(ctx, c)
	if err != nil {
		return contract.Reply{}, err
	}
	var warns []contract.Warning
	if len(o.TargetsMismatch) > 0 {
		warns = append(warns, contract.Warning{Code: contract.CodeReceiverTargetsMismatch, Detail: mustMarshal(map[string]any{"target_ids": o.TargetsMismatch}), Next: []contract.Next{}})
	}
	res := map[string]any{"snapshot_ref": o.Material.ManifestID, "targets": resultTargets(o), "checks": []any{}, "health": "incomplete",
		"uploaded_bytes": o.Material.BytesNew, "duration_s": int64(o.Duration.Seconds()), "chunk_keys_missing": o.Material.ChunkKeysMissing}
	if len(o.Skipped) > 0 {
		res["skipped_backups"] = o.Skipped
	}
	for _, oc := range o.Outcomes {
		if oc.Empty { // 0 件の対象(全消去の後など)。対象は partial、世代は incomplete になっている
			warns = append(warns, contract.Warning{Code: contract.CodeTargetEmpty, Detail: mustMarshal(map[string]any{"target": oc.ID}), Next: []contract.Next{}})
		}
	}
	if o.Material.ChunkKeysMissing > 0 {
		warns = append(warns, contract.Warning{Code: contract.CodeChunkKeyMissing, Detail: mustMarshal(map[string]any{"count": o.Material.ChunkKeysMissing}), Next: []contract.Next{}})
	}
	rep, err := d.finishResult(ctx, c, contract.Reply{Warnings: warns}, res, func() (ManifestWritten, error) { return d.finishManifest(ctx, c, o) })
	if err == nil {
		d.autoHold(ctx, c.RunID)
	}
	return rep, err
}

// emitBackup は backup.failed / backup.partial を発行する。配送の結果は run_end の notifications[] に載る。
func (d *Deps) emitBackup(ctx context.Context, runID, typ, errCode string, warn []string, snap any, total, failed int, res map[string]any) {
	var ec any
	if errCode != "" {
		ec = errCode
	}
	var reason any
	skipped := lenOf(res["skipped_backups"])
	if typ == "backup.partial" && skipped > 0 {
		reason = "index_rebuild"
	}
	facts, _ := json.Marshal(map[string]any{"run_id": runID, "error_code": ec, "warning_codes": warn, "snapshot_ref": snap,
		"targets_total": total, "targets_failed": failed, "chunk_keys_missing": intOf(res["chunk_keys_missing"]),
		"reason": reason, "skipped_backups_count": skipped})
	_, _, _ = d.Recorder.Emit(ctx, typ, facts)
}

// lenOf は配列の要素の数(型を問わない。[]SkippedRef なども数える)。
func lenOf(v any) int {
	if v == nil {
		return 0
	}
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	var a []json.RawMessage
	_ = json.Unmarshal(b, &a)
	return len(a)
}

// normalizeTarget は結果の対象 1 件から errors[]・warnings[] を外して件数にし、run_end の targets[] の要素と failed かを返す。
func normalizeTarget(m map[string]any) (map[string]any, bool) {
	errs, nErr := ackItems(m["errors"])
	warns, nWarn := ackItems(m["warnings"])
	code := m["error_code"]
	je := map[string]any{"id": m["id"], "error_code": code, "detail": m["detail"], "next": m["next"], "errors": errs, "warnings": warns}
	delete(m, "errors")
	delete(m, "warnings")
	m["errors_omitted"] = intOf(m["errors_omitted"]) + max(0, nErr-len(errs))
	m["warnings_omitted"] = intOf(m["warnings_omitted"]) + max(0, nWarn-len(warns))
	if _, has := m["error_code"]; !has {
		m["error_code"] = nil
	}
	return je, m["status"] == "failed"
}
