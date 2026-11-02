package cli

// report --weekly(spec 3.1.2、4.8 の digest.weekly。作業指示書 5.17 の完了条件 22)。週次の一覧を作って通知する。
// 取れていない候補の件数(uncovered_count)と、人が対象にしないと決めた候補(source.declined)の件数・除外 id を毎回載せる。
// 実名(パス・DB 名)は載せない。月次の報告(--weekly なし)はイテレーション 3。

import (
	"context"
	"encoding/json"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func init() { routeOwn["report"] = reportEntry }

func reportEntry(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
	if _, weekly := c.Args["weekly"]; !weekly {
		return notImpl("report-monthly") // 月次の報告はイテレーション 3
	}
	if d.State == nil || d.Recorder == nil || d.Clock == nil {
		return notImpl("report-weekly")
	}
	now, err := d.Clock.Now(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	norm, _, _ := d.State.ReadDef(ctx)
	var def struct {
		Timezone string `json:"timezone"`
		Source   struct {
			Declined []map[string]json.RawMessage `json:"declined"`
		} `json:"source"`
	}
	_ = json.Unmarshal(norm, &def)
	if def.Timezone == "" {
		def.Timezone = "Asia/Tokyo"
	}
	loc, lerr := time.LoadLocation(def.Timezone)
	if lerr != nil {
		loc = time.UTC
	}
	start, end := weekPeriod(now, loc)
	facts := map[string]any{
		"period_start_at": start.UTC().Format(time.RFC3339), "period_end_at": end.UTC().Format(time.RFC3339),
		"backups": weeklyBackups(ctx, d, start, end), "last_restore_ok_at": lastRestoreOKAt(ctx, d),
		"cost_over_approved": false, "uncovered_count": 0, // 費用の超過の継続の判定は estimate の現在の構成(イテレーション 3)。uncovered は status と同じ 0
	}
	ids := []string{}
	for _, e := range def.Source.Declined {
		var id string
		if json.Unmarshal(e["id"], &id) == nil && id != "" {
			ids = append(ids, id)
		}
	}
	facts["declined_count"], facts["declined_ids"] = len(ids), ids
	delivered := []map[string]any{}
	if !d.digestSentFor(ctx, end) { // 同じ週の一覧は 1 回だけ通知する
		_, res, err := d.Recorder.Emit(ctx, "digest.weekly", mustMarshal(facts))
		if err != nil {
			return contract.Reply{}, err
		}
		delivered = deliveredOf(res)
		d.noteDigest(ctx, now, res)
	}
	out := map[string]any{"capacity": weeklyCapacity(ctx, d), "estimate": emptyEstimate(), "restore_tests": []any{}, "delivered": delivered}
	b, _ := json.Marshal(out)
	return contract.Reply{Result: b}, nil
}

// weekPeriod は now を含む週の月曜日の現地 0 時を終わりとする前の 7 日(月曜日〜日曜日)。
func weekPeriod(now time.Time, loc *time.Location) (start, end time.Time) {
	l := now.In(loc)
	back := (int(l.Weekday()) + 6) % 7 // 月曜日 = 0
	end = time.Date(l.Year(), l.Month(), l.Day()-back, 0, 0, 0, 0, loc)
	return end.AddDate(0, 0, -7), end
}

func weeklyBackups(ctx context.Context, d *Deps, start, end time.Time) []map[string]any {
	var snaps []struct {
		Ref       string `json:"ref"`
		CreatedAt string `json:"created_at"`
		Health    string `json:"health"`
		TargetsOK bool   `json:"targets_ok"`
	}
	if v, err := d.State.Get(ctx, "snapshots"); err == nil {
		_ = json.Unmarshal(v, &snaps)
	}
	out := []map[string]any{}
	for _, s := range snaps {
		if t, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil && !t.Before(start) && t.Before(end) {
			out = append(out, map[string]any{"snapshot_ref": s.Ref, "created_at": s.CreatedAt, "health": s.Health, "targets_ok": s.TargetsOK, "changed_during_read": 0})
		}
	}
	return out
}

func lastRestoreOKAt(ctx context.Context, d *Deps) any {
	var lr struct {
		LastOKAt *string `json:"last_ok_at"`
	}
	if v, err := d.State.Get(ctx, "last_restore_test"); err == nil && json.Unmarshal(v, &lr) == nil && lr.LastOKAt != nil {
		return *lr.LastOKAt
	}
	return nil
}

func weeklyCapacity(ctx context.Context, d *Deps) map[string]any {
	var snaps []struct {
		Size int64 `json:"size"`
	}
	if v, err := d.State.Get(ctx, "snapshots"); err == nil {
		_ = json.Unmarshal(v, &snaps)
	}
	var total int64
	for _, s := range snaps {
		total += s.Size
	}
	return map[string]any{"stored_bytes": total, "objects": 0, "delta_bytes": 0, "capacity_gb": map[string]any{"y1": 0, "y3": 0}}
}

// emptyEstimate は価格表と入力が結線されるまでの report の estimate(月額を出さない形。estimate の現在の構成の結果と同じ欄)。
func emptyEstimate() map[string]any {
	return map[string]any{"capacity_gb": map[string]any{"y1": 0, "y3": 0}, "monthly_jpy": []any{}, "deltas": []any{}, "cost_ceiling_yen": 0, "over_ceiling": nil,
		"uncounted": []string{"server", "restore_dest"}, "excluded_suspect": 0, "price_table_date": nil, "teardown_daily_jpy": 0, "basis": "args",
		"inputs": map[string]any{"size_gb": 0, "daily_gb": 0, "dump_gb": 0}}
}

func deliveredOf(res []contract.DeliveryResult) []map[string]any {
	out := []map[string]any{}
	for _, r := range res {
		var at, reason any
		if r.AcceptedAt != nil {
			at = r.AcceptedAt.UTC().Format(time.RFC3339)
		}
		if r.Reason != "" {
			reason = r.Reason
		}
		out = append(out, map[string]any{"to": r.To, "channel": r.Channel, "delivered": r.Delivered, "attempts": r.Attempts, "reason": reason, "accepted_at": at})
	}
	return out
}

// digestSentFor は同じ週(period_end_at)の一覧を送り済みか。
func (d *Deps) digestSentFor(ctx context.Context, end time.Time) bool {
	var n struct {
		LastDigestAt *string `json:"last_digest_at"`
	}
	if v, err := d.State.Get(ctx, "notify"); err == nil && json.Unmarshal(v, &n) == nil && n.LastDigestAt != nil {
		if t, e := time.Parse(time.RFC3339, *n.LastDigestAt); e == nil {
			return !t.Before(end)
		}
	}
	return false
}

// noteDigest は status.notify.last_digest_at(送った時刻)と last_digest_accepted_at(人向けの宛先の少なくとも 1 つが受理した最後の時刻)を書く。
func (d *Deps) noteDigest(ctx context.Context, now time.Time, res []contract.DeliveryResult) {
	h, err := d.State.Lock(ctx)
	if err != nil {
		return
	}
	defer h.Release()
	m := map[string]json.RawMessage{}
	if v, e := d.State.Get(ctx, "notify"); e == nil {
		_ = json.Unmarshal(v, &m)
	}
	if m == nil { // 項目が無い(null)ときは空の表から
		m = map[string]json.RawMessage{}
	}
	m["last_digest_at"] = mustMarshal(now.UTC().Format(time.RFC3339))
	for _, r := range res {
		if r.Delivered && r.AcceptedAt != nil {
			m["last_digest_accepted_at"] = mustMarshal(r.AcceptedAt.UTC().Format(time.RFC3339))
		}
	}
	_ = d.State.Update(ctx, h, "notify", mustMarshal(m))
}
