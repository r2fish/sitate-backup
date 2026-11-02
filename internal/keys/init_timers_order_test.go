package keys

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type failingTimers struct{}

func (failingTimers) Register(context.Context, []contract.TimerUnit) error {
	return errors.New("タイマーの登録に失敗")
}
func (failingTimers) SetOuterLimit(context.Context, time.Duration) error { return nil }

// 主経路の第 2 段で見つけた順序の誤り: init はタイマーの登録に失敗したら、状態(initialized など)を進めない。
// 状態だけが進むと、再実行が確定前の再実行（保留の作り直し）にならず、タイマーの無い初期化済みの状態が残る。
func TestKeysInitDoesNotAdvanceStateWhenTimersFail(t *testing.T) {
	e, call := initPhraseEnv()
	e.k.Timers = failingTimers{}
	if _, err := e.k.Init(ctx, call); err == nil {
		t.Fatal("タイマーの登録に失敗したのに init が成功した")
	}
	for _, item := range []string{"initialized", "repo_id", "approval_key", "kit"} {
		if raw, ok := e.st.Items[item]; ok {
			t.Errorf("タイマーの登録に失敗したのに状態 %s が進んだ: %s", item, raw)
		}
	}
}
