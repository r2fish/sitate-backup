package keys

// 台帳(ledger/)の issued_key の記録(spec 3.2.6、4.5)。同じ key_id は後の行が前の行を上書きする。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// ledgerKey は台帳の issued_key の記録(同じ key_id は後の行が前の行を上書きする)。
type ledgerKey struct {
	KeyID     string  `json:"key_id"`
	Role      *string `json:"role"`
	RevokedAt *string `json:"revoked_at"`
	Orphaned  bool    `json:"orphaned"`
}

// ledgerKeys は台帳の issued_key を key_id ごとの最後の行で返す(出現順)。
func (k *Keys) ledgerKeys(ctx context.Context) (order []string, byID map[string]ledgerKey, err error) {
	rows, err := k.State.LedgerList(ctx)
	if err != nil {
		return nil, nil, err
	}
	byID = map[string]ledgerKey{}
	for _, r := range rows {
		var h struct {
			Kind string `json:"kind"`
		}
		var lk ledgerKey
		if json.Unmarshal(r, &h) != nil || h.Kind != "issued_key" || json.Unmarshal(r, &lk) != nil || lk.KeyID == "" {
			continue
		}
		prev, seen := byID[lk.KeyID]
		if !seen {
			order = append(order, lk.KeyID)
		}
		byID[lk.KeyID] = mergeLedger(prev, lk)
	}
	return order, byID, nil
}

// ledgerPut は issued_key の記録を 1 行追記する(失効や孤児の印は新しい行で上書きする)。
func (k *Keys) ledgerPut(ctx context.Context, runID, adapter string, lk ledgerKey) error {
	rid := make([]byte, 8)
	if _, err := rand.Read(rid); err != nil {
		return errInternal()
	}
	row, err := json.Marshal(map[string]any{"record_id": "l-" + hex.EncodeToString(rid), "kind": "issued_key", "run_id": runID, "adapter": adapter,
		"role": lk.Role, "key_id": lk.KeyID, "revoked_at": lk.RevokedAt, "orphaned": lk.Orphaned})
	if err != nil {
		return errInternal()
	}
	return k.withLock(ctx, func(h contract.StateLockHandle) error { return k.State.LedgerAppend(ctx, h, row) })
}

func strp(s string) *string { return &s }

// recordOrphans は失効にも失敗した鍵を台帳に orphaned: true で記録する(写す先のコードを問わず)。
func (k *Keys) recordOrphans(ctx context.Context, ic *isCtx, ids []string) {
	for _, id := range ids {
		ic.noteLedger(k.ledgerPut(ctx, ic.c.RunID, ic.name, ledgerKey{KeyID: id, Orphaned: true}))
	}
}

// noteLedger は台帳を読めない・書けないことを警告 KEY_RECONCILE_UNVERIFIED(reason: LEDGER_UNAVAILABLE)にする(1 回だけ)。
func (ic *isCtx) noteLedger(err error) {
	if err == nil || ic.ledgerWarned {
		return
	}
	ic.ledgerWarned = true
	ic.warns = append(ic.warns, mkWarn(contract.CodeKeyReconcileUnverified, map[string]string{"reason": "LEDGER_UNAVAILABLE"}))
}

// markRevoked は失効した鍵の台帳の記録を失効の時刻つきで追記する。
func (k *Keys) markRevoked(ctx context.Context, ic *isCtx, ids []string, orphaned bool) {
	now, err := k.now(ctx)
	if err != nil {
		return
	}
	_, led, lerr := k.ledgerKeys(ctx)
	ic.noteLedger(lerr)
	for _, id := range ids {
		lk := led[id]
		lk.KeyID, lk.RevokedAt = id, strp(fmtTime(now))
		lk.Orphaned = lk.Orphaned || orphaned
		ic.noteLedger(k.ledgerPut(ctx, ic.c.RunID, ic.name, lk))
	}
}

// mergeLedger は同じ key_id の 2 行を、行の並びに依らない形で合成する(LedgerList の順序は契約に無い):
// revoked_at は非 null を優先(両方あれば新しい時刻)、orphaned は OR、role は非 null を優先。
func mergeLedger(a, b ledgerKey) ledgerKey {
	out := b
	out.Orphaned = a.Orphaned || b.Orphaned
	if out.Role == nil {
		out.Role = a.Role
	}
	switch {
	case a.RevokedAt == nil:
	case b.RevokedAt == nil || *a.RevokedAt > *b.RevokedAt:
		out.RevokedAt = a.RevokedAt
	}
	return out
}
