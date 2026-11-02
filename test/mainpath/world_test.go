//go:build mainpath

package mainpath

// 実物の部品を組み、偽物(Secrets・StateFiles・Recorder・RetentionParams)と小さな実装(Clock・SSH)で結ぶ。

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/r2fish/sitate-backup/internal/acquire"
	"github.com/r2fish/sitate-backup/internal/adapter"
	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/format"
	"github.com/r2fish/sitate-backup/internal/keys"
	"github.com/r2fish/sitate-backup/internal/restore"
	"github.com/r2fish/sitate-backup/internal/store"
)

type realClock struct{}

func (realClock) Now(context.Context) (time.Time, error) { return time.Now().UTC(), nil }

// oneDay は試験用の保持(日次 1 日。MinIO の最小。月次は定数のまま)。固定の値。
type oneDay struct{}

func (oneDay) DailyDays() int     { return 1 }
func (oneDay) MonthlyMonths() int { return contract.RetentionMonthlyMonths }

type envS3 struct{ ep, bucket, wk, ws, rk, rs string }

func s3env(t *testing.T) envS3 {
	t.Helper()
	e := envS3{os.Getenv(contract.EnvTestS3Endpoint), os.Getenv(contract.EnvTestS3Bucket), os.Getenv(contract.EnvTestS3WriteAccessKey),
		os.Getenv(contract.EnvTestS3WriteSecretKey), os.Getenv(contract.EnvTestS3ReadAccessKey), os.Getenv(contract.EnvTestS3ReadSecretKey)}
	if e.ep == "" || e.wk == "" || e.rk == "" {
		t.Skip("試験用の保管先の環境変数が無い(make test-env-up NAME=mainpath の後に NAME=mainpath を付けて実行)")
	}
	if os.Getenv(contract.EnvTestS3Kind) == "fake" {
		t.Skip("MinIO に対する通し(KIND=fake では飛ばす)")
	}
	return e
}

type world struct {
	t      *testing.T
	ctx    context.Context
	s3     envS3
	prefix string
	dir    string // 状態ディレクトリ
	src    string // 元データの根
	sg     *signer

	codec *format.Codec
	crypt *format.Crypto
	keyS  *keys.KeyService
	st    *fake.StateFiles
	sec   *fake.SecretStore
	rec   *fake.Recorder
	put   *store.ObjectStore // 書き込み鍵
	idx   *acquire.Index
	ret   *store.Retention
	acq   *acquire.Acquire
	rst   *restore.Restore
	ssh   *localSSH
	keysC *keys.Keys

	def      []byte
	defHash  string
	repoID   string
	phrase   string
	lastMID  string // 直近の世代
	binVer   string
	storeFor func(addr contract.StoreAddr, keyID, secret string) contract.ObjectStore
}

func (w *world) setItem(name string, v any) {
	w.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		w.t.Fatal(err)
	}
	_ = w.st.Update(w.ctx, nil, name, b)
}

func must[T any](t *testing.T, v T, err error) T {
	t.Helper()
	if err != nil {
		t.Fatalf("%v", err)
	}
	return v
}

