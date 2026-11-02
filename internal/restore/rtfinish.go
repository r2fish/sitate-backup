package restore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// teardown は表の 11: 成否と中止にかかわらず、台帳の所在で stop / destroy と inspect を呼ぶ。取り消された文脈とは別の締め切りを持つ文脈で呼ぶ。
// 全資源 absent のときだけ台帳の記録を消す。確かめられなければ記録を destroy_failed にして残す。
func (t *rtState) teardown(ctx context.Context) *teardownResult {
	if t.rec == nil {
		return nil
	}
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), contract.RunAbortGrace)
	defer cancel()
	tr := teardownSession(c, t.r.Adapters, t.r.Secrets, *t.rec)
	if tr.Absent {
		_ = ledgerDelete(c, t.r.State, t.rec.RecordID)
	} else {
		_, _ = markFailed(c, t.r.State, *t.rec, tr, t.start)
	}
	return &tr
}

// finishFail は失敗の後始末: 破棄、復元できなかった印、記録、通知。err をそのまま返す。
func (t *rtState) finishFail(ctx context.Context, err error) (contract.Reply, error) {
	if ctx.Err() != nil {
		var ce *contract.Error
		if !errors.As(err, &ce) || ce.Code != contract.CodeRunAborted {
			err = abortError(ctx) // 取り消しは中止として記録する
		}
	}
	tr := t.teardown(ctx)
	var ce *contract.Error
	code := contract.Code("")
	if errors.As(err, &ce) {
		code = ce.Code
	}
	if t.readStarted && t.snap.Ref != "" && (code == contract.CodeRestoreMismatch || decryptObject(ce) != "") {
		t.markRestoreFailed(context.WithoutCancel(ctx), code)
	}
	t.record(context.WithoutCancel(ctx), false, code, nil, tr)
	return contract.Reply{}, err
}

// decryptObject は DECRYPT_FAILED のうち復元できなかった印の原因になる object(manifest・entries・key_table)。それ以外は空。
func decryptObject(ce *contract.Error) string {
	if ce == nil || ce.Code != contract.CodeDecryptFailed {
		return ""
	}
	var d struct {
		Object string `json:"object"`
	}
	_ = json.Unmarshal(ce.Detail, &d)
	switch d.Object {
	case "manifest", "entries", "key_table":
		return d.Object
	}
	return ""
}

// markRestoreFailed は世代に「復元できなかった」印を付け(snapshots[]、journal の restore_failed)、削除を自動停止する(spec 3.5)。
func (t *rtState) markRestoreFailed(ctx context.Context, code contract.Code) {
	if h, err := t.r.State.Lock(ctx); err == nil {
		_ = updateSnapshots(ctx, t.r.State, h, t.snap.Ref, func(m map[string]any) { m["restore_failed"] = true })
		h.Release()
	}
	body, _ := json.Marshal(map[string]any{"kind": "restore_failed", "run_id": t.runID, "body": map[string]any{"refs": []string{t.snap.Ref}, "code": code}})
	_, _ = t.r.Recorder.Record(ctx, body)
	if t.r.Retention != nil {
		c := code
		_, _ = t.r.Retention.HoldSet(ctx, contract.HoldInput{Reason: "restore_failed", Code: &c, Refs: []string{t.snap.Ref}})
	}
}

func (t *rtState) warningCodes() []string {
	out := []string{}
	for _, w := range t.warnings {
		out = append(out, string(w.Code))
	}
	return out
}

