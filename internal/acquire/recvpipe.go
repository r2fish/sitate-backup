package acquire

// 呼び出し口 SITATE_RECV(spec 3.2.2): 容器の中の補助コマンド(RunRecvHelper)と、核の側の中継(recvRelay)。
// 通信路は核が作業ディレクトリに作る 2 本の名前付きパイプ(FIFO。ソケットとネットワークは使わない)で、枠付けは sitate.recvpipe/v1(schema/receiver/pipe.json)。
// 受け口の操作は SSH で核が容器の外から呼ぶ(pull 用鍵は容器に渡さない)。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 容器の中で補助コマンドが見る環境変数の名前と、パイプの置き場。
const (
	// EnvRecvPipe は補助コマンドの動作を選ぶ環境変数(値は容器の中のパイプのディレクトリ)。核の実行ファイル自身を SITATE_RECV に置くため、cmd/sitate の main は
	// この変数が空でなければ RunRecvHelper を呼んで終わる(U1f-1 の結線)。
	EnvRecvPipe = "SITATE_RECVPIPE"
	pipeReqName = "req"
	pipeRepName = "rep"
	// maxRelayLine は要求と {in:n} の 1 行の上限(改行の無い入力でメモリを増やさない)。
	maxRelayLine = 4 << 10
	// relayWriteTimeout は補助コマンドへ出力を書く待ちの上限(読み手が消えたとき書き手を止めない)。
	relayWriteTimeout = 30 * time.Second
	relayStopWait     = 3 * time.Second
	maxDeniedKept     = 100
)

// maxRelayInput は 1 回の呼び出しの入力(read-batch のパス一覧)の上限。超えたら 126(試験が小さくする)。
var maxRelayInput = 64 << 20

// 補助コマンドの終了状態(受け口の終了状態と重ならない値は contract.RecvExitDenied / RecvExitUnreachable)。
var helperOps = []string{"list", "read-batch", "dump-pg-globals", "dump-pg", "dump-my"}

// RunRecvHelper は "$SITATE_RECV" <操作> <target-id> の本体。標準入力を核へ送り、核から受けた出力を標準出力へ書き、受け口の終了状態を返す。
// 形の合わない呼び出しは 126、核に届かない・終了状態を得る前に閉じたときは 255。
func RunRecvHelper(args []string, stdin io.Reader, stdout io.Writer, pipeDir string) int {
	if len(args) != 2 || !inList(helperOps, args[0]) {
		return contract.RecvExitDenied
	}
	req, err := os.OpenFile(filepath.Join(pipeDir, pipeReqName), os.O_WRONLY, 0)
	if err != nil {
		return contract.RecvExitUnreachable
	}
	rep, err := os.OpenFile(filepath.Join(pipeDir, pipeRepName), os.O_RDONLY, 0)
	if err != nil {
		req.Close()
		return contract.RecvExitUnreachable
	}
	defer rep.Close()
	if sendHelperRequest(req, args, stdin) != nil {
		req.Close()
		return contract.RecvExitUnreachable
	}
	req.Close()
	return readHelperReply(bufio.NewReader(rep), stdout)
}

func sendHelperRequest(w io.Writer, args []string, stdin io.Reader) error {
	head, _ := json.Marshal(contract.PipeRequest{Format: contract.RecvPipeFormat, Op: args[0], Target: args[1]})
	if _, err := w.Write(append(head, '\n')); err != nil {
		return err
	}
	buf := make([]byte, contract.StreamFragmentMax)
	for {
		n, rerr := stdin.Read(buf)
		if n > 0 {
			if _, err := fmt.Fprintf(w, "{\"in\":%d}\n", n); err != nil {
				return err
			}
			if _, err := w.Write(buf[:n]); err != nil {
				return err
			}
		}
		if rerr != nil {
			break
		}
	}
	_, err := io.WriteString(w, "{\"in\":0}\n")
	return err
}