// newWorld は Keys.Init → PhraseVerify まで通した世界を返す。
func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, ctx: context.Background(), s3: s3env(t), binVer: harnessVersion}
	w.prefix = fmt.Sprintf("mp%d/", time.Now().UnixNano())
	root := t.TempDir()
	w.dir = filepath.Join(root, "state")
	w.src = filepath.Join(root, "source")
	for _, d := range []string{w.dir, w.src, filepath.Join(root, "adapters")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w.sg = newSigner(t)
	makeTree(t, w.src)
	w.codec, w.crypt = &format.Codec{}, &format.Crypto{}
	w.keyS = &keys.KeyService{Dir: w.dir, Codec: w.codec, Crypto: w.crypt}
	w.st, w.sec, w.rec = &fake.StateFiles{}, &fake.SecretStore{}, &fake.Recorder{}
	w.put = &store.ObjectStore{Endpoint: w.s3.ep, Bucket: w.s3.bucket, AccessKey: w.s3.wk, SecretKey: w.s3.ws, Prefix: w.prefix, Params: oneDay{}}
	w.storeFor = func(a contract.StoreAddr, keyID, secret string) contract.ObjectStore {
		return &store.ObjectStore{Endpoint: a.Endpoint, Bucket: a.Bucket, AccessKey: keyID, SecretKey: secret, Prefix: a.Prefix, Params: oneDay{}}
	}
	w.setupDefinition()
	w.setupKeys()
	w.setupAcquire(root)
	w.ret = &store.Retention{Store: w.put, State: w.st, Codec: w.codec, KeyS: w.keyS, Clock: realClock{}, Index: w.idx, Params: oneDay{}, BinVersion: w.binVer}
	w.rst = &restore.Restore{Codec: w.codec, KeyS: w.keyS, Crypto: w.crypt, Clock: realClock{}, State: w.st, Recorder: w.rec,
		OpenStore: func(_ context.Context, a contract.StoreAddr, keyID string, sec *contract.Secret) (contract.ObjectStore, error) {
			return w.storeFor(a, keyID, string(sec.Reveal())), nil
		}}
	return w
}

func (w *world) setupDefinition() {
	d := map[string]any{"version": 1,
		"source":  map[string]any{"host": "src.example.test", "targets": []any{map[string]any{"id": "t1", "files": w.src}}},
		"storage": []any{map[string]any{"endpoint": w.s3.ep, "bucket": w.s3.bucket, "key_ref": "sitate-storage", "prefix": w.prefix}},
		"restore": map[string]any{"dest": map[string]any{"at": "/srv/restore-target", "key_ref": "sitate-restore"}},
		"notify":  map[string]any{"to": []string{"owner@example.test"}}}
	b, _ := json.Marshal(d)
	if err := contract.Validate("data/definition.json#/$defs/definition", b); err != nil {
		w.t.Fatalf("ハーネスの定義が定義スキーマに合わない: %v", err)
	}
	w.def = b
	h, err := contract.CanonicalHash(b)
	if err != nil {
		w.t.Fatal(err)
	}
	w.defHash = h
	w.st.Def = b
	w.setItem("sealed", true)
}

// setupKeys は Keys.Init と PhraseVerify を実物に通す(署名つきの配布物は使い捨ての鍵で作る)。
func (w *world) setupKeys() {
	t := w.t
	_, apriv, _ := ed25519.GenerateKey(rand.Reader)
	sp, _ := ssh.NewPublicKey(apriv.Public())
	pub := base64.StdEncoding.EncodeToString(sp.Marshal())
	w.keysC = &keys.Keys{Store: w.put, State: w.st, KeyS: w.keyS, Codec: w.codec, Clock: realClock{}, Secrets: w.sec, Dir: w.dir,
		Recorder: w.rec, Verifier: &keys.Verifier{Anchors: []string{w.sg.anchor()}}, Bundle: w.bundle()}
	r, err := w.keysC.Init(w.ctx, contract.Call{Words: []string{"init"}, SecretOut: true, Args: map[string][]string{"approval-pubkey": {pub}}})
	if err != nil {
		t.Fatalf("Keys.Init: %v", err)
	}
	var rec struct {
		Words json.RawMessage `json:"words"`
	}
	if json.Unmarshal(r.Phrase.Reveal(), &rec) != nil {
		t.Fatalf("フレーズのレコードを読めない")
	}
	var arr []string
	if json.Unmarshal(rec.Words, &arr) == nil {
		w.phrase = strings.Join(arr, " ")
	} else {
		_ = json.Unmarshal(rec.Words, &w.phrase)
	}
	var ir struct {
		RepoID string `json:"repo_id"`
	}
	_ = json.Unmarshal(r.Result, &ir)
	w.repoID = ir.RepoID
	if _, err := w.keysC.PhraseVerify(w.ctx, contract.Call{Words: []string{"phrase", "verify"}, PhraseIn: contract.NewSecret([]byte(w.phrase)), SecretOut: true}); err != nil {
		t.Fatalf("Keys.PhraseVerify: %v", err)
	}
	// 保管先の鍵(書き込み・読み取り)を Secrets に置く(定義の key_ref)。
	b, _ := json.Marshal(map[string]string{"key_id": w.s3.wk, "secret": w.s3.ws})
	_ = w.sec.Put(w.ctx, "sitate-storage", b)
}

