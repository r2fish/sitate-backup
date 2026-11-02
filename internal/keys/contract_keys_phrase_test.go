package keys

// phrase verify の正常系の契約試験(spec 3.1.2 `phrase verify`、4.3 の「保留と確定」「確定の書く順序と途中の失敗」、4.2 の `bin/` の中身)。
// 判定する内容: フレーズは Call.PhraseIn で受ける / 確定の前は保管先へ書かない / 確定でキーリング・repo.json・bin/ を書き、書いた後に Commit する /
// bin/ は署名が合うものだけを同じ相対パスで書く / 版 ID を状態ファイルに記録する / 2 回目は書かない。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	bip39 "github.com/tyler-smith/go-bip39"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

const phraseTestPhrase = "legal winner thank year wave sausage worth useful legal winner thank year wave sausage worth useful legal winner thank year wave sausage worth title"

// wantVerifier は署名の検証の呼び出し(期待値)を記録し、Reject に含まれる Path は不合格にする。
type wantVerifier struct {
	mu     sync.Mutex
	Reject map[string]bool
	Seen   []contract.TrustedComment
}

func (v *wantVerifier) Verify(_ context.Context, _, _ []byte, w contract.TrustedComment) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.Seen = append(v.Seen, w)
	return !v.Reject[w.Path], nil
}

type phraseEnv struct {
	*kenv
	pendingRepo []byte
	keyring     []byte
	master      []byte
	ver         *wantVerifier
}

const phraseMasterKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=" // 32 バイトの合成のマスター鍵(base64)

// writePending は spec 4.5 の keys/pending/ の配置(keyring・repo.json・master.json・pending.json)を状態ディレクトリへ置く。
func writePending(dir, op string, keyring, repo, master []byte) {
	pd := filepath.Join(dir, contract.KeysPendingDir)
	if err := os.MkdirAll(pd, 0o700); err != nil {
		panic(err)
	}
	meta, _ := json.Marshal(map[string]any{"op": op, "change_id": nil})
	for n, b := range map[string][]byte{contract.KeysPendingKeyring: keyring, contract.KeysPendingRepo: repo, contract.KeysPendingMaster: master, contract.KeysPendingMeta: meta} {
		if err := os.WriteFile(filepath.Join(pd, n), b, 0o600); err != nil {
			panic(err)
		}
	}
}

func readState(dir, rel string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(dir, rel))
	return string(b), err == nil
}

// pendingKept は保留(keys/pending/)が残っていて、マスター鍵が確定の位置に無い(置き換えられていない)こと。
func pendingKept(e *phraseEnv) bool {
	_, hasPending := readState(e.dir, filepath.Join(contract.KeysPendingDir, contract.KeysPendingMeta))
	_, hasMaster := readState(e.dir, contract.KeysMasterFile)
	return hasPending && !hasMaster
}

// committed は確定の後の配置(spec 4.5): master.json は保留の master.json、keyring.latest・repo.latest は保管先へ書いたバイト列、保留は消えている。
func committed(t *testing.T, e *phraseEnv) bool {
	t.Helper()
	m, okM := readState(e.dir, contract.KeysMasterFile)
	kr, okK := readState(e.dir, contract.KeysKeyringLatest)
	rp, okR := readState(e.dir, contract.KeysRepoLatest)
	_, pend := readState(e.dir, filepath.Join(contract.KeysPendingDir, contract.KeysPendingMeta))
	return okM && okK && okR && !pend && m == string(e.master) && kr == string(e.keyring) && rp == string(e.pendingRepo)
}

func bundleFile(path, arch string, data string) contract.BundleFile {
	return contract.BundleFile{Path: path, Data: []byte(data), Sig: []byte("sig:" + data),
		Want: contract.TrustedComment{Version: "1.0.0", Arch: arch, Path: path}}
}

func provisionDecl(exe string) string {
	sum := sha256.Sum256([]byte(exe))
	aj, _ := json.Marshal(map[string]string{"kind": "provision", "name": "b2", "exe": "b2", "sha256": hex.EncodeToString(sum[:])})
	return string(aj)
}