// record は表の 12: journal の restore_test の項目、状態(last_restore_test・読み出し量の累計)、通知。合否は 1〜10 で決まる。
func (t *rtState) record(ctx context.Context, ok bool, code contract.Code, proof json.RawMessage, tr *teardownResult) {
	if tr != nil && !tr.Absent {
		t.warnings = append(t.warnings, warn(contract.CodeTeardownUnverified, map[string]any{"sessions": []map[string]any{{"session_id": t.runID, "remaining": nonNilRes(tr.Remaining)}}}))
	}
	var codeV, proofV, defV, refV any
	if code != "" {
		codeV = string(code)
	}
	if proof != nil {
		proofV = proof
	}
	if t.defCopy != nil {
		defV = *t.defCopy
	}
	if t.snap.Ref != "" {
		refV = t.snap.Ref
	}
	read := t.remote.ReadBytes + t.o.read // 復元先が保管先から読んだ量 + 核の側の読み出し(キーリング・config・マニフェスト)
	body, _ := json.Marshal(map[string]any{"kind": "restore_test", "run_id": t.runID, "body": map[string]any{"backup_ref": refV, "ok": ok, "code": codeV,
		"counted_as_restore_test": false, "read_bytes": read, "def_copy_ok": defV, "checks": []any{}, "proof": proofV}})
	_, _ = t.r.Recorder.Record(ctx, body)
	t.updateState(ctx, ok, read, proof)
	typ := "restore_test.failed"
	if ok {
		typ = "restore_test.passed"
	}
	facts, _ := json.Marshal(map[string]any{"backup_ref": refV, "error_code": codeV, "days_since_last_ok": t.daysSinceOK(ctx), "counted_as_restore_test": false,
		"teardown_pending": tr != nil && !tr.Absent, "warning_codes": t.warningCodes()})
	_, _, _ = t.r.Recorder.Emit(ctx, typ, facts)
	if tr != nil && !tr.Absent {
		uf, _ := json.Marshal(map[string]any{"sessions": []map[string]any{{"session_id": t.runID, "remaining": nonNilRes(tr.Remaining)}}, "remaining_count": len(tr.Remaining)})
		_, _, _ = t.r.Recorder.Emit(ctx, "teardown.unverified", uf)
	}
}

// notifyDeferred は延期の通知 restore_test.deferred(spec 4.8)。
func (t *rtState) notifyDeferred(ctx context.Context, reason string) {
	var ref any
	if t.snap.Ref != "" {
		ref = t.snap.Ref
	}
	facts, _ := json.Marshal(map[string]any{"reason": reason, "backup_ref": ref})
	_, _, _ = t.r.Recorder.Emit(ctx, "restore_test.deferred", facts)
}

type lastRT struct {
	At                  string   `json:"at"`
	OK                  bool     `json:"ok"`
	BackupRef           string   `json:"backup_ref"`
	LastOKAt            *string  `json:"last_ok_at"`
	ConsecutiveFailures int      `json:"consecutive_failures"`
	Deferred            bool     `json:"deferred"`
	DeferredReason      *string  `json:"deferred_reason"`
	WarningCodes        []string `json:"warning_codes"`
}

func (t *rtState) daysSinceOK(ctx context.Context) any {
	var l lastRT
	if has, _ := item(ctx, t.r.State, "last_restore_test", &l); !has || l.LastOKAt == nil {
		return nil
	}
	at, err := time.Parse(time.RFC3339, *l.LastOKAt)
	if err != nil {
		return nil
	}
	return int(t.start.Sub(at).Hours() / 24)
}

// updateState は state.lock の下で last_restore_test・storage_reads・restore_test_plan・世代の印を書く。
func (t *rtState) updateState(ctx context.Context, ok bool, read int64, proof json.RawMessage) {
	h, err := t.r.State.Lock(ctx)
	if err != nil {
		return
	}
	defer h.Release()
	st := t.r.State
	var l lastRT
	has, _ := item(ctx, st, "last_restore_test", &l)
	ref := t.snap.Ref
	if ref == "" && has {
		ref = l.BackupRef
	}
	if ref != "" {
		n := tsz(t.start)
		next := lastRT{At: n, OK: ok, BackupRef: ref, WarningCodes: t.warningCodes()}
		if has {
			next.LastOKAt = l.LastOKAt
		}
		if ok {
			next.LastOKAt, next.ConsecutiveFailures = &n, 0
		} else {
			next.ConsecutiveFailures = l.ConsecutiveFailures + 1
		}
		b, _ := json.Marshal(next)
		_ = st.Update(ctx, h, "last_restore_test", b)
	}
	t.updatePlan(ctx, h, ok)
	if ok {
		if proof != nil {
			_ = st.Update(ctx, h, "last_proof", proof)
		}
		_ = updateSnapshots(ctx, st, h, t.snap.Ref, func(m map[string]any) {
			m["restore_verified"] = true
			if t.defCopy != nil && !*t.defCopy {
				m["def_copy_mismatch"] = true
			}
		})
	}
}

