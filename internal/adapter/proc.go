package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 取り消しの原因。
const (
	causeNone int32 = iota
	causeTimeout
	causeContext
	causeCallback
	causeOutput
)

// childEnv は子の環境(spec 3.2.1「環境」): PATH と LC_ALL=C のほかは、取得アダプタの SITATE_RECV と、試験ビルドだけの変数。
func (r *Runner) childEnv(kind string) []string {
	extra := map[string]string{}
	if v, ok := r.Env[contract.EnvRecv]; ok && kind == "acquire" {
		extra[contract.EnvRecv] = v
	}
	for k, v := range testBuildEnv() {
		extra[k] = v
	}
	return contract.AdapterEnv(extra)
}

// 核が結果を読み終えた後、孫が標準出力・標準エラーを握り続けても読み口を閉じるまでの待ち。
const defaultDrainWait = 2 * time.Second

// exec は子を自分のプロセスグループで起動し、要求を標準入力へ渡し、標準出力を読む。
// 経路を問わず、親が終わったらグループを SIGKILL で止める(孫を残さない。3.1.1 の規則 13)。
func (r *Runner) exec(ctx context.Context, rc contract.RunContext, l *loaded, kind, name string, line []byte, secretVals []string, onFrame func(contract.Frame) error) (json.RawMessage, error) {
	cctx := ctx
	if !rc.Deadline.IsZero() {
		var cancel context.CancelFunc
		cctx, cancel = context.WithDeadline(ctx, rc.Deadline)
		defer cancel()
	}
	cmd, stdin, stdout, stderr, err := r.startChild(rc, l, kind, name)
	if err != nil {
		return nil, err
	}
	pid := cmd.Process.Pid
	defer func() { _, _ = stdout.Close(), stderr.Close() }()
	go func() { _, _ = stdin.Write(append(line, '\n')); _ = stdin.Close() }()
	errc := collectStderr(stderr)
	var cause atomic.Int32
	done := make(chan struct{})
	exited := make(chan *os.ProcessState, 1)
	readDone := make(chan struct{})
	go func() {
		ps, _ := cmd.Process.Wait()
		killGroup(pid, syscall.SIGKILL) // 親が終わったら、経路を問わずグループに残ったものを止める
		select {                        // setsid で抜けた孫が読み口を握っていても、待たずに閉じる
		case <-time.After(r.drainOrDefault()):
			_ = stdout.Close()
			_ = stderr.Close()
		case <-readDone:
		}
		exited <- ps
	}()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // 時限の監視。SIGKILL まで送ったら読み口を閉じて読み取りを解く
		defer wg.Done()
		if r.watch(cctx, pid, r.limitFor(kind), &cause, done) {
			_, _ = stdout.Close(), stderr.Close()
		}
	}()
	out, st, cbErr := readOutput(kind, bufio.NewReaderSize(stdout, 64<<10), onFrame)
	switch st {
	case outCbErr:
		cause.CompareAndSwap(causeNone, causeCallback)
		killGroup(pid, syscall.SIGKILL)
	case outBad:
		cause.CompareAndSwap(causeNone, causeOutput)
		killGroup(pid, syscall.SIGKILL)
	}
	_, _ = io.Copy(io.Discard, stdout)
	se := <-errc // 標準エラーを読み終えてから親の終了を待つ
	close(readDone)
	ps := <-exited
	close(done)
	wg.Wait()
	if r.Stderr != nil {
		text, dropped := limitStderr(se.buf, se.total)
		r.Stderr(StderrRecord{RunID: rc.RunID, Kind: kind, Name: name, Text: redact(text, secretVals), Dropped: dropped})
	}
	return finish(kind, name, r.limitFor(kind), cause.Load(), cbErr, ps, out, st)
}

