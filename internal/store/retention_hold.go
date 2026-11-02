package store

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// holdReasons は state.json の hold.reason・causes[].reason の列挙(spec 4.2「削除停止の状態と判定」)。
var holdReasons = []string{"check_failed", "check_error", "no_healthy_backup", "restore_failed", "manual"}

type holdCause struct {
	Reason string   `json:"reason"`
	Code   *string  `json:"code"`
	At     string   `json:"at"`
	Days   *int     `json:"days"`
	Refs   []string `json:"refs"`
}

type holdState struct {
	Active     bool            `json:"active"`
	Since      *string         `json:"since"`
	Auto       bool            `json:"auto"`
	Reason     *string         `json:"reason"`
	Causes     []holdCause     `json:"causes"`
	Suppressed json.RawMessage `json:"suppressed"`
}

func (r *Retention) readHold(ctx context.Context) (holdState, error) {
	h := holdState{Causes: []holdCause{}, Suppressed: json.RawMessage(`[]`)}
	if err := r.readState(ctx, "hold", &h); err != nil {
		return h, err
	}
	if h.Causes == nil {
		h.Causes = []holdCause{}
	}
	if len(h.Suppressed) == 0 || string(h.Suppressed) == "null" {
		h.Suppressed = json.RawMessage(`[]`)
	}
	return h, nil
}

// HoldSet は contract.Retention を満たす。削除停止を掛ける。掛かっていれば since・reason・auto を変えず、同じ reason と code の要素が無ければ causes[] に足すだけ
// (restore_failed は同じ要素の refs[] に新しい世代を足す)。since を返す。hold.set の通知は呼び出し側(cli)が、新しく掛けたときだけ送る。
func (r *Retention) HoldSet(ctx context.Context, in contract.HoldInput) (time.Time, error) {
	if err := r.ready(); err != nil {
		return time.Time{}, err
	}
	if err := aborted(ctx); err != nil {
		return time.Time{}, err
	}
	if !slices.Contains(holdReasons, in.Reason) {
		return time.Time{}, newErr(contract.CodeArgInvalid, map[string]any{"flag": "--reason", "reason": "format"})
	}
	if r.Clock == nil {
		return time.Time{}, nilDep()
	}
	now, err := r.Clock.Now(ctx)
	if err != nil {
		return time.Time{}, err
	}
	at := now.UTC().Truncate(time.Second).Format(time.RFC3339)
	lock, err := r.State.Lock(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer lock.Release()
	h, err := r.readHold(ctx)
	if err != nil {
		return time.Time{}, err
	}
	cause := holdCause{Reason: in.Reason, At: at, Days: in.Days, Refs: append([]string{}, in.Refs...)}
	if in.Code != nil {
		c := string(*in.Code)
		cause.Code = &c
	}
	if !h.Active {
		h.Active, h.Since, h.Auto, h.Reason, h.Causes = true, &at, in.Reason != "manual", &in.Reason, []holdCause{cause}
	} else {
		h.Causes = mergeCause(h.Causes, cause)
	}
	b, err := json.Marshal(h)
	if err != nil {
		return time.Time{}, err
	}
	if err := r.State.Update(ctx, lock, "hold", b); err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, *h.Since)
}

func sameCode(a, b *string) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

// mergeCause は停止中に当たった原因を causes[] に足す(同じ reason と code の要素があれば足さない。restore_failed は refs[] を足す)。
func mergeCause(causes []holdCause, c holdCause) []holdCause {
	for i := range causes {
		if causes[i].Reason == c.Reason && sameCode(causes[i].Code, c.Code) {
			if c.Reason == "restore_failed" {
				for _, ref := range c.Refs {
					if !slices.Contains(causes[i].Refs, ref) {
						causes[i].Refs = append(causes[i].Refs, ref)
					}
				}
			}
			return causes
		}
	}
	return append(causes, c)
}

// Class は contract.Retention を満たす。現地(timezone)の暦月に同じ月の monthly の世代が無く、その世代が健全なら monthly、それ以外は daily。
func (r *Retention) Class(in contract.ClassInput) (contract.RetentionClass, error) {
	loc, err := time.LoadLocation(in.Timezone)
	if err != nil || in.Timezone == "" {
		return "", newErr(contract.CodeArgInvalid, map[string]any{"flag": "--timezone", "reason": "format"})
	}
	if !in.Healthy {
		return contract.RetentionDaily, nil
	}
	y, m, _ := in.CreatedAt.In(loc).Date()
	for _, s := range in.Snapshots {
		sy, sm, _ := s.CreatedAt.In(loc).Date()
		if s.RetentionClass == contract.RetentionMonthly && sy == y && sm == m {
			return contract.RetentionDaily, nil
		}
	}
	return contract.RetentionMonthly, nil
}
