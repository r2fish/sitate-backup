package cli

// hold set(spec 3.1.2。人が掛ける削除停止。理由は manual に固定)と、核が自動で掛ける削除停止の判定(spec 4.2「削除停止の状態と判定」、
// 3.1.1 の規則 16)。停止の状態の書き込みは Retention.HoldSet(U1b)が行い、cli は掛けたときの通知と journal/ の記録だけを持つ。

import (
	"context"
	"encoding/json"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func init() { routeOwn["hold set"] = holdSetEntry }

// autoHoldDaysDefault は auto_hold_days の既定(spec 4.2)。
const autoHoldDaysDefault = 14

type holdCauseView struct {
	Reason string  `json:"reason"`
	Code   *string `json:"code"`
}

type holdView struct {
	Active     bool            `json:"active"`
	Causes     []holdCauseView `json:"causes"`
	Suppressed []holdCauseView `json:"suppressed"`
}

func readHoldView(ctx context.Context, d *Deps) holdView {
	var h holdView
	if v, err := d.State.Get(ctx, "hold"); err == nil {
		_ = json.Unmarshal(v, &h)
	}
	return h
}

func sameCause(a, b holdCauseView) bool {
	if a.Reason != b.Reason {
		return false
	}
	return (a.Code == nil) == (b.Code == nil) && (a.Code == nil || *a.Code == *b.Code)
}

func hasCause(list []holdCauseView, c holdCauseView) bool {
	for _, x := range list {
		if sameCause(x, c) {
			return true
		}
	}
	return false
}

// clearSuppressed は健常性 pass かつ全対象 ok のバックアップが 1 回できたときに hold.suppressed[] を空にする(spec 4.2。解除後の再発動の抑止はそれまで)。
func (d *Deps) clearSuppressed(ctx context.Context) {
	if d.State == nil {
		return
	}
	v, err := d.State.Get(ctx, "hold")
	if err != nil {
		return
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(v, &m) != nil || len(m["suppressed"]) == 0 || string(m["suppressed"]) == "[]" || string(m["suppressed"]) == "null" {
		return
	}
	m["suppressed"] = json.RawMessage("[]")
	nb, _ := json.Marshal(m)
	h, err := d.State.Lock(ctx)
	if err != nil {
		return
	}
	defer h.Release()
	_ = d.State.Update(ctx, h, "hold", nb)
}

func holdActive(ctx context.Context, d *Deps) bool {
	v, err := d.State.Get(ctx, "hold")
	var h holdView
	return err == nil && json.Unmarshal(v, &h) == nil && h.Active
}

func holdSetEntry(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
	if c.DryRun { // --dry-run は実行しない(spec 3.1.1)。削除停止も通知も journal も動かさない
		return notImpl("hold-set.dry-run")
	}
	if d.Store == nil || d.State == nil {
		return notImpl("hold-set")
	}
	since, err := d.setHold(ctx, contract.HoldInput{Reason: "manual"}, "")
	if err != nil {
		return contract.Reply{}, err
	}
	b, _ := json.Marshal(map[string]any{"held_since": since.UTC().Format(time.RFC3339)})
	return contract.Reply{Result: b}, nil
}

// setHold は削除停止を掛ける。新しく掛けたときだけ hold.set(critical)を送り、journal/ の hold の項目を書く(spec 4.8、4.5)。
// 掛かっていれば原因を足すだけで通知しない(spec 4.2)。
func (d *Deps) setHold(ctx context.Context, in contract.HoldInput, runID string) (time.Time, error) {
	was := holdActive(ctx, d)
	since, err := d.Store.HoldSet(ctx, in)
	if err != nil {
		return since, err
	}
	if d.Recorder == nil {
		return since, nil
	}
	var code any
	if in.Code != nil {
		code = string(*in.Code)
	}
	var days any
	if in.Days != nil {
		days = *in.Days
	}
	body := map[string]any{"active": true, "reason": in.Reason, "code": code, "auto": in.Reason != "manual"}
	d.recordItem(ctx, "hold", runID, body)
	if !was {
		_ = d.emit(ctx, "hold.set", map[string]any{"reason": in.Reason, "code": code, "days": days})
	}
	return since, nil
}

type snapshotView struct {
	CreatedAt      string `json:"created_at"`
	Health         string `json:"health"`
	TargetsOK      bool   `json:"targets_ok"`
	RestoreFailed  bool   `json:"restore_failed"`
	DefCopyMismtch bool   `json:"def_copy_mismatch"`
}

// healthySnapshot は「健常性 pass かつ全対象 ok」で復元できなかった印の無い世代(spec 4.2 の保護中の世代と同じ)。
func (s snapshotView) healthy() bool { return s.Health == "pass" && s.TargetsOK && !s.RestoreFailed }

// autoHold は自動の削除停止の判定: 最後の健全な世代(無ければ seal を適用した時刻)の現地の暦日から auto_hold_days 日以上たっていたら
// no_healthy_backup で停止する。タイマーで起動した各実行の始め(LOCKED を返す経路を含む。規則 16)と backup の終わりに呼ぶ。
// 停止中は原因を足すだけ。失敗は実行を止めない。
func (d *Deps) autoHold(ctx context.Context, runID string) {
	if d.Store == nil || d.State == nil || d.Clock == nil {
		return
	}
	norm, _, err := d.State.ReadDef(ctx)
	if err != nil || len(norm) == 0 {
		return
	}
	var def struct {
		AutoHoldDays int    `json:"auto_hold_days"`
		Timezone     string `json:"timezone"`
	}
	_ = json.Unmarshal(norm, &def)
	if def.AutoHoldDays <= 0 {
		def.AutoHoldDays = autoHoldDaysDefault
	}
	if def.Timezone == "" {
		def.Timezone = "Asia/Tokyo" // spec 4.1 の既定
	}
	loc, e := time.LoadLocation(def.Timezone)
	if e != nil {
		loc = time.UTC
	}
	base, ok := d.autoHoldBase(ctx)
	if !ok {
		return
	}
	now, err := d.Clock.Now(ctx)
	if err != nil {
		return
	}
	days := calendarDays(base.In(loc), now.In(loc))
	if days < def.AutoHoldDays {
		return
	}
	in := contract.HoldInput{Reason: "no_healthy_backup", Days: &days, Code: d.lastBackupFailure(ctx)}
	var code *string
	if in.Code != nil {
		c := string(*in.Code)
		code = &c
	}
	cause := holdCauseView{Reason: in.Reason, Code: code}
	h := readHoldView(ctx, d)
	if hasCause(h.Suppressed, cause) { // hold-release の後、バックアップが 1 回成功するまで同じ原因では再発動しない(spec 4.2)
		return
	}
	if h.Active && hasCause(h.Causes, cause) { // 停止中の同じ原因は項目も通知も重ねない
		return
	}
	_, _ = d.setHold(ctx, in, runID)
}

// autoHoldBase は基点: snapshots[] の最後の健全な世代の created_at。無ければ sealed_at。
func (d *Deps) autoHoldBase(ctx context.Context) (time.Time, bool) {
	var snaps []snapshotView
	if v, err := d.State.Get(ctx, "snapshots"); err == nil {
		_ = json.Unmarshal(v, &snaps)
	}
	var best time.Time
	for _, s := range snaps {
		if t, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil && s.healthy() && t.After(best) {
			best = t
		}
	}
	if !best.IsZero() {
		return best, true
	}
	var sealedAt string
	if v, err := d.State.Get(ctx, "sealed_at"); err == nil && json.Unmarshal(v, &sealedAt) == nil {
		if t, e := time.Parse(time.RFC3339, sealedAt); e == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func calendarDays(from, to time.Time) int {
	a := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	b := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a).Hours() / 24)
}

// lastBackupFailure は直近の失敗した backup の run_end の error.code(無ければ nil)。
func (d *Deps) lastBackupFailure(ctx context.Context) *contract.Code {
	items, err := d.journal(ctx)
	if err != nil {
		return nil
	}
	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		if it.Kind != "run_end" {
			continue
		}
		var b runEndBody
		if json.Unmarshal(it.Body, &b) != nil || len(b.Command) == 0 || b.Command[0] != "backup" || b.OK || b.Code == nil {
			continue
		}
		if *b.Code == string(contract.CodeLocked) {
			continue
		}
		c := contract.Code(*b.Code)
		return &c
	}
	return nil
}
