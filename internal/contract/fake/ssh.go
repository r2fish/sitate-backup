package fake

import (
	"context"
	"io"
	"sync"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// SSHCall は SSH.Run が受けた 1 回の実行の記録。Stdin は受けた標準入力の全体(Script のときは Script)。
type SSHCall struct {
	Req   contract.SSHRequest
	Stdin []byte
	Cred  string // Cred の値(試験が「資格情報が渡った」ことを確かめる。実物はログに出さない)
}

// SSHStep は 1 回の実行への応答。Err があればそれを返す(*contract.SSHError など)。SeenFP が空なら SSH.FP。
type SSHStep struct {
	Exit   int
	Stdout []byte
	SeenFP string
	Err    error
}

// SSH は contract.SSH の偽物。Handler があればそれが応答を決める(標準入力を読むかどうかも Handler の自由。開いたままの標準入力を試せる)。
// 無ければ Steps を順に使い(尽きたら終了 0・標準出力なし)、受けた標準入力を全部読んで記録する。
// 固定する指紋(PinnedFP)が見えた指紋と違えば、標準入力を読まず何も実行せずに *contract.SSHHostKeyError を返す。
type SSH struct {
	FP      string // 見える指紋の既定(空なら "SHA256:fakeSeen")
	Steps   []SSHStep
	Handler func(ctx context.Context, req contract.SSHRequest) (contract.SSHResult, error)

	mu    sync.Mutex
	Calls []SSHCall
	next  int
}

// Run は contract.SSH を満たす。
func (s *SSH) Run(ctx context.Context, req contract.SSHRequest) (contract.SSHResult, error) {
	if err := ctx.Err(); err != nil {
		return contract.SSHResult{}, err
	}
	var step SSHStep
	s.mu.Lock()
	if s.next < len(s.Steps) {
		step = s.Steps[s.next]
		s.next++
	}
	s.mu.Unlock()
	seen := step.SeenFP
	if seen == "" {
		seen = s.FP
	}
	if seen == "" {
		seen = "SHA256:fakeSeen"
	}
	if req.PinnedFP != "" && req.PinnedFP != seen {
		s.record(req, nil)
		return contract.SSHResult{SeenFP: seen}, &contract.SSHHostKeyError{Pinned: req.PinnedFP, Seen: seen}
	}
	if s.Handler != nil {
		s.record(req, nil)
		return s.Handler(ctx, req)
	}
	if step.Err != nil {
		s.record(req, nil)
		return contract.SSHResult{SeenFP: seen}, step.Err
	}
	var in []byte
	if len(req.Script) > 0 {
		in = req.Script
	} else if req.Stdin != nil {
		in, _ = io.ReadAll(req.Stdin)
	}
	s.record(req, in)
	if req.Stdout != nil && len(step.Stdout) > 0 {
		_, _ = req.Stdout.Write(step.Stdout)
	}
	return contract.SSHResult{ExitStatus: step.Exit, SeenFP: seen}, nil
}

func (s *SSH) record(req contract.SSHRequest, in []byte) {
	if in == nil && len(req.Script) > 0 {
		in = req.Script
	}
	c := SSHCall{Req: req, Stdin: in}
	if req.Cred != nil {
		c.Cred = string(req.Cred.Reveal())
	}
	s.mu.Lock()
	s.Calls = append(s.Calls, c)
	s.mu.Unlock()
}

// Recorded は記録した呼び出しの写し。
func (s *SSH) Recorded() []SSHCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SSHCall(nil), s.Calls...)
}
