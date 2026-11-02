package approval

// 承認と遅延(spec 3.6、4.6、7.5 の部品 approval)。pending/ の書き込みは approval が持つ。登録以外の操作は実行しない。

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// PlanHook は cli が組む plan の追加項目と前提の材料を返す継ぎ目(spec 7.5: plan の費用・読み出し量・ホスト鍵の組み立ては cli)。
// plan は plan へ足す項目、material は precondition_hash の材料のキー(3.1.2.9 の表の「前提の材料」のうち、approval が自分で読めないもの)。
// 適用の開始前の前提の再計算(3.6 の apply の (b))でも同じ args で呼ばれる。nil なら追加なし。
type PlanHook func(ctx context.Context, op string, args map[string]json.RawMessage) (plan, material map[string]json.RawMessage, err error)

// Approval は contract.Approval の実装。依存は公開フィールドで受ける(契約試験が偽物を差し込む)。
type Approval struct {
	State    contract.StateFiles
	Enroller contract.Enroller
	Recorder contract.Recorder
	Clock    contract.Clock
	// TestNotifier は notify-add・notify-change の提案の作成時の試験通知(確認語。3.6)。必須ではないが、無ければこれらの種別の提案は INTERNAL。
	TestNotifier contract.TestNotifier

	// Dir は状態ディレクトリ(4.5)。pending/ はこの下に置く。必須(空なら全操作が INTERNAL)。
	Dir string
	// Planner は plan の追加項目と前提の材料(nil 可)。
	Planner PlanHook
	// ReadDefinition は propose seal・def-change の --file の内容を読む(file は "-" か、バックアップサーバー上のパス。"-" は標準入力)。
	// 標準入力の読み取りは cli が持つため注入する。nil のとき定義を受ける種別は INTERNAL にする。
	ReadDefinition func(ctx context.Context, file string) ([]byte, error)

	mem   memBlobs
	mu    sync.Mutex
	cur   contract.Call                // 直近の ApplyPlan の呼び出し(ApplyEnroll が管理用資格情報を引く)
	steps map[string][]contract.OpStep // 変更ごとの、直近の ApplyPlan が返した操作の列
}

var _ contract.Approval = (*Approval)(nil)

func (a *Approval) store() *pstore {
	if a.Dir == "" {
		return &pstore{b: &a.mem}
	}
	return &pstore{b: &fileBlobs{root: filepath.Join(a.Dir, pendingDir)}}
}

// ready は依存が埋まっていることを確かめる(埋まっていなければ INTERNAL。panic しない)。
func (a *Approval) ready() error {
	if a == nil || a.State == nil || a.Recorder == nil || a.Clock == nil || a.Dir == "" {
		return internalErr() // Dir が空のメモリ置き場は試験の部品(memStore)だけ。製品の経路では動かさない
	}
	return nil
}

func (a *Approval) now(ctx context.Context) (time.Time, error) {
	t, err := a.Clock.Now(ctx)
	if err != nil {
		return t, err
	}
	return t.UTC().Truncate(time.Second), nil
}

// getItem は state.json の項目を v へ読む。項目が無い・null なら false。
func (a *Approval) getItem(ctx context.Context, item string, v any) (bool, error) {
	raw, err := a.State.Get(ctx, item)
	if err != nil {
		return false, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return false, nil
	}
	return true, json.Unmarshal(raw, v)
}

func (a *Approval) putItem(ctx context.Context, h contract.StateLockHandle, item string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return a.State.Update(ctx, h, item, b)
}

func (a *Approval) readDelays(ctx context.Context) ([]delayRec, error) {
	var d []delayRec
	_, err := a.getItem(ctx, "delays", &d)
	return d, err
}

func findDelay(ds []delayRec, id string) *delayRec {
	for i := range ds {
		if ds[i].ChangeID == id {
			return &ds[i]
		}
	}
	return nil
}

// dropDelay は終わった変更の delays[] の要素を消す(4.5)。state.lock を持つ呼び出し側から。
func (a *Approval) dropDelay(ctx context.Context, h contract.StateLockHandle, id string) error {
	ds, err := a.readDelays(ctx)
	if err != nil {
		return err
	}
	out := make([]delayRec, 0, len(ds))
	for _, d := range ds {
		if d.ChangeID != id {
			out = append(out, d)
		}
	}
	if len(out) == len(ds) {
		return nil
	}
	return a.putItem(ctx, h, "delays", out)
}

// record は事象を journal/ に写す(4.5 の項目 change: event と record)。state.lock を持つ呼び出し側から。
func (a *Approval) record(ctx context.Context, h contract.StateLockHandle, ev logEvent, rec json.RawMessage) error {
	if len(rec) == 0 {
		rec = json.RawMessage(`null`)
	}
	b, err := json.Marshal(map[string]any{"kind": "change", "body": map[string]any{"event": ev, "record": rec}})
	if err != nil {
		return err
	}
	_, err = a.Recorder.AppendLocked(ctx, h, b)
	return err
}

// emit は通知イベントを全通知先へ送る。送信の失敗は変更の状態を変えない(配送の結果は呼び出し側が材料にする)。
func (a *Approval) emit(ctx context.Context, typ string, facts map[string]any) []contract.DeliveryResult {
	b, err := json.Marshal(facts)
	if err != nil {
		return nil
	}
	_, res, err := a.Recorder.Emit(ctx, typ, b)
	if err != nil {
		return nil
	}
	return res
}

func stateOf(m map[string]*chgState, id string) *string {
	if st := m[id]; st != nil {
		s := st.Last
		return &s
	}
	return nil
}
