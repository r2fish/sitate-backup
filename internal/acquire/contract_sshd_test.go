package acquire

// 契約試験の道具: 元サーバーの代わりをする、試験の中の ssh サーバー(127.0.0.1)と、状態ファイル・入口の呼び出しの組み立て。
// 実装は注入された contract.SSH で管理アクセスに接続し(実物の ssh サーバーは起こさず、routeSSH が偽の元サーバーへ振り分ける)、核が生成した処理を標準入力で `sh -s`(root)か `sudo -n sh -s` に渡す(spec 3.3「接続先と資格情報」「実行の仕方」)。
// このサーバーは受けた実行(ユーザー・コマンド・標準入力)を記録し、台本の関数で標準出力と終了状態を返す。

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

type execRec struct{ User, Cmd, Stdin string }

type fakeSource struct {
	Port int
	FP   string // サーバーのホスト鍵の指紋(SHA256:...)
	// Reply は受けた実行に対する標準出力と終了状態。
	Reply  func(execRec) (string, int)
	Reject bool // 公開鍵認証をすべて拒否する

	mu     sync.Mutex
	execs  []execRec
	conns  int
	ln     io.Closer
	closed bool
}

func (s *fakeSource) Execs() []execRec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]execRec(nil), s.execs...)
}

func (s *fakeSource) Conns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conns
}

func (s *fakeSource) URL(user string) string {
	return "ssh://" + user + "@127.0.0.1:" + strconv.Itoa(s.Port)
}

var (
	srcMu   sync.Mutex
	srcByPt = map[int]*fakeSource{}
)

type stubCloser struct{ s *fakeSource }

func (c stubCloser) Close() error { c.s.mu.Lock(); c.s.closed = true; c.s.mu.Unlock(); return nil }

// startSource は元サーバーの代わりの偽の SSH(contract.SSH を満たす routeSSH が、接続先のポートで引く)を起こす。
// pull 接続のポートは 22 固定(spec 3.3)なので、最後に起こしたものがポート 22 の相手になる。
// reply が nil なら標準出力なし・終了 0。実物の ssh サーバーは起こさない(接続の実物は結合 U1c・U1f-2 の持ち物)。
func startSource(t *testing.T, reply func(execRec) (string, int)) *fakeSource {
	t.Helper()
	_, hk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(hk)
	if err != nil {
		t.Fatal(err)
	}
	srcMu.Lock()
	s := &fakeSource{Port: 22, FP: ssh.FingerprintSHA256(signer.PublicKey()), Reply: reply}
	srcByPt[s.Port] = s
	srcMu.Unlock()
	s.ln = stubCloser{s}
	return s
}

// routeSSH は接続先のポートで fakeSource を選ぶ contract.SSH。
type routeSSH struct{}

func (routeSSH) Run(ctx context.Context, req contract.SSHRequest) (contract.SSHResult, error) {
	srcMu.Lock()
	s := srcByPt[req.Target.Port]
	srcMu.Unlock()
	if s == nil {
		return contract.SSHResult{}, &contract.SSHError{Reason: contract.SSHReasonConnect}
	}
	s.mu.Lock()
	closed, reject := s.closed, s.Reject
	s.mu.Unlock()
	if closed {
		return contract.SSHResult{}, &contract.SSHError{Reason: contract.SSHReasonConnect}
	}
	if req.Cred == nil {
		return contract.SSHResult{}, &contract.SSHError{Reason: contract.SSHReasonAuth}
	}
	if _, err := ssh.ParsePrivateKey(req.Cred.Reveal()); err != nil {
		return contract.SSHResult{}, &contract.SSHError{Reason: contract.SSHReasonKeyUnparsable}
	}
	if req.PinnedFP != "" && req.PinnedFP != s.FP {
		return contract.SSHResult{SeenFP: s.FP}, &contract.SSHHostKeyError{Pinned: req.PinnedFP, Seen: s.FP}
	}
	if reject {
		return contract.SSHResult{SeenFP: s.FP}, &contract.SSHError{Reason: contract.SSHReasonAuth}
	}
	rec := execRec{User: req.Target.User}
	if len(req.Script) > 0 {
		// コマンドは SSHRequest.Sudo で決まる(呼び出し側が決める。User からは決めない)。
		rec.Cmd = "sh -s"
		if req.Sudo {
			rec.Cmd = "sudo -n sh -s"
		}
		rec.Stdin = string(req.Script)
	} else {
		rec.Cmd = strings.Join(req.Argv, " ")
		if req.Stdin != nil {
			in, _ := io.ReadAll(req.Stdin)
			rec.Stdin = string(in)
		}
	}
	s.mu.Lock()
	s.execs = append(s.execs, rec)
	s.mu.Unlock()
	out, code := "", 0
	if s.Reply != nil {
		out, code = s.Reply(rec)
	}
	if req.Stdout != nil {
		_, _ = io.WriteString(req.Stdout, out)
	}
	return contract.SSHResult{ExitStatus: code, SeenFP: s.FP}, nil
}

