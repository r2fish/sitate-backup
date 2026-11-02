package cli

// status(spec 3.1.2.7)。読み取りのみで、ロックを試し取りしない。実行中の実行は実行中の印(4.5)から、そのほかは state.json の項目から作る。
// 材料の項目が無い(まだ誰も書いていない)ときは、結果の型に合う空の値にする。

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func init() { routeOwn["status"] = statusEntry }

const (
	scheduleDefault = "02:30" // 定義の schedule が無いときの表示(spec 4.1 の既定)
	kitCheckDays    = 365     // 所在確認から 1 年(spec 3.1.2.7 の location_check_due)
	protectionDays  = 90      // protection_ends_at = last_extend_ok_at + 90 日(spec 3.1.2.7)
)

type stSnap struct {
	Ref            string `json:"ref"`
	CreatedAt      string `json:"created_at"`
	Health         string `json:"health"`
	TargetsOK      bool   `json:"targets_ok"`
	RefsVerified   *bool  `json:"refs_verified"`
	RetentionUntil string `json:"retention_until"`
	RetainUntil    string `json:"retain_until"`
	Size           int64  `json:"size"`
}

func statusEntry(d *Deps, ctx context.Context, _ contract.Call) (contract.Reply, error) {
	if d.State == nil {
		return notImpl("status")
	}
	get := func(item string, into any) {
		if v, err := d.State.Get(ctx, item); err == nil && string(v) != "null" {
			_ = json.Unmarshal(v, into)
		}
	}
	now, err := d.Clock.Now(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	var initialized, phraseConfirmed, keyRotate bool
	get("initialized", &initialized)
	get("phrase_confirmed", &phraseConfirmed)
	get("key_rotate_required", &keyRotate)
	var snaps []stSnap
	get("snapshots", &snaps)
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].CreatedAt < snaps[j].CreatedAt })

	res := map[string]any{
		"initialized": initialized, "running": runningOf(d), "schedule": scheduleOf(ctx, d),
		"last_backup": lastBackupOf(snaps), "last_restore_test": lastRestoreOf(ctx, d, now),
		"unverified_backups": unverifiedOf(snaps), "pending_changes": 0, "waiting_panel": []any{},
		"hold": holdOf(ctx, d), "kit": kitOf(ctx, d, now), "phrase_confirmed": phraseConfirmed, "key_rotate_required": keyRotate,
		"notify": notifyOf(ctx, d), "restore_sessions": []any{}, "teardown_pending": []any{},
		"storage_reads_this_month": readsOf(ctx, d, now), "storage": storageOf(ctx, d),
		"retention": retentionOf(ctx, d, snaps), "uncovered": []any{}, "uncovered_count": 0,
		"estimate": map[string]any{"monthly_jpy": nil, "cost_ceiling_yen": ceilingOf(ctx, d), "over_ceiling": nil, "uncounted": []string{"server", "restore_dest"}},
		"warnings": warningsOf(ctx, d),
	}
	b, _ := json.Marshal(res)
	return contract.Reply{Result: b}, nil
}

func runningOf(d *Deps) any {
	m, st := readMarker(d.StateDir)
	if st != markerOK {
		return nil
	}
	_, _, boot := selfProc()
	stale := (m.BootID != "" && boot != "" && m.BootID != boot) || !procMatches(m.Pid, m.PidStartTicks)
	return map[string]any{"run_id": m.RunID, "command": m.Command, "started_at": m.StartedAt, "stale_suspected": stale}
}

// procMatches は pid と開始時刻(/proc/<pid>/stat の 22 番目の欄)が一致するプロセスがあるか。/proc の無い環境では「ある」とする(表示だけの印)。
func procMatches(pid int, ticks int64) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		_, statErr := os.Stat("/proc/self/stat")
		return statErr != nil
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 20 {
		return false
	}
	got, _ := strconv.ParseInt(f[19], 10, 64)
	return ticks == 0 || got == ticks
}

func scheduleOf(ctx context.Context, d *Deps) string {
	var def struct {
		Schedule string `json:"schedule"`
	}
	if norm, _, err := d.State.ReadDef(ctx); err == nil {
		_ = json.Unmarshal(norm, &def)
	}
	if def.Schedule == "" {
		return scheduleDefault
	}
	return def.Schedule
}

func ceilingOf(ctx context.Context, d *Deps) int64 {
	var def struct {
		Ceiling int64 `json:"cost_ceiling_yen"`
	}
	if norm, _, err := d.State.ReadDef(ctx); err == nil {
		_ = json.Unmarshal(norm, &def)
	}
	return def.Ceiling
}

// lastBackupOf: ok は health が pass で全対象 ok。checks_count は、イテレーション 1 にはチェックが無いので 0(状態ファイルの世代の項目に件数は無い)。
func lastBackupOf(snaps []stSnap) any {
	if len(snaps) == 0 {
		return nil
	}
	s := snaps[len(snaps)-1]
	return map[string]any{"at": s.CreatedAt, "ok": s.Health == "pass" && s.TargetsOK, "health": s.Health, "snapshot_ref": s.Ref, "checks_count": 0}
}

func unverifiedOf(snaps []stSnap) int {
	n := 0
	for _, s := range snaps {
		if s.RefsVerified == nil || !*s.RefsVerified {
			n++
		}
	}
	return n
}