// provisionFiles は保管先準備アダプタの署名付きの adapter.json と、署名の無い実行ファイル。
func provisionFiles(arch, exe string) []contract.BundleFile {
	return []contract.BundleFile{
		bundleFile(arch+"/adapters/provision/b2/adapter.json", arch, provisionDecl(exe)),
		{Path: arch + "/adapters/provision/b2/b2", Data: []byte(exe)},
	}
}

func newPhraseEnv(withPending bool) *phraseEnv {
	e := &phraseEnv{kenv: newEnv(), ver: &wantVerifier{Reject: map[string]bool{}}}
	e.keyring = []byte("KEYRING-BYTES")
	e.pendingRepo = []byte(`{"format_version":1,"repo_id":"` + testRepoID + `","keyring_id":"` + testKeyringID + `"}`)
	e.master = []byte(`{"current":"k1","keys":[{"key_id":"k1","key":"` + phraseMasterKey + `"}]}`)
	if withPending {
		writePending(e.dir, "init", e.keyring, e.pendingRepo, e.master)
		e.istore.expectPending = true
	}
	e.ks.Open, e.ks.Info = true, &contract.KeyringInfo{Version: 1, KeyringID: testKeyringID, RepoID: testRepoID, KeyIDs: []string{"k1", "k2"}}
	// 配布物の並びは tools/build.sh と tools/sign.sh のとおり(実行ファイルは署名せず、adapter.json の sha256 で照らす。spec 3.2.1、4.2)。
	a, b := "linux-x86_64", "linux-arm64"
	e.k.Bundle = &fake.Bundle{Version: "1.0.0", FilesBy: map[string][]contract.BundleFile{
		a: append(provisionFiles(a, "exe-x"), bundleFile(a+"/sitate", a, "core-x"), bundleFile(a+"/adapters/check/fsck/adapter.json", a, "check-x"),
			bundleFile("sitate-1.0.0-src.tar.gz", "any", "src"), bundleFile("sitate-1.0.0.spdx.json", "any", "sbom")),
		b: append(provisionFiles(b, "exe-a"), bundleFile(b+"/sitate", b, "core-a")),
	}}
	e.k.Verifier = e.ver
	e.st.Items = map[string]json.RawMessage{}
	return e
}

func phraseCall(phrase string) contract.Call {
	return contract.Call{Words: []string{"phrase", "verify"}, PhraseIn: contract.NewSecret([]byte(phrase)), SecretOut: true}
}

