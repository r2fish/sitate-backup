// cli はサブコマンドの配線、封筒、原因コード、実行の排他(Lease)、gate に従う実行、backup の流れ、実物の結合(注入)。
// このファイルは入口: 引数の解釈 → 前提の確認 → 部品の入口 → 封筒。

package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Main は sitate の入口(cmd/sitate が呼ぶ)。標準出力へ封筒を 1 行出して終了コードを返す。
func Main(args []string, stdout, stderr io.Writer) int {
	return mainEntry(args, os.Stdin, stdout, stderr, os.Getenv)
}

// Run は注入済みの Deps で 1 回の呼び出しを実行する(試験と結線の入口)。
func Run(d *Deps, args []string, stdout, _ io.Writer) int {
	d.out = stdout
	o := runTop(context.Background(), d, args, stdout)
	b, code := o.render()
	writeLine(stdout, b)
	return code
}

// runTop は gate なら gate の流れ、それ以外は直接の呼び出し。秘密の出力(フレーズ)はここで書く(gate.go)。
func runTop(ctx context.Context, d *Deps, args []string, stdout io.Writer) outcome {
	if len(args) > 0 && args[0] == "gate" {
		return d.gate(ctx, args[1:], stdout)
	}
	o := execute(ctx, d, args, secretNone)
	return d.deliverPhrase(o, stdout, secretNone, o.secretFD)
}

// execute は 1 回の呼び出しを outcome にする。mode は gate が読み替えた秘密の出力先。
func execute(ctx context.Context, d *Deps, args []string, mode secretMode) outcome {
	p, cmd, perr := parseArgs(args)
	if perr != nil {
		return outcome{command: cmd, err: perr}
	}
	o := outcome{command: p.sub.Words, dryRun: p.dryRun}
	if p.sub.Public == contract.PublicTestOnly {
		return finish(o, mustReply(testTick(ctx, d, args[len(p.sub.Words):])), false)
	}
	if e := precheck(ctx, d, p); e != nil {
		o.err = e
		return o
	}
	if p.runID != "" {
		id := p.runID
		o.runID = &id
	} else if takesRunLock(p) {
		id, err := d.newRunID(ctx)
		if err != nil {
			o.err = toError(err)
			return o
		}
		o.runID = &id
	}
	if p.secretFD != nil {
		fd := *p.secretFD
		o.secretFD = &fd
	}
	call, cerr := buildCall(ctx, d, p, o.runID)
	call.SecretOut = call.SecretOut || mode == secretStdout
	if cerr != nil {
		o.err = cerr
		return o
	}
	ctx = withCall(ctx, call)      // 保管先の所在と apply の時計が引く
	if call.SnapshotKeyIn != nil { // 1 行目の後も標準入力を読み続け、終わり(SSH の切断)で実行を取り消す
		var cancel context.CancelCauseFunc
		ctx, cancel = context.WithCancelCause(ctx)
		defer cancel(nil)
		go d.watchStdin(cancel)
	}
	e := route(p.sub.Words)
	if p.byTimer { // タイマーで起動した各実行の始めに自動の削除停止を判定する。LOCKED を返す経路でも行う(規則 16)
		rid := ""
		if o.runID != nil {
			rid = *o.runID
		}
		d.autoHold(ctx, rid)
	}
	if takesRunLock(p) {
		return d.lockedRun(ctx, o, p, call, e)
	}
	return d.runUnlocked(ctx, o, e, call, strings.Join(p.sub.Words, " "))
}

type replyOrErr struct {
	rep contract.Reply
	err error
}

func mustReply(r contract.Reply, err error) replyOrErr { return replyOrErr{r, err} }

// finish は入口の結果を outcome にする。フレーズは出力先(secretOut)があるときだけ受ける(無いのに返った秘密は消して INTERNAL)。
func finish(o outcome, r replyOrErr, secretOut bool) outcome {
	if r.err != nil {
		if r.rep.Phrase != nil {
			r.rep.Phrase.Wipe()
		}
		o.err = toError(r.err)
		return o
	}
	if r.rep.Phrase != nil {
		if !secretOut {
			r.rep.Phrase.Wipe()
			o.err = internalErr()
			return o
		}
		o.phrase = r.rep.Phrase
	}
	o.result, o.warnings = r.rep.Result, r.rep.Warnings
	return o
}

// takesRunLock はロックを取る実行か(spec 3.1.1 の「同時実行」)。run_id を核が作る対象。
func takesRunLock(p *parsed) bool {
	switch strings.Join(p.sub.Words, " ") {
	case "backup", "extend", "prune", "restore-test", "restore-dest teardown", "apply":
		return true
	case "restore": // 復元先と宛先で動く restore(スナップショット鍵を標準入力で受ける形と --relay)は状態を持たず、ロックを取らない(規則 6)
		_, viaKey := p.args["snapshot-key-stdin"]
		_, relay := p.args["relay"]
		return !viaKey && !relay
	case "init":
		_, adopt := p.args["adopt"]
		return adopt
	}
	return false
}

// newRunID は開始時(ロックの前)に run_id を作る。実行中の印・last_runs[] と同じなら 1 回だけ作り直す(spec 3.1.1)。
func (d *Deps) newRunID(ctx context.Context) (string, error) {
	now, err := d.Clock.Now(ctx)
	if err != nil {
		return "", err
	}
	return newRunID(now, func(id string) bool { return d.runIDInUse(ctx, id) }), nil
}

func (d *Deps) runIDInUse(ctx context.Context, id string) bool {
	if m, st := readMarker(d.StateDir); st == markerOK && m.RunID == id { // 実行中の印の run_id
		return true
	}
	v, err := d.State.Get(ctx, "last_runs")
	if err != nil {
		return false
	}
	var runs []struct {
		RunID string `json:"run_id"`
	}
	_ = json.Unmarshal(v, &runs)
	for _, r := range runs {
		if r.RunID == id {
			return true
		}
	}
	return false
}

// safeEntry は部品の入口を呼び、panic を INTERNAL の誤りにする(例: 鍵が読めないときの KeyService の panic)。
// panic の値は秘密を含み得るので封筒にも記録にも入れない。ロックの本体の中では、戻った後に記録 → 印の消去の順が崩れない。
func safeEntry(e entry, d *Deps, ctx context.Context, call contract.Call) (rep contract.Reply, err error) {
	defer func() {
		if recover() != nil {
			rep, err = contract.Reply{}, internalErr()
		}
	}()
	return e(d, ctx, call)
}

// watchStdin は標準入力を終わりまで読み捨て、終わったら実行を errStdinClosed で取り消す。
func (d *Deps) watchStdin(cancel context.CancelCauseFunc) {
	_, _ = io.Copy(io.Discard, d.stdinReader())
	cancel(errStdinClosed)
}
