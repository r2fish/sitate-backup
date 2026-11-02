package restore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// openSession は開いている復元セッション(片付けの対象にしない。spec 3.5。復元セッションはイテレーション 4 以降)。
func openSession(r ledgerRec) bool { return r.Purpose == "restore_session" && r.State == "created" }

// markFailed は破棄を確かめられなかった記録を destroy_failed にして残存を書く(最初に確かめられなかった時刻は変えない)。
func markFailed(ctx context.Context, st contract.StateFiles, rec ledgerRec, tr teardownResult, now time.Time) (ledgerRec, error) {
	first := tsz(now)
	if rec.Teardown != nil && rec.Teardown.FirstFailedAt != "" {
		first = rec.Teardown.FirstFailedAt
	}
	rem := tr.Remaining
	if len(rem) == 0 {
		rem = rec.Resources
	}
	rec.State, rec.Teardown = "destroy_failed", &teardownRec{Remaining: nonNilRes(rem), FirstFailedAt: first}
	return rec, ledgerPut(ctx, st, rec)
}

// Cleanup は台帳に残る復元先の資源を片付ける(spec 3.5「中止の後の破棄」)。対象は creating・created の記録(開いている復元セッションを除く)。
// 中断した実行の run_id(interrupted)が分からないとき(空)は destroy_failed の記録も対象にする。own の記録(同じ run_id の前の試み)も対象。
// 片付けた資源と、確かめられず残った資源を返す。確かめられなければ TEARDOWN_UNVERIFIED を呼び出し側が警告にする。
func (c *RunCleanup) Cleanup(ctx context.Context, own string, interrupted string) (cleaned, unverified []contract.Resource, err error) {
	cleaned, unverified = []contract.Resource{}, []contract.Resource{}
	if c == nil || c.State == nil || c.Adapters == nil {
		return cleaned, unverified, internalErr("")
	}
	recs, err := readLedger(ctx, c.State)
	if err != nil {
		return cleaned, unverified, err
	}
	for _, rec := range recs {
		if openSession(rec) || !(rec.State == "creating" || rec.State == "created" || (interrupted == "" && rec.State == "destroy_failed")) {
			continue
		}
		tr := teardownSession(ctx, c.Adapters, c.Secrets, rec)
		if tr.Absent {
			if err := ledgerDelete(ctx, c.State, rec.RecordID); err != nil {
				return cleaned, unverified, err
			}
			cleaned = append(cleaned, toResources(rec.Resources)...)
			continue
		}
		upd, err := markFailed(ctx, c.State, rec, tr, time.Now().UTC())
		if err != nil {
			return cleaned, unverified, err
		}
		unverified = append(unverified, toResources(upd.Teardown.Remaining)...)
	}
	return cleaned, unverified, nil
}

// sweep は掃引(spec 3.2.4): inspect(session_id: null)で列挙した資源を session_id ごとに分け、台帳に記録の無い session_id だけ
// 記録を足して破棄する。記録のある session_id には新しい記録を足さず、資源を足すだけにする。
func sweep(ctx context.Context, ar contract.AdapterRunner, st contract.StateFiles, sec contract.Secrets, runID, adapter string, dest destRef, fp string, now time.Time) error {
	var fpArg any
	if fp != "" {
		fpArg = fp
	}
	raw, err := destCall(ctx, ar, runID, adapter, map[string]any{"op": "inspect", "session_id": nil, "dest": dest, "host_key_fp": fpArg, "resources": []resRef{},
		"secrets": map[string]string{"restore-dest-ssh": sshSecretKey(ctx, sec)}})
	if err != nil {
		return err
	}
	var res struct {
		Resources []struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			SessionID string `json:"session_id"`
		} `json:"resources"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return internalErr("output")
	}
	for _, r := range res.Resources {
		if r.SessionID == "" { // session_id の無い資源は、親の作業ディレクトリを指し得るので破棄しない。残存として表の 3 を行わない
			return internalErr("output")
		}
	}
	recs, err := readLedger(ctx, st)
	if err != nil {
		return err
	}
	known := map[string]*ledgerRec{}
	for i := range recs {
		known[recs[i].SessionID] = &recs[i]
	}
	by := map[string][]resRef{}
	var order []string
	for _, r := range res.Resources {
		if _, ok := by[r.SessionID]; !ok {
			order = append(order, r.SessionID)
		}
		by[r.SessionID] = append(by[r.SessionID], resRef{Type: r.Type, ID: r.ID})
	}
	for _, sid := range order {
		if rec, ok := known[sid]; ok {
			rec.Resources = mergeRes(rec.Resources, by[sid])
			if err := ledgerPut(ctx, st, *rec); err != nil {
				return err
			}
			continue
		}
		if err := sweepOrphan(ctx, ar, st, sec, adapter, dest, fp, sid, by[sid], now); err != nil {
			return err
		}
	}
	return nil
}

// sweepOrphan は台帳に記録の無い session_id の資源の記録を足して破棄する。
func sweepOrphan(ctx context.Context, ar contract.AdapterRunner, st contract.StateFiles, sec contract.Secrets, adapter string, dest destRef, fp, sid string, res []resRef, now time.Time) error {
	host, port, _ := parseDestAt(dest.At)
	var fpp *string
	if fp != "" {
		fpp = &fp
	}
	rec := ledgerRec{RecordID: newRecordID(), Kind: "dest_session", RunID: sid, Adapter: adapter, Dest: dest, Purpose: "restore_test", SessionID: sid,
		State: "created", Resources: res, SSH: sshRef{Host: host, Port: port, User: restoreUser}, HostKeyFP: fpp}
	if err := ledgerPut(ctx, st, rec); err != nil {
		return err
	}
	if tr := teardownSession(ctx, ar, sec, rec); tr.Absent {
		return ledgerDelete(ctx, st, rec.RecordID)
	} else if _, err := markFailed(ctx, st, rec, tr, now); err != nil {
		return err
	}
	return nil
}

func mergeRes(a, b []resRef) []resRef {
	seen := map[resRef]bool{}
	out := []resRef{}
	for _, r := range append(append([]resRef{}, a...), b...) {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}