func storeBodies(t *testing.T, e *phraseEnv) map[string]string {
	t.Helper()
	vs, err := e.k.Store.ListVersions(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, v := range vs {
		rc, err := e.k.Store.Get(ctx, v.Key, v.VersionID)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		m[v.Key] = string(b)
	}
	return m
}

// keyEnds は prefix を問わず、末尾が suffix のキーを返す(<p> は保管先のプレフィクス)。
func keyEnds(m map[string]string, suffix string) (string, bool) {
	for k := range m {
		if strings.HasSuffix(k, suffix) {
			return k, true
		}
	}
	return "", false
}

func TestContractKeysPhraseVerify(t *testing.T) {
	if _, err := bip39.NewMnemonic(make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	e := newPhraseEnv(true)
	r, err := e.k.PhraseVerify(ctx, phraseCall(phraseTestPhrase))
	harness.Implemented(t, err)
	if err != nil {
		t.Fatalf("開けるフレーズで失敗: %v", err)
	}
	if v := jsonField(t, r.Result, "verified"); v != true {
		t.Errorf("verified = %v", v)
	}
	if v := jsonField(t, r.Result, "committed"); v != true {
		t.Errorf("committed = %v (保留を確定した)", v)
	}
	if n := jsonField(t, r.Result, "keyring_version"); n != float64(1) {
		t.Errorf("keyring_version = %v (init の直後の保留は 1)", n)
	}
	if r.Phrase != nil || strings.Contains(string(r.Result), "legal winner") {
		t.Errorf("応答にフレーズがある")
	}
	bodies := storeBodies(t, e)
	// 保管先に書いたもの: repo.json・キーリングは保留の中身そのもの。キーは `<p>` を含まない名前(`<p>` は store が付ける。contract/storage.go)で、
	// キーリングのキーは repo.json の keyring_id から作る(keyring/<keyring_id>)。
	if bodies["repo.json"] != string(e.pendingRepo) {
		t.Errorf("repo.json を保留の中身で書いていない: %v", bodies)
	}
	if bodies["keyring/"+testKeyringID] != string(e.keyring) {
		t.Errorf("キーリング(keyring/<keyring_id>)を保留の中身で書いていない: %v", bodies)
	}
	// bin/: 核とアダプタ(保管先準備)を両方の arch で、署名つきで、同じ相対パスに書く。check と価格表などは書かない。
	for rel, want := range map[string]string{"linux-x86_64/sitate": "core-x", "linux-arm64/sitate": "core-a",
		"linux-x86_64/adapters/provision/b2/adapter.json": provisionDecl("exe-x"), "linux-arm64/adapters/provision/b2/adapter.json": provisionDecl("exe-a"),
		"sitate-1.0.0-src.tar.gz": "src", "sitate-1.0.0.spdx.json": "sbom"} {
		if bodies["bin/1.0.0/"+rel] != want {
			t.Errorf("bin/1.0.0/%s を書いていない: %v", rel, bodies)
		}
		if bodies["bin/1.0.0/"+rel+".sig"] != "sig:"+want {
			t.Errorf("bin/1.0.0/%s.sig を書いていない", rel)
		}
	}
	// 実行ファイルは写すが署名を書かない(adapter.json の sha256 で照らす)。
	for rel, want := range map[string]string{"linux-x86_64/adapters/provision/b2/b2": "exe-x", "linux-arm64/adapters/provision/b2/b2": "exe-a"} {
		if bodies["bin/1.0.0/"+rel] != want {
			t.Errorf("bin/1.0.0/%s を書いていない", rel)
		}
		if _, ok := bodies["bin/1.0.0/"+rel+".sig"]; ok {
			t.Errorf("bin/1.0.0/%s.sig を書いた", rel)
		}
	}
	for k := range bodies {
		if strings.Contains(k, "adapters/check/") || strings.Contains(k, "prices") {
			t.Errorf("bin/ に入れないものを書いた: %s", k)
		}
	}
	phraseCheckSigs(t, e)
	phraseCheckCommit(t, e, r)
}

// phraseCheckCommit は確定(Commit)の時機と、状態ファイルへの記録、2 回目の振る舞いを確かめる。
func phraseCheckCommit(t *testing.T, e *phraseEnv, r contract.Reply) {
	t.Helper()
	// 確定は書き終えた後: 保管先への書き込みの間は保留(keys/pending/)が残り、書き終えてから master.json の置き換えと保留の削除を行う。
	if !committed(t, e) {
		t.Errorf("確定の後の keys/ の配置が spec 4.5 と違う(master.json=保留の master.json、keyring.latest、repo.latest、保留なし)")
	}
	if len(e.istore.PendingGone) != 0 || len(e.istore.Puts) < 4 {
		t.Errorf("保管先へ書く間に保留が消えた(確定が早い)、または書いていない: 消えた後の Put %v, Put %v", e.istore.PendingGone, e.istore.Puts)
	}
	if len(e.istore.NoIntent) != 0 {
		t.Errorf("書き込み予定(write_intent)を実行の記録に残す前に書いた: %v", e.istore.NoIntent)
	}
	if strings.Contains(string(r.Result), phraseMasterKey) {
		t.Error("結果にマスター鍵の値が出た")
	}
	if string(e.st.Items["phrase_confirmed"]) != "true" {
		t.Errorf("phrase_confirmed = %s", e.st.Items["phrase_confirmed"])
	}
	// 書いた版 ID を状態ファイルに記録する(objects[] と last_phrase_verify.keyring_version_id)。
	var recs []map[string]any
	if err := json.Unmarshal(e.st.Items["objects"], &recs); err != nil {
		t.Fatalf("objects を記録していない: %v", err)
	}
	vs, _ := e.k.Store.ListVersions(ctx, "")
	for _, v := range vs {
		found := false
		for _, rc := range recs {
			if rc["key"] == v.Key && rc["version_id"] == v.VersionID {
				found = true
			}
		}
		if !found {
			t.Errorf("書いた版を記録していない: %s %s", v.Key, v.VersionID)
		}
	}
	if id := jsonField(t, e.st.Items["last_phrase_verify"], "keyring_version_id"); id == nil || id != jsonField(t, r.Result, "keyring_version_id") {
		t.Errorf("last_phrase_verify.keyring_version_id = %v, 結果 = %v", id, jsonField(t, r.Result, "keyring_version_id"))
	}
	// last_phrase_verify の値(表の 1 の入力): keyring_hash は書いたバイト列の SHA-256、key_ids は開けたキーリングの鍵 ID。
	sum := sha256.Sum256(e.keyring)
	if h := jsonField(t, e.st.Items["last_phrase_verify"], "keyring_hash"); h != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Errorf("last_phrase_verify.keyring_hash = %v (書いたキーリングの SHA-256)", h)
	}
	if ids, _ := jsonField(t, e.st.Items["last_phrase_verify"], "key_ids").([]any); len(ids) != 2 || ids[0] != "k1" || ids[1] != "k2" {
		t.Errorf("last_phrase_verify.key_ids = %v (開けたキーリングの [k1 k2])", ids)
	}
	// 同じフレーズの 2 回目: 確定済みなので書かず、committed は偽、判定は同じ。
	before := len(vs)
	r2, err := e.k.PhraseVerify(ctx, phraseCall(phraseTestPhrase))
	if err != nil {
		t.Fatalf("2 回目: %v", err)
	}
	if jsonField(t, r2.Result, "verified") != true || jsonField(t, r2.Result, "committed") != false {
		t.Errorf("2 回目の結果 = %s", r2.Result)
	}
	if vs2, _ := e.k.Store.ListVersions(ctx, ""); len(vs2) != before {
		t.Errorf("確定は 1 回だけ: 版 %d -> %d", before, len(vs2))
	}
}

func TestContractKeysPhraseVerifyWrongKeepsPending(t *testing.T) {
	e := newPhraseEnv(true)
	e.ks.Open = false
	_, err := e.k.PhraseVerify(ctx, phraseCall(phraseTestPhrase))
	harness.Implemented(t, err)
	harness.ExpectCode(t, err, contract.CodePhraseInvalid)
	if !pendingKept(e) {
		t.Errorf("開けないフレーズは保留を捨てない・確定しない(正しいフレーズで再実行できる)")
	}
	if vs, _ := e.k.Store.ListVersions(ctx, ""); len(vs) != 0 {
		t.Errorf("確定の前に保管先へ書いた: %d", len(vs))
	}
}

func TestContractKeysPhraseVerifyBadSignature(t *testing.T) {
	e := newPhraseEnv(true)
	e.ver.Reject["linux-arm64/sitate"] = true
	_, err := e.k.PhraseVerify(ctx, phraseCall(phraseTestPhrase))
	harness.Implemented(t, err)
	var ce *contract.Error
	if err == nil || !errors.As(err, &ce) {
		t.Fatalf("署名が合わない配布物で成功した、または型付きの誤りでない: %v", err)
	}
	if bodies := storeBodies(t, e); func() bool { _, ok := keyEnds(bodies, "linux-arm64/sitate"); return ok }() {
		t.Errorf("署名が合わないファイルを bin/ に書いた")
	}
	if !pendingKept(e) || string(e.st.Items["phrase_confirmed"]) == "true" {
		t.Errorf("失敗では確定せず保留を残す")
	}
	// 同じフレーズでの再実行が確定する。
	delete(e.ver.Reject, "linux-arm64/sitate")
	r, err := e.k.PhraseVerify(ctx, phraseCall(phraseTestPhrase))
	if err != nil || jsonField(t, r.Result, "committed") != true || !committed(t, e) {
		t.Errorf("再実行で確定していない: %v %s", err, r.Result)
	}
}

// phraseCheckSigs は署名の検証が配布物の期待値(version・arch・path)で行われたことを確かめる。
func phraseCheckSigs(t *testing.T, e *phraseEnv) {
	t.Helper()
	// 署名の検証は配布物の期待値(version・arch・path)で行った。
	if len(e.ver.Seen) < 4 {
		t.Errorf("署名を検証していない: %v", e.ver.Seen)
	}
	for _, w := range e.ver.Seen {
		top := w.Arch == "any" && !strings.Contains(w.Path, "/") // 版ディレクトリ直下のソースと SBOM
		if w.Version != "1.0.0" || (!top && !strings.HasPrefix(w.Path, w.Arch+"/")) {
			t.Errorf("trusted comment の期待値が配布物の形でない: %+v", w)
		}
	}
}
