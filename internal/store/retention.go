package store

import (
	"context"
	"encoding/json"
	"slices"
	"time"
	_ "time/tzdata" // timezone の名前を実行環境の tzdata に頼らず引く(Class)

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Retention は contract.Retention の実物(保管先の保持の操作。spec 4.2、7.5)。
// 依存は公開フィールドで受ける(契約試験が差し込む)。
type Retention struct {
	Store contract.ObjectStore
	State contract.StateFiles
	Codec contract.Codec
	KeyS  contract.KeyService
	// Clock はバックアップサーバーの時計(hold の時刻に使う。保持期限の「今」は保管先の Date + Offset で、Clock は使わない。TIME_SKEW は ObjectStore が見る)。
	Clock contract.Clock
	// Offset は保管先の Date に足す模擬時間のオフセット(試験ビルドだけ。nil はオフセット 0)。prune・extend の「今」は StoreTime + Offset で判定する(作業指示書 3.3)。
	Offset contract.Offsetter
	// Index はチャンクの記録(chunks/ の版の retain-until。extend の入力と更新、prune の除去)。nil で chunks/ を扱う場面は止まる。
	Index contract.Index
	// Intents は書き込み予定のキーの一覧(journal の write_intent。結合で cli が注入する)。中断した書き込みを UNEXPECTED_VERSION にしない。
	// nil は「予定なし」ではなく、記録外の版は UNEXPECTED_VERSION のまま(安全側)。読めない誤りは誤りで返す。
	Intents func(ctx context.Context) ([]string, error)
	// Params は保持の定数(nil は製品の固定の定数。試験ビルドだけが別の実装を入れる)。
	Params contract.RetentionParams
	// BinVersion は今の構成の核の版(`bin/<version>/` の延長と削除の判定に使う。空なら `bin/` の記録をすべて共有物として延長し、消さない)。
	BinVersion string
}

var _ contract.Retention = (*Retention)(nil)

func (r *Retention) params() contract.RetentionParams {
	if r.Params == nil {
		return contract.FixedRetention{}
	}
	return r.Params
}

func (r *Retention) ready() error {
	if r.Store == nil || r.State == nil {
		return nilDep()
	}
	return nil
}

// now は保管先の応答の Date + 注入したオフセット(秒まで)。
func (r *Retention) now(ctx context.Context) (time.Time, error) {
	t, err := r.Store.StoreTime(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if r.Offset != nil {
		off, err := r.Offset.Offset()
		if err != nil {
			return time.Time{}, err
		}
		t = t.Add(off)
	}
	return t.UTC().Truncate(time.Second), nil
}

// currentRetain は版の現在の retain-until(保管先から版指定で読む)。版が無ければ contract.ErrMissing。読めないときはゼロで進まず誤りで返す。
func (r *Retention) currentRetain(ctx context.Context, key, versionID string) (time.Time, error) {
	return r.Store.RetainUntil(ctx, key, versionID)
}

// readState は state.json の項目を v に読む。項目が無い・null は何もしない。
func (r *Retention) readState(ctx context.Context, item string, v any) error {
	raw, err := r.State.Get(ctx, item)
	if err != nil {
		return err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// writeState は state.lock の短い排他の下で項目を置き換える。取り消された文脈でも記録を残せるよう、呼び出し側が文脈を選ぶ。
func (r *Retention) writeState(ctx context.Context, items map[string]any) error {
	if len(items) == 0 {
		return nil
	}
	h, err := r.State.Lock(ctx)
	if err != nil {
		return err
	}
	defer h.Release()
	names := make([]string, 0, len(items))
	for k := range items {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, k := range names {
		b, err := json.Marshal(items[k])
		if err != nil {
			return err
		}
		if err := r.State.Update(ctx, h, k, b); err != nil {
			return err
		}
	}
	return nil
}

// recordCtx は取り消された実行でも記録を書くための文脈(締め切りは RecordDeadline。spec 3.1.1 の規則 21)。
func recordCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), contract.RecordDeadline)
}

// MissingRefs は contract.Retention を満たす。版付き一覧に記録した版 ID(と大きさ)があるかを照合する(backup の終わり。spec 4.4 の完了の規則)。
func (r *Retention) MissingRefs(ctx context.Context, refs []contract.VersionRef) ([]contract.VersionRef, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if err := aborted(ctx); err != nil {
		return nil, err
	}
	vs, err := r.Store.ListVersions(ctx, "")
	if err != nil {
		return nil, err
	}
	have := map[objID]int64{}
	for _, v := range vs {
		if !v.DeleteMarker {
			have[objID{v.Key, v.VersionID}] = v.Size
		}
	}
	missing := []contract.VersionRef{}
	for _, ref := range refs {
		size, ok := have[objID{ref.Key, ref.VersionID}]
		if !ok || (ref.Size != 0 && size != ref.Size) {
			missing = append(missing, ref)
		}
	}
	return missing, nil
}

// mutate は state.lock の中で objects[]・snapshots[] を読み直し、fn で変えた分だけを書く(読んでから書くまでの間に足された記録を消さない)。extra の項目も同じ排他で置く。
func (r *Retention) mutate(ctx context.Context, fn func(*stateRecs), extra map[string]any) error {
	h, err := r.State.Lock(ctx)
	if err != nil {
		return err
	}
	defer h.Release()
	s, err := r.loadRecs(ctx)
	if err != nil {
		return err
	}
	fn(s)
	if s.objects == nil {
		s.objects = []rec{}
	}
	items := map[string]any{"objects": s.objects}
	if s.snapshots != nil {
		items["snapshots"] = s.snapshots
	}
	for k, v := range extra {
		items[k] = v
	}
	for k, v := range items {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if err := r.State.Update(ctx, h, k, b); err != nil {
			return err
		}
	}
	return nil
}