// bundle は PhraseVerify が bin/ へ写す署名つきの配布物(中身は意味のないバイト列)。
func (w *world) bundle() *fake.Bundle {
	ver := w.binVer
	var files []contract.BundleFile
	add := func(arch, p string, data []byte, signed bool) {
		f := contract.BundleFile{Path: p, Data: data}
		if signed {
			f.Sig = w.sg.sign(data, "version="+ver+" arch="+arch+" path="+p)
		}
		files = append(files, f)
	}
	for _, arch := range []string{"linux-x86_64", "linux-arm64"} {
		add(arch, arch+"/sitate", []byte("core-"+arch), true)
		exe := []byte("provision-" + arch)
		sum := fmt.Sprintf("%x", sha256Sum(exe))
		decl, _ := json.Marshal(map[string]any{"kind": "provision", "name": "b2", "version": ver, "contract": contract.AdapterContract["provision"], "exe": "b2", "secrets": []string{}, "network": true, "sha256": sum})
		add(arch, arch+"/adapters/provision/b2/adapter.json", decl, true)
		add(arch, arch+"/adapters/provision/b2/b2", exe, false)
	}
	add("any", "sitate-"+ver+"-src.tar.gz", []byte("src"), true)
	add("any", "sitate-"+ver+".spdx.json", []byte("{}"), true)
	return &fake.Bundle{Version: ver, FilesBy: map[string][]contract.BundleFile{"": files}}
}

func (w *world) setupAcquire(root string) {
	t := w.t
	// 元サーバー側の pull 鍵と受け口(ハーネスの SSH)。
	pk, err := mkPullKey()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.sec.Put(w.ctx, contract.SourcePullKeyRef, pk)
	var sk contract.SSHKeySecret
	_ = json.Unmarshal(pk, &sk)
	authLine := `restrict,from="203.0.113.1",command="/usr/local/libexec/sitate-recv" ` + sk.PublicKey
	var hash string
	w.ssh, hash = newLocalSSH(t, filepath.Join(root, "adapters"), map[string]string{"t1": w.src}, authLine)
	w.setItem("receiver_hash", hash)
	w.setItem("host_keys", []map[string]any{{"role": "source", "host": "src.example.test", "host_key_fp": fakeSeenFP, "session_id": nil}})
	// アダプタ(署名つき)と実行器(実物の Verifier)。
	adRoot := filepath.Join(root, "adapters", "root")
	recvExe := installAdapter(t, adRoot, w.sg)
	runner := &adapter.Runner{Root: adRoot, Version: harnessVersion, Arch: archName(), Verifier: &keys.Verifier{Anchors: []string{w.sg.anchor()}}}
	w.idx = &acquire.Index{Dir: w.dir, Codec: w.codec, KeyS: w.keyS, Store: w.put}
	w.acq = &acquire.Acquire{Store: w.put, State: w.st, Index: w.idx, Codec: w.codec, KeyS: w.keyS, Adapters: runner, Clock: realClock{}, SSH: w.ssh, Secrets: w.sec,
		Dir: w.dir, WorkDir: filepath.Join(root, "work"), Container: &acquire.Container{RecvExe: recvExe, Home: os.Getenv("HOME"), RuntimeDir: os.Getenv("XDG_RUNTIME_DIR")},
		From: "203.0.113.1", Retention: oneDay{}}
}

func sha256Sum(b []byte) [32]byte { return sha256SumImpl(b) }

func mkPullKey() (json.RawMessage, error) {
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	blk, err := ssh.MarshalPrivateKey(k, "")
	if err != nil {
		return nil, err
	}
	pem := string(pemOf(blk))
	sg, err := ssh.ParsePrivateKey([]byte(pem))
	if err != nil {
		return nil, err
	}
	return json.Marshal(contract.SSHKeySecret{Kind: contract.SecretKindSSHKey, PrivateKey: pem,
		PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sg.PublicKey()))), Fp: ssh.FingerprintSHA256(sg.PublicKey())})
}