func readHelperReply(br *bufio.Reader, stdout io.Writer) int {
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return contract.RecvExitUnreachable
		}
		var f struct {
			Out  *int `json:"out"`
			Exit *int `json:"exit"`
		}
		if json.Unmarshal(line, &f) != nil {
			return contract.RecvExitUnreachable
		}
		switch {
		case f.Exit != nil:
			return *f.Exit
		case f.Out != nil && *f.Out > 0 && *f.Out <= contract.StreamFragmentMax:
			if _, err := io.CopyN(stdout, br, int64(*f.Out)); err != nil {
				return contract.RecvExitUnreachable
			}
		default:
			return contract.RecvExitUnreachable
		}
	}
}

// recvRelay は 1 つの対象の取得の間だけ動く核の側の中継。受けた操作を SSH で受け口へ流す(同時に 1 つ)。
type recvRelay struct {
	target string
	ssh    contract.SSH
	conn   contract.SSHConn
	req    *os.File
	rep    *os.File
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	closed bool
	fatal  error // ホスト鍵の不一致など、対象の失敗にしない誤り
	denied []string
}

// startRelay は dir に FIFO を作って中継を始める。stop で必ず戻る(読み書きの期限と文脈の取り消しで解く)。
func startRelay(ctx context.Context, dir, target string, s contract.SSH, conn contract.SSHConn) (*recvRelay, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for _, n := range []string{pipeReqName, pipeRepName} {
		p := filepath.Join(dir, n)
		if err := syscall.Mkfifo(p, 0o666); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		_ = os.Chmod(p, 0o666)
	}
	cctx, cancel := context.WithCancel(ctx)
	r := &recvRelay{target: target, ssh: s, conn: conn, cancel: cancel, done: make(chan struct{})}
	var err error
	// O_RDWR で開く(相手が開くまで止まらず、補助コマンドが閉じても EOF にならない)。
	if r.req, err = os.OpenFile(filepath.Join(dir, pipeReqName), os.O_RDWR, 0); err != nil {
		cancel()
		return nil, err
	}
	if r.rep, err = os.OpenFile(filepath.Join(dir, pipeRepName), os.O_RDWR, 0); err != nil {
		r.req.Close()
		cancel()
		return nil, err
	}
	go r.loop(cctx)
	return r, nil
}

// stop は中継を止める。読み待ち・書き待ち・受け口への SSH のどこにいても数秒で戻る。
func (r *recvRelay) stop() {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	r.cancel()
	past := time.Now().Add(-time.Second)
	_ = r.req.SetDeadline(past) // 期限が使えない環境では、下の起こす 1 バイトと待ちの上限で戻る
	_ = r.rep.SetDeadline(past)
	select {
	case <-r.done:
	case <-time.After(relayStopWait):
	}
	r.req.Close()
	r.rep.Close()
}

func (r *recvRelay) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func (r *recvRelay) fatalErr() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fatal
}

// Denied は拒否した操作(受け口へ送らなかったもの。上限つき)の記録。
func (r *recvRelay) Denied() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.denied...)
}

func (r *recvRelay) deny(what string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.denied) < maxDeniedKept {
		r.denied = append(r.denied, what)
	}
}

var errLineTooLong = errors.New("pipe: 行が長すぎる")

// readLine は改行までを上限つきで読む(上限を超えたら誤り。メモリを増やさない)。
func readLine(br *bufio.Reader, max int) ([]byte, error) {
	var out []byte
	for {
		part, err := br.ReadSlice('\n')
		out = append(out, part...)
		if len(out) > max {
			return nil, errLineTooLong
		}
		switch {
		case err == nil:
			return out, nil
		case errors.Is(err, bufio.ErrBufferFull):
		default:
			return nil, err
		}
	}
}