// newAdminKey はパスフレーズの無い OpenSSH 形式の秘密鍵の文字列を返す(spec 3.3。--admin-ref の値)。
func newAdminKey(t *testing.T) string {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	blk, err := ssh.MarshalPrivateKey(k, "")
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(blk))
}

const testRunID = "r-20261018T173000Z-3f9a"

// sourceCall は --host と --admin-ref(値は Secrets の "admin")を持つ呼び出し。
func sourceCall(words []string, host, key string) contract.Call {
	return contract.Call{
		Words:   words,
		Args:    map[string][]string{"host": {host}, "admin-ref": {"admin"}},
		Secrets: contract.SecretInput{"admin": json.RawMessage(strconv.Quote(key))},
		RunID:   testRunID,
	}
}

// srcState は fake.StateFiles に、seal の前に定義を読む候補(ReadCandidate)を足したもの。
type srcState struct {
	*fake.StateFiles
	cand []byte
}

func (s *srcState) ReadCandidate(context.Context) ([]byte, string, bool, error) {
	return s.cand, "", s.cand != nil, nil
}

// testDef は 127.0.0.1 の元サーバーとファイルの対象 t1 を持つ正規化済みの定義(試験に要る部分だけ)。
const testDef = `{"version":1,"source":{"host":"127.0.0.1","targets":[{"id":"t1","files":"/srv/app"}]}}`

// state の項目。
func sealedItem(sealed bool) json.RawMessage { return json.RawMessage(strconv.FormatBool(sealed)) }

func hostKeysItem(host, fp string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`[{"role":"source","host":%q,"host_key_fp":%q,"session_id":null}]`, host, fp))
}

func seenItem(pairs ...string) json.RawMessage {
	var l []map[string]any
	for i := 0; i+1 < len(pairs); i += 2 {
		l = append(l, map[string]any{"host": pairs[i], "host_key_fp": pairs[i+1], "at": "2026-11-03T00:00:00Z"})
	}
	b, _ := json.Marshal(l)
	return b
}

// newSrcAcquire は定義と状態の項目を持つ状態ファイルの偽物で Acquire を組む(保管先などは shapeAcquire と同じ偽物)。
func newSrcAcquire(items map[string]json.RawMessage) (*Acquire, *fake.StateFiles) {
	st := &fake.StateFiles{Def: []byte(testDef), Items: items}
	a := shapeAcquire()
	a.State = &srcState{StateFiles: st, cand: []byte(testDef)}
	a.SSH = routeSSH{}
	return a, st
}

// pullKeySecret は sitate-pull の秘密ファイルの値(kind: ssh_key。private_key・public_key・fp。spec 4.5 の表)を pem から作る。
func pullKeySecret(t *testing.T, pemKey string) json.RawMessage {
	t.Helper()
	sg, err := ssh.ParsePrivateKey([]byte(pemKey))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(contract.SSHKeySecret{Kind: contract.SecretKindSSHKey, PrivateKey: pemKey,
		PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sg.PublicKey()))), Fp: ssh.FingerprintSHA256(sg.PublicKey())})
	return b
}