// updatePlan は restore_test_plan(計画の期間の合格と読み出しの精算済みの印)を書く。
func (t *rtState) updatePlan(ctx context.Context, h contract.StateLockHandle, ok bool) {
	p := map[string]any{"read_started": nil, "deferred_month": nil}
	_, _ = item(ctx, t.r.State, "restore_test_plan", &p)
	p["period_start"] = t.start.In(t.loc).Format("2006-01") + "-01"
	if ok {
		p["passed"] = true
	} else if _, has := p["passed"]; !has {
		p["passed"] = false
	}
	for _, k := range []string{"failed_after_read", "read_settled"} { // 読み出し量の累計と予約はイテレーション 3
		if _, has := p[k]; !has {
			p[k] = map[string]any{"failed_after_read": false}[k]
		}
	}
	b, _ := json.Marshal(p)
	_ = t.r.State.Update(ctx, h, "restore_test_plan", b)
}

// markDeferred は延期を last_restore_test に残す(あれば。deferred・deferred_reason だけを変える)。
func (r *Restore) markDeferred(ctx context.Context, reason string) {
	var l lastRT
	if has, _ := item(ctx, r.State, "last_restore_test", &l); !has {
		return
	}
	h, err := r.State.Lock(ctx)
	if err != nil {
		return
	}
	defer h.Release()
	l.Deferred, l.DeferredReason = true, &reason
	b, _ := json.Marshal(l)
	_ = r.State.Update(ctx, h, "last_restore_test", b)
}

// signProof は合格記録 sitate.restore-proof/v1 を作って署名する(spec 4.5)。署名は sig を除いた記録の RFC 8785 直列化に掛ける。
func (t *rtState) signProof(ctx context.Context) (json.RawMessage, error) {
	var repo string
	if has, err := item(ctx, t.r.State, "repo_id", &repo); err != nil || !has {
		return nil, internalErr("")
	}
	rec := map[string]any{"repo_id": repo, "backup_ref": t.snap.Ref, "stored_at": t.snap.StoredAt, "retention_class": t.snap.RetentionClass,
		"retention_until": t.snap.RetentionUntil, "key_id": t.signingKeyID(ctx)}
	body, _ := json.Marshal(rec)
	canon, err := contract.CanonicalJSON(body)
	if err != nil {
		return nil, internalErr("")
	}
	key, err := t.r.KeyS.DerivedKey("proof")
	if err != nil {
		return nil, err
	}
	sig, err := t.r.Crypto.SignProof(canon, key)
	if err != nil {
		return nil, err
	}
	rec["sig"] = base64.StdEncoding.EncodeToString(sig)
	return json.Marshal(rec)
}

// signingKeyID は署名鍵を導いたマスター鍵の鍵 ID(キーリングの鍵のうち番号が最大のもの。無ければマニフェストの鍵 ID)。
func (t *rtState) signingKeyID(ctx context.Context) string {
	var lpv phraseVerify
	best, bestN := t.man.KeyID, -1
	if has, _ := item(ctx, t.r.State, "last_phrase_verify", &lpv); has {
		for _, k := range lpv.KeyIDs {
			if n, err := strconv.Atoi(strings.TrimPrefix(k, "k")); err == nil && n > bestN {
				best, bestN = k, n
			}
		}
	}
	return best
}

// finishOK は合格の後始末: 破棄、合格記録、記録、結果。破棄の失敗は合否を変えず警告にする。
func (t *rtState) finishOK(ctx context.Context) (contract.Reply, error) {
	tr := t.teardown(ctx)
	proof, err := t.signProof(context.WithoutCancel(ctx))
	if err != nil {
		t.record(context.WithoutCancel(ctx), false, contract.CodeInternal, nil, tr)
		return contract.Reply{}, err
	}
	t.record(context.WithoutCancel(ctx), true, "", proof, tr)
	end, _ := t.r.Clock.Now(ctx)
	var defV any
	if t.defCopy != nil {
		defV = *t.defCopy
	}
	var td any
	if tr != nil {
		td = map[string]any{"done": tr.Absent, "verified_by": tr.VerifiedBy, "remaining": nonNilRes(tr.Remaining)}
	}
	rep, err := reply(map[string]any{"backup_ref": t.snap.Ref, "restore_ok": true, "keyring_ok": t.keyringOK, "config_copy_ok": t.configOK, "def_copy_ok": defV,
		"checks": []any{}, "duration_s": int64(end.Sub(t.start).Seconds()), "egress_bytes": t.remote.ReadBytes + t.o.read, "teardown": td, "deferred": false, "deferred_reason": nil, "proof": proof})
	rep.Warnings = t.warnings
	return rep, err
}