func (r *recvRelay) loop(ctx context.Context) {
	defer close(r.done)
	br := bufio.NewReaderSize(r.req, 4096)
	for {
		line, err := readLine(br, maxRelayLine)
		if err != nil || r.isClosed() {
			return
		}
		var rq contract.PipeRequest
		bad := contract.Validate("receiver/pipe.json#/$defs/request", line) != nil || json.Unmarshal(line, &rq) != nil
		in, tooBig, ierr := readRelayInput(br)
		if ierr != nil {
			return
		}
		code := contract.RecvExitDenied
		if tooBig {
			r.deny("input too large")
		} else {
			code = r.serve(ctx, rq, in, bad)
		}
		r.reply(fmt.Sprintf("{\"exit\":%d}\n", code))
	}
}

func (r *recvRelay) reply(s string) {
	r.armWrite()
	_, _ = io.WriteString(r.rep, s)
}

// armWrite は rep への書き待ちに期限を付ける。止めた後は過去の期限のままにする(書き待ちに戻らない)。
func (r *recvRelay) armWrite() {
	d := time.Now().Add(relayWriteTimeout)
	if r.isClosed() {
		d = time.Now().Add(-time.Second)
	}
	_ = r.rep.SetWriteDeadline(d)
}

// readRelayInput は {in:n} と本体の繰り返しを in:0 まで読む。上限を超えた分は捨てて tooBig を返す(補助コマンドを書き待ちで止めない。捨てる量にも上限)。
func readRelayInput(br *bufio.Reader) (in []byte, tooBig bool, err error) {
	var total int64
	for {
		line, err := readLine(br, maxRelayLine)
		if err != nil {
			return nil, false, err
		}
		var f struct{ In int }
		if contract.Validate("receiver/pipe.json#/$defs/in", line) != nil || json.Unmarshal(line, &f) != nil {
			return nil, false, errors.New("pipe: in の形")
		}
		if f.In == 0 {
			return in, tooBig, nil
		}
		total += int64(f.In)
		if total > int64(4*maxRelayInput) {
			return nil, false, errors.New("pipe: 入力が大きすぎる")
		}
		if tooBig || len(in)+f.In > maxRelayInput {
			tooBig, in = true, nil
			if _, err := io.CopyN(io.Discard, br, int64(f.In)); err != nil {
				return nil, false, err
			}
			continue
		}
		b := make([]byte, f.In)
		if _, err := io.ReadFull(br, b); err != nil {
			return nil, false, err
		}
		in = append(in, b...)
	}
}

// serve は 1 つの操作を受け口へ流して終了状態を返す。selftest・survey など許可外の操作と、要求の対象 id と違う id は流さずに 126。
func (r *recvRelay) serve(ctx context.Context, rq contract.PipeRequest, in []byte, bad bool) int {
	if bad || rq.Target != r.target || !inList(helperOps, rq.Op) {
		r.deny(rq.Op + " " + rq.Target)
		return contract.RecvExitDenied
	}
	req := contract.SSHRequest{SSHConn: r.conn, Argv: []string{rq.Op, rq.Target}, Stdin: bytes.NewReader(in), Stdout: &outFramer{f: r.rep, arm: r.armWrite}}
	res, err := r.ssh.Run(ctx, req)
	if err != nil {
		var hk *contract.SSHHostKeyError
		if errors.As(err, &hk) {
			r.mu.Lock()
			r.fatal = errHostKey(hk.Pinned, hk.Seen)
			r.mu.Unlock()
		}
		return contract.RecvExitUnreachable
	}
	return res.ExitStatus
}

// outFramer は受け口の標準出力を {out:n} の枠に包んで書く(書き待ちに期限を付ける)。
type outFramer struct {
	f   *os.File
	arm func()
}

func (o *outFramer) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n := len(p)
		if n > contract.StreamFragmentMax {
			n = contract.StreamFragmentMax
		}
		o.arm()
		if _, err := fmt.Fprintf(o.f, "{\"out\":%d}\n", n); err != nil {
			return total, err
		}
		if _, err := o.f.Write(p[:n]); err != nil {
			return total, err
		}
		total += n
		p = p[n:]
	}
	return total, nil
}
