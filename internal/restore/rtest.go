package restore

import (
	"context"
	"errors"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// rtState は 1 回の復元テストの途中の状態。
type rtState struct {
	r                   *Restore
	c                   contract.Call
	d                   *defDoc
	def                 []byte
	runID               string
	start               time.Time
	loc                 *time.Location
	o                   *objReader
	snap                snapRec
	man                 *manifestDoc
	snapKey             []byte
	keyringOK, configOK bool
	defCopy             *bool
	warnings            []contract.Warning
	rec                 *ledgerRec
	remote              remoteOut
	abortErr            error
	scratch             string
	readStarted         bool
	allow               []string // start の allow_ssh_from(provision が決める)
}

// abortError は取り消された文脈を RUN_ABORTED にする(deadline か signal)。
func abortError(ctx context.Context) error {
	reason := "signal"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		reason = "deadline"
	}
	return newErr(contract.CodeRunAborted, map[string]any{"reason": reason})
}

// deferred は実行しなかった結果(spec 3.1.2.6。deferred: true のとき多くの欄は null)。
func deferred(reason string, ws ...contract.Warning) (contract.Reply, error) {
	rep, err := reply(map[string]any{"backup_ref": nil, "restore_ok": nil, "keyring_ok": nil, "config_copy_ok": nil, "def_copy_ok": nil, "checks": []any{},
		"duration_s": nil, "egress_bytes": nil, "teardown": nil, "deferred": true, "deferred_reason": reason, "proof": nil})
	rep.Warnings = ws
	return rep, err
}

// deferral は前の破棄の残存(teardown_pending)と開いている復元セッション(session_open)による延期を判定する(spec 3.1.2.6 の分岐 5・10)。
func deferral(recs []ledgerRec) (string, []contract.Warning) {
	var sessions []map[string]any
	for _, rec := range recs {
		if rec.State == "destroy_failed" {
			rem := []resRef{}
			if rec.Teardown != nil {
				rem = nonNilRes(rec.Teardown.Remaining)
			}
			sessions = append(sessions, map[string]any{"session_id": rec.SessionID, "remaining": rem})
		}
	}
	if len(sessions) > 0 {
		return "teardown_pending", []contract.Warning{warn(contract.CodeTeardownUnverified, map[string]any{"sessions": sessions})}
	}
	for _, rec := range recs {
		if openSession(rec) {
			return "session_open", []contract.Warning{warn(contract.CodeRestoreSessionOpen, map[string]any{"session_id": rec.SessionID})}
		}
	}
	return "", nil
}

// RestoreTest は月 1 回の復元テスト(spec 3.5、3.1.2.6)。バックアップサーバーの状態(索引、前回のマニフェスト)を使わず、
// 保管先 + 対象の世代のスナップショット鍵と読み取り鍵だけで、復元先に復旧キットと同じ経路の restore を実行させて照合する。
func (r *Restore) RestoreTest(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if err := r.coreMissing(); err != nil {
		return contract.Reply{}, err
	}
	if c.DryRun { // --dry-run は実行しない(spec 3.1.1)。復元先も保管先の読み出しも記録も動かさない
		return contract.Reply{}, contract.ErrNotImplemented("restore.Restore.RestoreTest.dry-run")
	}
	if c.RunID == "" {
		return contract.Reply{}, internalErr("")
	}
	if ctx.Err() != nil {
		return contract.Reply{}, abortError(ctx)
	}
	d, def, err := r.readDef(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	recs, err := readLedger(ctx, r.State)
	if err != nil {
		return contract.Reply{}, err
	}
	if recs, err = r.retryDestroyFailed(ctx, recs); err != nil { // 前の破棄の残存は、始めに 1 回だけ呼び直す(消せれば続け、残れば下で deferred)
		return contract.Reply{}, err
	}
	if reason, ws := deferral(recs); reason != "" {
		r.markDeferred(ctx, reason)
		(&rtState{r: r, runID: c.RunID}).notifyDeferred(ctx, reason)
		return deferred(reason, ws...)
	}
	now, err := r.Clock.Now(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	t := &rtState{r: r, c: c, d: d, def: def, runID: c.RunID, start: now, loc: defTimezone(def), o: &objReader{store: r.Store, codec: r.Codec, keyS: r.KeyS}}
	return t.run(ctx)
}

// run は表の 1〜12 を順に行う。失敗は finishFail で破棄と記録を済ませて返す。
func (t *rtState) run(ctx context.Context) (contract.Reply, error) {
	if err := t.checkKeyring(ctx); err != nil {
		return t.finishFail(ctx, err)
	}
	snaps, err := loadSnapshots(ctx, t.r.State)
	if err != nil {
		return t.finishFail(ctx, err)
	}
	s, ok := latestRef(snaps)
	if !ok {
		return t.finishFail(ctx, newErr(contract.CodeNoHealthyBackup, nil))
	}
	t.snap = s
	if t.man, t.snapKey, err = t.o.manifest(ctx, s.Ref, s.VersionID, nil); err != nil {
		return t.finishFail(ctx, err)
	}
	t.keyringOK = true
	t.configOK = t.checkConfig(ctx)
	t.defCopy = t.checkDefCopy(ctx)
	if err := t.provision(ctx); err != nil {
		var de *deferErr
		if errors.As(err, &de) {
			t.r.markDeferred(context.WithoutCancel(ctx), de.reason)
			t.notifyDeferred(context.WithoutCancel(ctx), de.reason)
			return deferred(de.reason, de.ws...)
		}
		return t.finishFail(ctx, err)
	}
	if err := t.remoteRestore(ctx); err != nil {
		return t.finishFail(ctx, err)
	}
	return t.finishOK(ctx)
}
