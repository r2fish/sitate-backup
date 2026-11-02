package adapter

import (
	"context"
	"encoding/json"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Runner は contract.AdapterRunner の実物。署名の検証そのもの(信頼の起点)は持たず、Verifier を呼ぶだけ。
// Root はアダプタの置き場(<Root>/<kind>/<name>/{adapter.json,adapter.json.sig,<exe>})、
// Version・Arch は trusted comment の期待値(spec 3.2.1)に使う動いている核が属する配布物の版とホストの <os>-<arch>。
type Runner struct {
	Root     string
	Version  string
	Arch     string
	Verifier contract.Verifier
	// Env は子に渡す追加の環境変数。受けるのは取得アダプタの SITATE_RECV だけ(それ以外の名前は捨てる)。
	// 試験の切り替えと追加の CA の変数は、試験ビルドの核だけが自分の環境から渡す(製品ビルドでは渡らない)。
	Env map[string]string
	// Stderr は子の標準エラー(先頭の ADAPTER_STDERR_MAX、最後の改行まで、伏せ字済み)を実行の記録へ渡す口。
	// 結果・error・通知には出さない。nil なら捨てる。
	Stderr func(StderrRecord)

	// 試験用: kind ごとの時限と SIGTERM から SIGKILL までの猶予の差し替え(0 なら既定)。
	timeouts map[string]time.Duration
	grace    time.Duration
	drain    time.Duration
}

// StderrRecord は実行の記録に書く子の標準エラー。Text は伏せ字を当てた後の文、Dropped は捨てたバイト数。
type StderrRecord struct {
	RunID, Kind, Name string
	Text              string
	Dropped           int64
}

var _ contract.AdapterRunner = (*Runner)(nil)

// Declaration は署名を検証した adapter.json の内容だけを返す(spec 3.2.1、7.5)。
func (r *Runner) Declaration(ctx context.Context, kind, name string) (contract.Declaration, error) {
	l, err := r.load(ctx, kind, name)
	if err != nil {
		return contract.Declaration{}, err
	}
	return l.decl, nil
}

// Run は要求 JSON を渡して結果 JSON を返す。起動の前に署名・trusted comment・sha256 を確かめ、
// 合わなければ起動しない。要求と結果と adapter.json の contract の版を照合する。
func (r *Runner) Run(ctx context.Context, rc contract.RunContext, kind, name string, req json.RawMessage, onFrame func(contract.Frame) error) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, abortedErr("max_duration")
	}
	if _, ok := contract.AdapterContract[kind]; !ok {
		return nil, internalErr(kind, name, "request")
	}
	var top struct {
		Contract string `json:"contract"`
	}
	if json.Unmarshal(req, &top) != nil || top.Contract != contract.AdapterContract[kind] {
		// 版の違う要求では adapter.json の検証へも進まない(アダプタを起動しない)
		return nil, internalErr(kind, name, "contract")
	}
	l, err := r.load(ctx, kind, name)
	if err != nil {
		return nil, err
	}
	if err := l.checkExe(); err != nil {
		return nil, err
	}
	line, secretVals, err := prepRequest(kind, name, l.secrets, req)
	if err != nil {
		return nil, err
	}
	return r.exec(ctx, rc, l, kind, name, line, secretVals, onFrame)
}

// limitFor は kind ごとの時限(spec 3.2.1。kind check は CHECK_TIMEOUT)。
func (r *Runner) limitFor(kind string) time.Duration {
	if d, ok := r.timeouts[kind]; ok {
		return d
	}
	switch kind {
	case "acquire":
		return contract.AdapterTimeoutAcquire
	case "restore-dest":
		return contract.AdapterTimeoutRestoreDest
	case "notify":
		return contract.AdapterTimeoutNotify
	case "provision":
		return contract.AdapterTimeoutProvision
	}
	return contract.CheckTimeout
}

func (r *Runner) drainOrDefault() time.Duration {
	if r.drain > 0 {
		return r.drain
	}
	return defaultDrainWait
}

func (r *Runner) graceOrDefault() time.Duration {
	if r.grace > 0 {
		return r.grace
	}
	return contract.AdapterKillGrace
}
