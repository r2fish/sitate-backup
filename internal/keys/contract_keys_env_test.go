package keys

// 入口(Keys の 5 つの入力)の契約試験の共通部品: 偽の保管先準備アダプタ(操作ごとに応答を返す)、記録つきの偽物、結果の読み出し。
// 偽物は internal/contract/fake を土台にし、他 WP のパッケージは import しない。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

// opRunner は偽の保管先準備アダプタ。要求の op(check は枠 admin / key_role)ごとに応答を返し、呼び出しを記録する。
type opRunner struct {
	mu    sync.Mutex
	Calls []map[string]any
	Resp  map[string]func() (json.RawMessage, error) // 鍵: create-bucket / issue-keys / revoke-keys / check:admin / check:write / check:read
}

func (o *opRunner) Declaration(_ context.Context, kind, name string) (contract.Declaration, error) {
	return contract.Declaration{Kind: kind, Name: name, Version: "0.0.0-fake", Raw: json.RawMessage(`{}`)}, nil
}

func (o *opRunner) Run(_ context.Context, _ contract.RunContext, kind, name string, req json.RawMessage, _ func(contract.Frame) error) (json.RawMessage, error) {
	var m map[string]any
	_ = json.Unmarshal(req, &m)
	op, _ := m["op"].(string)
	key := op
	if op == "check" {
		key = "check:admin"
		if r, _ := m["key_role"].(string); r != "" {
			key = "check:" + r
		}
	}
	m["_key"], m["_kind"] = key, kind
	o.mu.Lock()
	o.Calls = append(o.Calls, m)
	fn := o.Resp[key]
	o.mu.Unlock()
	if fn == nil || kind != "provision" {
		return nil, &contract.Error{Code: contract.CodeInternal, Class: "internal", Detail: json.RawMessage(`{"component":"adapter","reason":"script_exhausted","kind":null,"name":null,"http_status":null}`), Next: []contract.Next{}}
	}
	return fn()
}

// called は key の呼び出し(op と枠)の回数。
func (o *opRunner) called(key string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := 0
	for _, c := range o.Calls {
		if c["_key"] == key {
			n++
		}
	}
	return n
}

func ok(v string) func() (json.RawMessage, error) {
	return func() (json.RawMessage, error) { return json.RawMessage(v), nil }
}

func adapterFail(code string) func() (json.RawMessage, error) {
	return func() (json.RawMessage, error) {
		d, _ := json.Marshal(map[string]string{"kind": "provision", "name": "b2", "adapter_code": code})
		return nil, &contract.Error{Code: contract.CodeAdapterError, Class: contract.ExitClass(contract.CodeTable[contract.CodeAdapterError].Exit), Detail: d, Next: []contract.Next{}}
	}
}

// recKS は記録つきの偽の KeyService(発行の要求・失効した鍵 ID・キーリングを開けるか)。
type recKS struct {
	fake.KeyService
	mu      sync.Mutex
	Issued  []string
	Reqs    []contract.IssueRequest
	RevReqs []contract.RevokeRequest
	Revoked []string
	Open    bool
	// Info は OpenedKeyring の値(零値なら版 1・鍵 ID k1。Ver があれば版だけ上書き)。FailRole はその役割を含む発行を丸ごと失敗させ(全部か無しか)、Orphans を返す。
	// IssuedSecrets は発行した秘密(呼び出し側が Wipe したかを見る)。
	Info          *contract.KeyringInfo
	Ver           int
	FailRole      string
	Orphans       []string
	IssuedSecrets []*contract.Secret
}

// secretOf は試験用の合成の秘密(役割ごとに一意)。
func secretOf(role string) string { return "SYNTH-ISSUED-SECRET-" + role }

func (r *recKS) IssueKeys(_ context.Context, req contract.IssueRequest) ([]contract.IssuedKey, []string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Reqs = append(r.Reqs, req)
	var out []contract.IssuedKey
	for _, role := range req.Roles {
		if role == r.FailRole {
			_, err := adapterFail("quota")()
			return nil, r.Orphans, err
		}
		sec := contract.NewSecret([]byte(secretOf(role)))
		r.IssuedSecrets = append(r.IssuedSecrets, sec)
		r.Issued = append(r.Issued, role)
		out = append(out, contract.IssuedKey{Role: role, KeyID: "kid-" + role, Secret: sec})
	}
	return out, nil, nil
}

const testKeyringID = "kr-0123456789abcdef0123456789abcdef"

func (r *recKS) OpenedKeyring(ctx context.Context, p *contract.Secret, kr []byte) (contract.KeyringInfo, error) {
	if !r.Open {
		return contract.KeyringInfo{}, &contract.Error{Code: contract.CodePhraseInvalid, Class: contract.ExitClass(contract.CodeTable[contract.CodePhraseInvalid].Exit), Detail: json.RawMessage(`{}`), Next: []contract.Next{}}
	}
	info := contract.KeyringInfo{Version: 1, KeyringID: testKeyringID, RepoID: testRepoID, KeyIDs: []string{"k1"}}
	if r.Info != nil {
		info = *r.Info
	}
	if r.Ver != 0 {
		info.Version = r.Ver
	}
	return info, nil
}

func (r *recKS) RevokeKey(_ context.Context, req contract.RevokeRequest) ([]string, []string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RevReqs = append(r.RevReqs, req)
	r.Revoked = append(r.Revoked, req.KeyIDs...)
	return append([]string{}, req.KeyIDs...), []string{}, nil
}

func (r *recKS) OpenKeyring(context.Context, *contract.Secret, []byte) (bool, error) {
	return r.Open, nil
}

