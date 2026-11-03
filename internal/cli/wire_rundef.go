package cli

// seal の前の取得・復元テストが使う定義(spec 3.1.3 の NOT_SEALED、D-0025 の 5: 取得・延長・照合・復元テストは seal の前も止めない)。
// 承認済みの定義(def/approved.json)は seal の前には無い。止めないこれらの操作は、候補の定義(def validate に通った最新の内容)を読む。
// 取得の入口(acquire.Ingest)は自分で候補へ寄せる。ここで寄せるのは、取得の後に cli が承認済みの定義を読む所(マニフェストの定義の写し、索引の再構成)と、
// 復元テストの部品(restore.Restore)が読む所。定義を扱う他のサブコマンド(def・notify・status など)は NOT_SEALED を見て分岐するため寄せない。

import (
	"context"
	"sync/atomic"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/restore"
)

// candidateFallbackState は ReadDef が NOT_SEALED のとき、active が真なら候補を返す(候補も無ければ NOT_SEALED のまま)。
type candidateFallbackState struct {
	contract.StateFiles
	active func() bool
}

func (s candidateFallbackState) ReadDef(ctx context.Context) ([]byte, string, error) {
	raw, h, err := s.StateFiles.ReadDef(ctx)
	if e, ok := err.(*contract.Error); !ok || e.Code != contract.CodeNotSealed || !s.active() {
		return raw, h, err
	}
	if raw, gerr := s.StateFiles.Get(ctx, "sealed"); gerr == nil && string(raw) == "true" {
		return nil, h, err // seal の後に approved.json だけが失われた状態では、候補へ開かない
	}
	cand, ch, found, cerr := s.StateFiles.ReadCandidate(ctx)
	if cerr != nil || !found {
		return raw, h, err
	}
	return cand, ch, nil
}

// restoreTestOnCandidate は restore-test と、導入の手順 9 の restore-dest enroll・restore-dest check(seal の前に行う)を、候補の定義を読める部品で動かす
// (list・restore などは従来の部品。restore の seal の前の拒否(NOT_SEALED)を変えない)。
type restoreTestOnCandidate struct {
	restoreIface
	test contract.Restore
	skip func(context.Context, contract.Call) (contract.Reply, bool) // 導入中のタイマーの飛ばし(timerskip.go)
}

// restoreIface は contract.Restore の別名(メソッド Restore と埋め込みの欄の名前がぶつからないようにする)。
type restoreIface = contract.Restore

func (r *restoreTestOnCandidate) RestoreTest(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if rep, skip := r.skip(ctx, c); skip {
		return rep, nil
	}
	return r.test.RestoreTest(ctx, c)
}

func (r *restoreTestOnCandidate) RestoreDestEnroll(ctx context.Context, c contract.Call) (contract.Reply, error) {
	return r.test.RestoreDestEnroll(ctx, c)
}

func (r *restoreTestOnCandidate) RestoreDestCheck(ctx context.Context, c contract.Call) (contract.Reply, error) {
	return r.test.RestoreDestCheck(ctx, c)
}

// wireRunDef は wireParts の後に、seal の前も止めない操作の定義の読み先を結ぶ。
func wireRunDef(d *Deps) {
	if d.State == nil {
		return
	}
	ingesting := &atomic.Bool{}
	d.State = candidateFallbackState{StateFiles: d.State, active: ingesting.Load}
	if orig := d.Ingest; orig != nil {
		d.Ingest = func(ctx context.Context, c contract.Call) (IngestOut, error) {
			ingesting.Store(true) // この実行は取得の流れ(backup)。以後の定義の読みは候補へ寄せる
			return orig(ctx, c)
		}
	}
	if r, ok := d.Restore.(*restore.Restore); ok {
		cp := *r
		cp.State = candidateFallbackState{StateFiles: r.State, active: func() bool { return true }}
		d.Restore = &restoreTestOnCandidate{restoreIface: r, test: &cp, skip: d.skipTimerSetup}
	}
}
