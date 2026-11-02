package restore

import (
	"context"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type storageReads struct {
	Month        string `json:"month"`
	ReadBytes    int64  `json:"read_bytes"`
	ReservedUsed int    `json:"reserved_used"`
}

// plannedRuns はその月に計画された復元テストの回数(monthly は 1、weekly は月曜日がその月に属する週の数。spec 3.5)。
func plannedRuns(def []byte, now time.Time, loc *time.Location) int {
	var d struct {
		RestoreTest string `json:"restore_test"`
	}
	_ = jsonUnmarshal(def, &d)
	if d.RestoreTest != "weekly" {
		return 1
	}
	t := now.In(loc)
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
	n := 0
	for day := first; day.Month() == first.Month(); day = day.AddDate(0, 0, 1) {
		if day.Weekday() == time.Monday {
			n++
		}
	}
	return n
}

// sizeOfChunks は latest の世代が参照するデータのチャンクの保管先の上の大きさの和(同じチャンクは 1 回。マニフェスト等は含めない)。
func sizeOfChunks(ctx context.Context, o *objReader, m *manifestDoc) (int64, error) {
	rows, err := o.keyTable(ctx, m)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	vers, err := o.store.ListVersions(ctx, "chunks/")
	if err != nil {
		return 0, err
	}
	size := map[string]int64{}
	for _, v := range vers {
		size[v.Key+"\x00"+v.VersionID] = v.Size
	}
	var sum int64
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.ID] || len(r.ID) < 2 {
			continue
		}
		seen[r.ID] = true
		sum += size["chunks/"+r.ID[:2]+"/"+r.ID+"\x00"+r.V]
	}
	return sum, nil
}

// planAmount は latest の世代の計画の量とその月の累計・予約の有無を返す。generation は読んだ世代(無ければ nil)。
func planAmount(ctx context.Context, st contract.StateFiles, o *objReader, clk contract.Clock) (contract.PlanAmount, *snapRec, *manifestDoc, error) {
	var a contract.PlanAmount
	def, _, err := st.ReadDef(ctx)
	if err != nil {
		return a, nil, nil, err
	}
	now, err := clk.Now(ctx)
	if err != nil {
		return a, nil, nil, err
	}
	loc := defTimezone(def)
	var sr storageReads
	has, err := item(ctx, st, "storage_reads", &sr)
	if err != nil {
		return a, nil, nil, err
	}
	a.Reserved = true
	if has && sr.Month == monthOf(now, loc) {
		a.MonthToDateBytes = sr.ReadBytes
		a.Reserved = sr.ReservedUsed < plannedRuns(def, now, loc)
	}
	snaps, err := loadSnapshots(ctx, st)
	if err != nil {
		return a, nil, nil, err
	}
	s, ok := latestRef(snaps)
	if !ok {
		return a, nil, nil, nil
	}
	if o.codec == nil || o.keyS == nil {
		return a, &s, nil, internalErr("")
	}
	m, _, err := o.manifest(ctx, s.Ref, s.VersionID, nil)
	if err != nil {
		return a, &s, nil, err
	}
	if a.PlannedBytes, err = sizeOfChunks(ctx, o, m); err != nil {
		return a, &s, m, err
	}
	return a, &s, m, nil
}

// Plan は読み出し量の計画(spec 3.5)。計画の量は latest の世代の全体を復元するときに保管先から読む量。
func (p *ReadPlan) Plan(ctx context.Context) (contract.PlanAmount, error) {
	if p == nil || p.Store == nil || p.State == nil || p.Clock == nil {
		return contract.PlanAmount{}, internalErr("")
	}
	a, _, _, err := planAmount(ctx, p.State, &objReader{store: p.Store, codec: p.Codec, keyS: p.KeyS}, p.Clock)
	return a, err
}