// recProbe は記録つきの偽の StoreProbe(PutProbe のキーと Try の組)。
type recProbe struct {
	fake.StoreProbe
	rec      *fake.Recorder
	NoIntent []string // write_intent を実行の記録に残す前に書いたキー
	mu       sync.Mutex
	Puts     []string
	Tried    []string
}

func (p *recProbe) PutProbe(ctx context.Context, key string, body []byte) (string, error) {
	p.mu.Lock()
	p.Puts = append(p.Puts, key)
	if !hasIntent(p.rec, key) {
		p.NoIntent = append(p.NoIntent, key)
	}
	p.mu.Unlock()
	return p.StoreProbe.PutProbe(ctx, key, body)
}

func (p *recProbe) Try(ctx context.Context, role, op string) (bool, error) {
	p.mu.Lock()
	p.Tried = append(p.Tried, role+"/"+op)
	p.mu.Unlock()
	return p.StoreProbe.Try(ctx, role, op)
}

// stateT は偽の状態ファイルに、候補の有無(定義の段階)を足したもの。
type stateT struct {
	*fake.StateFiles
	cand    bool
	candDef string // 候補の定義(空なら candStorage)
}

// candStorage は storage[0] を持つ候補の定義(key_ref は store-main)。
const candStorage = `{"version":1,"storage":[{"endpoint":"https://s3.example.invalid","bucket":"bkt-1","prefix":"sitate/","key_ref":"store-main"}]}`

func (s *stateT) ReadCandidate(context.Context) ([]byte, string, bool, error) {
	if s.cand {
		d := s.candDef
		if d == "" {
			d = `{}`
		}
		return []byte(d), "h-cand", true, nil
	}
	return nil, "", false, nil
}

type kenv struct {
	k   *Keys
	rec *fake.Recorder
	st  *stateT
	ks  *recKS
	run *opRunner
	pr  *recProbe
	dir string
	// istore は Put の前に write_intent(書き込み予定)が実行の記録にあるか、keys/pending が残っているかを見る保管先。
	istore *intentStore
}

func newEnv() *kenv {
	store, _, _ := fake.NewTestStore(fake.RoleWrite)
	e := &kenv{st: &stateT{StateFiles: &fake.StateFiles{}}, ks: &recKS{}, run: &opRunner{Resp: map[string]func() (json.RawMessage, error){}}, pr: &recProbe{}, rec: &fake.Recorder{}, dir: nextDir()}
	e.pr.rec = e.rec
	e.istore = &intentStore{ObjectStore: store, rec: e.rec, dir: e.dir}
	e.k = &Keys{Store: e.istore, Probe: e.pr, State: e.st, KeyS: e.ks, Provision: e.run, Dir: e.dir, Recorder: e.rec, Verifier: &fake.Verifier{Valid: true}, Bundle: &fake.Bundle{}, Clock: &fake.Clock{T: time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)}}
	e.k.Secrets, e.k.OS = issuedSecrets{}, &fake.OSProbe{} // 結線の漏れは INTERNAL で止まるので、全部を差す
	return e
}

func findWarn(r contract.Reply, code contract.Code) *contract.Warning {
	for i := range r.Warnings {
		if r.Warnings[i].Code == code {
			return &r.Warnings[i]
		}
	}
	return nil
}

func jsonField(t *testing.T, raw json.RawMessage, path ...string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("JSON を読めない: %v: %s", err, raw)
	}
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	return v
}

const testRepoID = "rp-0123456789abcdef0123456789abcdef"

var testRoot string

func TestMain(m *testing.M) {
	d, err := os.MkdirTemp("", "keys-contract-")
	if err != nil {
		panic(err)
	}
	testRoot = d
	code := m.Run()
	_ = os.RemoveAll(d)
	os.Exit(code)
}

var dirSeq atomic.Int32

// nextDir は試験ごとの状態ディレクトリ(keys/ を置く)。
func nextDir() string {
	d := filepath.Join(testRoot, fmt.Sprintf("s%d", dirSeq.Add(1)))
	if err := os.MkdirAll(d, 0o700); err != nil {
		panic(err)
	}
	return d
}

// intentStore は Put のたびに、(1) そのキーを含む write_intent が実行の記録に先にあること、(2) keys/pending が残っていること(確定が書き終えた後であること)を記録する。
type intentStore struct {
	contract.ObjectStore
	rec           *fake.Recorder
	dir           string
	Puts          []string
	NoIntent      []string
	PendingGone   []string
	expectPending bool // 保留がある世界(phrase verify)では、Put の時点で保留が残っていなければならない
}

func (s *intentStore) Put(c context.Context, key string, body io.Reader, class contract.RetentionClass) (contract.PutResult, error) {
	s.Puts = append(s.Puts, key)
	found := hasIntent(s.rec, key)
	if !found {
		s.NoIntent = append(s.NoIntent, key)
	}
	if _, err := os.Stat(filepath.Join(s.dir, contract.KeysPendingDir)); err != nil && s.expectPending {
		s.PendingGone = append(s.PendingGone, key)
	}
	return s.ObjectStore.Put(c, key, body, class)
}

// hasIntent は実行の記録に、key を含む write_intent(書き込み予定。spec 4.5 の journal/)があるか。
func hasIntent(rec *fake.Recorder, key string) bool {
	for _, e := range rec.Entries {
		var h struct {
			Kind string `json:"kind"`
			Body struct {
				Keys []string `json:"keys"`
			} `json:"body"`
		}
		if json.Unmarshal(e, &h) == nil && h.Kind == "write_intent" && slices.Contains(h.Body.Keys, key) {
			return true
		}
	}
	return false
}
