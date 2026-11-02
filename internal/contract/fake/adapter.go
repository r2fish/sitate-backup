package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Outcome は偽の AdapterRunner の台本の 1 件の結果の種類。
type Outcome int

// 台本の結果。
const (
	OutcomeSuccess      Outcome = iota // 結果 JSON を返す
	OutcomeError                       // アダプタの error(終了 1)
	OutcomeTimeout                     // 時限切れ(ADAPTER_TIMEOUT)
	OutcomeLostResponse                // 発行は成功したが結果を返さない
	OutcomeCheckFail                   // check の失敗(findings つきの結果)
	OutcomeOmitted                     // 省いた件数(stats.errors_omitted・stats.warnings_omitted)
)

// Step は台本の 1 件。Frames は取得アダプタが結果の前に流すフレーム。
type Step struct {
	Outcome Outcome
	Result  json.RawMessage
	Frames  []contract.Frame
	// ErrCode は OutcomeError のアダプタの error.code(ADAPTER_ERROR の detail.adapter_code に入る。空なら ADAPTER_FAILED)。
	ErrCode string
	// ErrorsOmitted・WarningsOmitted は OutcomeOmitted で stats に載せる件数。
	ErrorsOmitted, WarningsOmitted int
}

// AdapterRunner は contract.AdapterRunner の偽物。kind ごとの台本を順に消費し、呼び出しを記録する。
// 偽の取得アダプタ(acquire)・偽の復元先アダプタ(restore-dest)・偽の保管先準備アダプタ(provision)を演じる。
// 台本を渡す関数 Script を呼ぶのは各 WP の _test.go だけにする(製品ビルドの成果物に入らない)。
type AdapterRunner struct {
	mu     sync.Mutex
	script map[string][]Step
	Calls  []Call
}

// Call は呼び出しの記録。
type Call struct {
	Kind, Name string
	Req        json.RawMessage
	RC         contract.RunContext
}

var _ contract.AdapterRunner = (*AdapterRunner)(nil)

// NewAdapterRunner は空の台本の偽物を返す。
func NewAdapterRunner() *AdapterRunner { return &AdapterRunner{script: map[string][]Step{}} }

// Script は kind の台本に結果を足す(kind: acquire / check / restore-dest / notify / provision)。
func (a *AdapterRunner) Script(kind string, steps ...Step) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.script[kind] = append(a.script[kind], steps...)
}

// Declaration は contract.AdapterRunner を満たす。
func (a *AdapterRunner) Declaration(_ context.Context, kind, name string) (contract.Declaration, error) {
	return contract.Declaration{Kind: kind, Name: name, Version: "0.0.0-fake", Raw: json.RawMessage(`{}`)}, nil
}

func (a *AdapterRunner) next(kind string) (Step, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	q := a.script[kind]
	if len(q) == 0 {
		return Step{}, false
	}
	a.script[kind] = q[1:]
	return q[0], true
}

func adapterErr(code contract.Code, detail string) *contract.Error {
	info := contract.CodeTable[code]
	return &contract.Error{Code: code, Class: contract.ExitClass(info.Exit), Retryable: info.Retryable, Detail: json.RawMessage(detail), Next: []contract.Next{}}
}

// Run は contract.AdapterRunner を満たす。台本が尽きたら NOT_IMPLEMENTED 相当の INTERNAL を返す。
func (a *AdapterRunner) Run(ctx context.Context, rc contract.RunContext, kind, name string, req json.RawMessage, onFrame func(contract.Frame) error) (json.RawMessage, error) {
	a.mu.Lock()
	a.Calls = append(a.Calls, Call{kind, name, req, rc})
	a.mu.Unlock()
	st, ok := a.next(kind)
	if !ok {
		return nil, adapterErr(contract.CodeInternal, fmt.Sprintf(`{"component":"adapter","reason":"script_exhausted","kind":%q,"name":%q}`, kind, name))
	}
	for _, f := range st.Frames {
		if onFrame != nil {
			if err := onFrame(f); err != nil {
				return nil, err
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	switch st.Outcome {
	case OutcomeError:
		ac := st.ErrCode
		if ac == "" {
			ac = "ADAPTER_FAILED"
		}
		return nil, adapterErr(contract.CodeAdapterError, fmt.Sprintf(`{"kind":%q,"name":%q,"adapter_code":%q}`, kind, name, ac))
	case OutcomeTimeout:
		return nil, adapterErr(contract.CodeAdapterTimeout, fmt.Sprintf(`{"kind":%q,"name":%q}`, kind, name))
	case OutcomeLostResponse:
		return nil, adapterErr(contract.CodeAdapterError, fmt.Sprintf(`{"kind":%q,"name":%q,"lost_response":true}`, kind, name))
	case OutcomeOmitted:
		return json.RawMessage(fmt.Sprintf(`{"stats":{"errors_omitted":%d,"warnings_omitted":%d}}`, st.ErrorsOmitted, st.WarningsOmitted)), nil
	}
	if st.Result == nil {
		return json.RawMessage(`{}`), nil
	}
	return st.Result, nil
}