func lastRestoreOf(ctx context.Context, d *Deps, now time.Time) any {
	var lr struct {
		At                  string  `json:"at"`
		OK                  bool    `json:"ok"`
		BackupRef           *string `json:"backup_ref"`
		LastOKAt            *string `json:"last_ok_at"`
		ConsecutiveFailures int     `json:"consecutive_failures"`
		Deferred            bool    `json:"deferred"`
		DeferredReason      *string `json:"deferred_reason"`
	}
	v, err := d.State.Get(ctx, "last_restore_test")
	if err != nil || string(v) == "null" || json.Unmarshal(v, &lr) != nil {
		return nil
	}
	var days any
	from := ""
	if lr.LastOKAt != nil {
		from = *lr.LastOKAt
	} else {
		var sealedAt *string
		if sv, e := d.State.Get(ctx, "sealed_at"); e == nil {
			_ = json.Unmarshal(sv, &sealedAt)
		}
		if sealedAt != nil {
			from = *sealedAt
		}
	}
	if t, e := time.Parse(time.RFC3339, from); e == nil {
		n := int(now.Sub(t) / (24 * time.Hour))
		if n < 0 {
			n = 0
		}
		days = n
	}
	var proof any
	if pv, e := d.State.Get(ctx, "last_proof"); e == nil && string(pv) != "null" {
		proof = json.RawMessage(pv)
	}
	return map[string]any{"at": lr.At, "ok": lr.OK, "last_ok_at": lr.LastOKAt, "days_since": days, "consecutive_failures": lr.ConsecutiveFailures,
		"backup_ref": lr.BackupRef, "deferred": lr.Deferred, "deferred_reason": lr.DeferredReason, "proof": proof}
}

func holdOf(ctx context.Context, d *Deps) any {
	h := map[string]any{"active": false, "since": nil, "reason": nil, "auto": false, "causes": []any{}}
	var st map[string]json.RawMessage
	if v, err := d.State.Get(ctx, "hold"); err == nil && json.Unmarshal(v, &st) == nil {
		for _, k := range []string{"active", "since", "reason", "auto", "causes"} {
			if x, ok := st[k]; ok {
				h[k] = x
			}
		}
	}
	return h
}

func kitOf(ctx context.Context, d *Deps, now time.Time) any {
	var k struct {
		Version       string  `json:"version"`
		Confirmed     *string `json:"confirmed_version"`
		LocationCheck *string `json:"location_checked_at"`
	}
	v, err := d.State.Get(ctx, "kit")
	if err == nil && string(v) != "null" {
		_ = json.Unmarshal(v, &k)
	}
	due := true
	if k.LocationCheck != nil {
		if t, e := time.Parse(time.RFC3339, *k.LocationCheck); e == nil {
			due = now.Sub(t) > kitCheckDays*24*time.Hour
		}
	}
	if k.Version == "" {
		k.Version = "none"
	}
	return map[string]any{"version": k.Version, "confirmed": k.Confirmed != nil, "matches_current": k.Confirmed != nil && *k.Confirmed == k.Version,
		"location_checked_at": k.LocationCheck, "location_check_due": due}
}

func notifyOf(ctx context.Context, d *Deps) any {
	n := map[string]any{"last_test_at": nil, "all_delivered": nil, "proof_confirmed": false, "last_digest_at": nil, "last_digest_accepted_at": nil}
	var st map[string]json.RawMessage
	if v, err := d.State.Get(ctx, "notify"); err == nil && json.Unmarshal(v, &st) == nil {
		for k := range n {
			if x, ok := st[k]; ok {
				n[k] = x
			}
		}
	}
	return n
}

func readsOf(ctx context.Context, d *Deps, now time.Time) any {
	out := map[string]any{"read_bytes": 0, "allowance_bytes": 0, "timer_reserved": false}
	var sr struct {
		Month        string `json:"month"`
		ReadBytes    int64  `json:"read_bytes"`
		ReservedUsed bool   `json:"reserved_used"`
	}
	if v, err := d.State.Get(ctx, "storage_reads"); err == nil && json.Unmarshal(v, &sr) == nil && sr.Month == now.UTC().Format("2006-01") {
		out["read_bytes"], out["timer_reserved"] = sr.ReadBytes, !sr.ReservedUsed
	}
	return out
}

func storageOf(ctx context.Context, d *Deps) any {
	var objs []struct {
		Size int64 `json:"size"`
	}
	var snaps []stSnap
	if v, err := d.State.Get(ctx, "objects"); err == nil {
		_ = json.Unmarshal(v, &objs)
	}
	if v, err := d.State.Get(ctx, "snapshots"); err == nil {
		_ = json.Unmarshal(v, &snaps)
	}
	var bytes int64
	for _, o := range objs {
		bytes += o.Size
	}
	for _, s := range snaps {
		bytes += s.Size
	}
	return map[string]any{"bytes": bytes, "objects": len(objs) + len(snaps)}
}

func retentionOf(ctx context.Context, d *Deps, snaps []stSnap) any {
	var last any
	max := ""
	for _, s := range snaps {
		if s.RetainUntil > max {
			max = s.RetainUntil
		}
	}
	if max != "" {
		last = max
	}
	var ext *string
	if v, err := d.State.Get(ctx, "last_extend_ok_at"); err == nil {
		_ = json.Unmarshal(v, &ext)
	}
	var prot any
	if ext != nil {
		if t, e := time.Parse(time.RFC3339, *ext); e == nil {
			prot = t.UTC().Add(protectionDays * 24 * time.Hour).Format(time.RFC3339)
		}
	}
	return map[string]any{"last_expires_at": last, "last_extend_ok_at": ext, "protection_ends_at": prot}
}

func warningsOf(ctx context.Context, d *Deps) []any {
	var ws []struct {
		Code  string `json:"code"`
		Since string `json:"since"`
	}
	if v, err := d.State.Get(ctx, "warnings_since"); err == nil {
		_ = json.Unmarshal(v, &ws)
	}
	out := []any{}
	for _, w := range ws {
		out = append(out, map[string]any{"code": w.Code, "detail": map[string]any{}, "next": []any{}, "since": w.Since})
	}
	return out
}