// startChild は子を自分のプロセスグループで起動する(Wrap があれば、その引数列を起動する)。
func (r *Runner) startChild(rc contract.RunContext, l *loaded, kind, name string) (cmd *exec.Cmd, in io.WriteCloser, out, errPipe io.ReadCloser, err error) {
	env := r.childEnv(kind)
	argv := []string{l.exe}
	if kind == "acquire" && rc.Wrap == nil && !r.allowBareAcquire { // 取得アダプタは容器の外で動かさない(spec 3.4。隔離が黙って外れないように)
		return nil, nil, nil, nil, internalErr(kind, name, "request")
	}
	if rc.Wrap != nil {
		// 検証(署名・sha256)は済んでいる。容器などで包んだ引数列(実行ファイルまで)を起動する
		w, werr := rc.Wrap(l.exe, env)
		if werr != nil || len(w) == 0 {
			return nil, nil, nil, nil, internalErr(kind, name, "request")
		}
		argv = w
	}
	cmd = exec.Command(argv[0], append(argv[1:len(argv):len(argv)], "run")...)
	cmd.Dir = "/"
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if rc.PdeathSig {
		setPdeath(cmd.SysProcAttr)
	}
	in, e1 := cmd.StdinPipe()
	out, e2 := cmd.StdoutPipe()
	errPipe, e3 := cmd.StderrPipe()
	if e1 != nil || e2 != nil || e3 != nil || cmd.Start() != nil {
		return nil, nil, nil, nil, internalErr(kind, name, "request")
	}
	return cmd, in, out, errPipe, nil
}

// finish は取り消しの原因、終了コード、標準出力の状態から結果を決める。
func finish(kind, name string, limit time.Duration, cause int32, cbErr error, ps *os.ProcessState, out []byte, st outStatus) (json.RawMessage, error) {
	switch cause {
	case causeCallback:
		return nil, cbErr
	case causeOutput:
		return nil, internalErr(kind, name, "output")
	case causeTimeout:
		return nil, timeoutErr(kind, name, int64(math.Ceil(limit.Seconds())))
	case causeContext:
		return nil, abortedErr("max_duration")
	}
	code := -1 // シグナルによる終了は -1
	if ps != nil {
		code = ps.ExitCode()
	}
	if code != 0 && code != 1 {
		return nil, internalErr(kind, name, "exit_code")
	}
	if st != outOK {
		return nil, internalErr(kind, name, "output")
	}
	return interpret(kind, name, code, out)
}

// watch は時限(kind ごとの時限と実行の残りの早い方)で子のプロセスグループに SIGTERM、猶予の後に SIGKILL を送る。
// SIGKILL まで送ったときは true を返す(呼び出し側が読み口を閉じて読み取りを解く)。
func (r *Runner) watch(ctx context.Context, pid int, limit time.Duration, cause *atomic.Int32, done <-chan struct{}) bool {
	t := time.NewTimer(limit)
	defer t.Stop()
	select {
	case <-done:
		return false
	case <-t.C:
		cause.CompareAndSwap(causeNone, causeTimeout)
	case <-ctx.Done():
		cause.CompareAndSwap(causeNone, causeContext)
	}
	killGroup(pid, syscall.SIGTERM)
	g := time.NewTimer(r.graceOrDefault())
	defer g.Stop()
	select {
	case <-done:
		return false
	case <-g.C:
		killGroup(pid, syscall.SIGKILL)
		return true
	}
}

func killGroup(pid int, sig syscall.Signal) { _ = syscall.Kill(-pid, sig) }

type stderrData struct {
	buf   []byte
	total int64
}

// collectStderr は標準エラーの先頭の ADAPTER_STDERR_MAX だけを受け、残りは捨てて数える。
func collectStderr(rd io.Reader) <-chan stderrData {
	c := make(chan stderrData, 1)
	go func() {
		buf, _ := io.ReadAll(io.LimitReader(rd, contract.AdapterStderrMax))
		rest, _ := io.Copy(io.Discard, rd)
		c <- stderrData{buf: buf, total: int64(len(buf)) + rest}
	}()
	return c
}
