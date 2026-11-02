package restore

import (
	"context"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func tsz(t time.Time) string { return t.UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z") }

type listItem struct {
	Ref              string `json:"ref"`
	At               string `json:"at"`
	StoredAt         string `json:"stored_at"`
	StoredAtMismatch bool   `json:"stored_at_mismatch"`
	RetentionClass   string `json:"retention_class"`
	Health           string `json:"health"`
	TargetsOK        bool   `json:"targets_ok"`
	RestoreVerified  *bool  `json:"restore_verified"`
	RefsVerified     *bool  `json:"refs_verified"`
	RestoreFailed    bool   `json:"restore_failed"`
	Findings         []any  `json:"findings"`
}

// List は世代の一覧(spec 3.1.2.3)。stored_at は保管先の版一覧の書き込み時刻(秒に切り捨て)、at の新しい順。
// 取得時チェックの所見はイテレーション 1 には無いため findings は常に空の配列。
func (r *Restore) List(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if err := r.coreMissing(); err != nil {
		return contract.Reply{}, err
	}
	snaps, err := loadSnapshots(ctx, r.State)
	if err != nil {
		return contract.Reply{}, err
	}
	vers, err := r.Store.ListVersions(ctx, "manifests/")
	if err != nil {
		return contract.Reply{}, err
	}
	stored := map[string]time.Time{}
	for _, v := range vers {
		if !v.DeleteMarker {
			stored[v.Key+"\x00"+v.VersionID] = v.LastModified
		}
	}
	var offset time.Duration
	if o, ok := r.Clock.(contract.Offsetter); ok {
		if offset, err = o.Offset(); err != nil {
			return contract.Reply{}, err
		}
	}
	rows := make([]listItem, 0, len(snaps))
	for _, s := range sortedByAtDesc(snaps) {
		storedAt := s.StoredAt
		st, _ := time.Parse(time.RFC3339, s.StoredAt)
		if lm, ok := stored["manifests/"+s.Ref+"\x00"+s.VersionID]; ok {
			st = lm.UTC().Truncate(time.Second)
			storedAt = tsz(st)
		}
		// 試験ビルドの模擬時間: 保管先の Date は実時刻なので、注入したオフセットを足して比べる(作業指示書 3.3)。
		skew := st.Add(offset).Sub(s.created())
		if skew < 0 {
			skew = -skew
		}
		rows = append(rows, listItem{Ref: s.Ref, At: s.CreatedAt, StoredAt: storedAt, StoredAtMismatch: skew > contract.TimeSkewMax,
			RetentionClass: s.RetentionClass, Health: s.Health, TargetsOK: s.TargetsOK, RestoreVerified: s.RestoreVerified,
			RefsVerified: s.RefsVerified, RestoreFailed: s.RestoreFailed, Findings: []any{}})
	}
	var latest any
	if l, ok := latestRef(snaps); ok {
		latest = l.Ref
	}
	return reply(map[string]any{"backups": rows, "latest": latest})
}
