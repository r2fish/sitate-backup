package keys

// 契約試験が持たない規則の試験(U1g): init の再実行・確定後の ALREADY_INITIALIZED、フレーズとマスター鍵をディスクの確定の前の場所に書かないこと、
// 確定の途中の失敗からの再開、複数の公開鍵の検証、キーリングの見出しの上限、マスター鍵ごとの ID の違い。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func initPhraseEnv() (*phraseEnv, contract.Call) {
	e := newPhraseEnv(false)
	e.k.KeyS, e.k.Codec = newKS(), &harness.RefCodec{}
	e.st.cand, e.st.candDef = true, candStorage
	b := binary.BigEndian.AppendUint32(nil, 11)
	b = binary.BigEndian.AppendUint32(append(b, "ssh-ed25519"...), 32)
	pub := base64.StdEncoding.EncodeToString(append(b, bytes.Repeat([]byte{0x07}, 32)...))
	return e, contract.Call{Words: []string{"init"}, SecretOut: true, Args: map[string][]string{"approval-pubkey": {pub}}}
}

func phraseOf(t *testing.T, r contract.Reply) string {
	t.Helper()
	var sr struct {
		Words []string `json:"words"`
	}
	if err := json.Unmarshal(r.Phrase.Reveal(), &sr); err != nil || len(sr.Words) != 24 {
		t.Fatalf("秘密のレコードを読めない: %v", err)
	}
	return strings.Join(sr.Words, " ")
}

// 確定の前の init の再実行は保留を捨てて作り直し(別の repo_id)、確定の後の init は ALREADY_INITIALIZED で何も変えない。
func TestKeysInitRerunAndAlreadyInitialized(t *testing.T) {
	e, call := initPhraseEnv()
	useBundle(e, layout())
	r1, err := e.k.Init(ctx, call)
	harness.Shape(t, []string{"init"}, r1, err)
	r2, err := e.k.Init(ctx, call)
	harness.Shape(t, []string{"init"}, r2, err)
	if jsonField(t, r1.Result, "repo_id") == jsonField(t, r2.Result, "repo_id") {
		t.Error("確定前の再実行で保留を作り直していない")
	}
	if _, err := e.k.PhraseVerify(ctx, phraseCall(phraseOf(t, r2))); err != nil {
		t.Fatalf("確定: %v", err)
	}
	before, _ := os.ReadFile(filepath.Join(e.dir, contract.KeysMasterFile))
	_, err = e.k.Init(ctx, call)
	harness.ExpectCode(t, err, contract.CodeAlreadyInitialized)
	if after, _ := os.ReadFile(filepath.Join(e.dir, contract.KeysMasterFile)); !bytes.Equal(before, after) {
		t.Error("ALREADY_INITIALIZED でマスター鍵が変わった")
	}
}

// repo.json を書いた後(確定の途中)の init の再実行は保留を捨てず ALREADY_INITIALIZED。
func TestKeysInitAfterRepoJSONWritten(t *testing.T) {
	e, call := initPhraseEnv()
	r, err := e.k.Init(ctx, call)
	harness.Shape(t, []string{"init"}, r, err)
	e.st.Items["objects"] = json.RawMessage(`[{"key":"repo.json","version_id":"v1","size":1,"etag":"e","retain_until":"2027-03-01T00:00:00Z","written_at":"2027-03-01T00:00:00Z"}]`)
	_, err = e.k.Init(ctx, call)
	harness.ExpectCode(t, err, contract.CodeAlreadyInitialized)
	if _, ok := readState(e.dir, filepath.Join(contract.KeysPendingDir, contract.KeysPendingKeyring)); !ok {
		t.Error("確定の途中の init の再実行が保留を捨てた")
	}
}

// init はフレーズを残さず、確定の前に保管先へ書かない。マスター鍵の平文は keys/pending/master.json だけに置く。
func TestKeysInitWritesNothingOutsidePending(t *testing.T) {
	e, call := initPhraseEnv()
	r, err := e.k.Init(ctx, call)
	harness.Shape(t, []string{"init"}, r, err)
	words := strings.Fields(phraseOf(t, r))
	var master struct {
		Keys []struct {
			Key string `json:"key"`
		} `json:"keys"`
	}
	b, _ := os.ReadFile(filepath.Join(e.dir, contract.KeysPendingDir, contract.KeysPendingMaster))
	if json.Unmarshal(b, &master) != nil || len(master.Keys) != 1 {
		t.Fatalf("保留のマスター鍵を読めない: %s", b)
	}
	mk := master.Keys[0].Key
	_ = filepath.WalkDir(e.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(e.dir, p)
		if bytes.Contains(data, []byte(words[0]+" "+words[1])) || bytes.Contains(data, []byte(`"`+words[0]+`","`+words[1]+`"`)) {
			t.Errorf("フレーズがディスクに書かれている: %s", rel)
		}
		if bytes.Contains(data, []byte(mk)) && rel != filepath.Join(contract.KeysPendingDir, contract.KeysPendingMaster) {
			t.Errorf("マスター鍵が保留の master.json 以外にある: %s", rel)
		}
		if info, _ := d.Info(); info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s が所有者以外に読める: %v", rel, info.Mode().Perm())
		}
		return nil
	})
	for _, item := range e.st.Items {
		if bytes.Contains(item, []byte(mk)) || bytes.Contains(item, []byte(words[0]+" "+words[1])) {
			t.Error("状態ファイルの項目にマスター鍵かフレーズがある")
		}
	}
	if len(e.istore.Puts) != 0 {
		t.Errorf("確定の前に保管先へ書いた: %v", e.istore.Puts)
	}
	if strings.Contains(string(harness.Envelope([]string{"init"}, r)), mk) {
		t.Error("封筒にマスター鍵が出た")
	}
}

// failOnce は n 回目の Put を 1 度だけ失敗させる保管先。
type failOnce struct {
	contract.ObjectStore
	n, calls int
	puts     map[string]int
}

func (f *failOnce) Put(c context.Context, key string, body io.Reader, class contract.RetentionClass) (contract.PutResult, error) {
	f.calls++
	if f.calls == f.n {
		return contract.PutResult{}, errors.New("simulated")
	}
	f.puts[key]++
	return f.ObjectStore.Put(c, key, body, class)
}

// 確定の途中で書き込みが失敗したら保留を残して失敗を返し、同じフレーズでの再実行は記録した物を書き直さず残りを書いて確定する。
func TestKeysPhraseVerifyResumesAfterPartialWrite(t *testing.T) {
	e := newPhraseEnv(true)
	useBundle(e, layout())
	f := &failOnce{ObjectStore: e.istore, n: 4, puts: map[string]int{}}
	e.k.Store = f
	_, err := e.k.PhraseVerify(ctx, phraseCall(phraseTestPhrase))
	if err == nil {
		t.Fatal("書き込みの失敗で成功を返した")
	}
	if !pendingKept(e) || string(e.st.Items["phrase_confirmed"]) == "true" {
		t.Fatal("途中の失敗で確定した、または保留を消した")
	}
	r, err := e.k.PhraseVerify(ctx, phraseCall(phraseTestPhrase))
	harness.Shape(t, []string{"phrase", "verify"}, r, err)
	if jsonField(t, r.Result, "committed") != true || !committed(t, e) {
		t.Errorf("再実行で確定していない: %s", r.Result)
	}
	for key, n := range f.puts {
		if n != 1 {
			t.Errorf("%s を %d 回書いた(記録した物は書き直さない)", key, n)
		}
	}
}

// 信頼の起点が 2 つのとき、署名ファイルの鍵 ID と一致する鍵で検証し、一致する鍵が無ければ使わない。
func TestVerifierMultipleAnchors(t *testing.T) {
	k1 := harness.NewMinisignKey(bytes.Repeat([]byte{0x51}, 32))
	k2 := harness.NewMinisignKey(bytes.Repeat([]byte{0x52}, 32))
	k3 := harness.NewMinisignKey(bytes.Repeat([]byte{0x53}, 32))
	v := &Verifier{Anchors: []string{k1.Public(), k2.Public()}}
	tgt := []byte("core")
	w := contract.TrustedComment{Version: "1.0.0", Arch: "linux-x86_64", Path: "linux-x86_64/sitate"}
	tc := "version=1.0.0 arch=linux-x86_64 path=linux-x86_64/sitate"
	for name, c := range map[string]struct {
		sig  []byte
		want bool
	}{"1 本目": {k1.Sign(tgt, tc), true}, "2 本目": {k2.Sign(tgt, tc), true}, "起点に無い": {k3.Sign(tgt, tc), false}} {
		if ok, err := v.Verify(ctx, tgt, c.sig, w); err != nil || ok != c.want {
			t.Errorf("%s: %v %v", name, ok, err)
		}
	}
	// 署名の中身を壊す(trusted comment の全体の署名の欠け)と通らない。
	sig := k1.Sign(tgt, tc)
	bad := bytes.Replace(sig, []byte("trusted comment: version=1.0.0"), []byte("trusted comment: version=1.0.1"), 1)
	if ok, _ := v.Verify(ctx, tgt, bad, contract.TrustedComment{Version: "1.0.1", Arch: w.Arch, Path: w.Path}); ok {
		t.Error("trusted comment を書き換えた署名を通した")
	}
	if ok, _ := (&Verifier{Anchors: []string{}}).Verify(ctx, tgt, sig, w); ok {
		t.Error("信頼の起点が空なのに通した")
	}
}

// ID 鍵はマスター鍵から導くので、別のマスター鍵ではチャンク ID・ref・指紋が変わる。ref は f: と 32 桁の 16 進。
func TestKeyServiceIDsDependOnMaster(t *testing.T) {
	a, b := newKS(), newKS()
	mj, _ := json.Marshal(map[string]any{"current": "k1", "keys": []map[string]string{{"key_id": "k1", "key": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x32}, contract.KeySize))}}})
	if err := os.WriteFile(filepath.Join(b.Dir, contract.KeysMasterFile), mj, 0o600); err != nil {
		t.Fatal(err)
	}
	if a.ChunkID([]byte("x")) == b.ChunkID([]byte("x")) || a.EntryRef("p") == b.EntryRef("p") || a.CandidateFingerprint("mount", "/d") == b.CandidateFingerprint("mount", "/d") {
		t.Error("別のマスター鍵で同じ ID になった")
	}
	if r := a.EntryRef("p"); len(r) != 34 || !strings.HasPrefix(r, "f:") {
		t.Errorf("ref の形: %q", r)
	}
}

// 承認鍵は ssh-ed25519 の blob だけを受ける: base64 でない・短い・別の種類・鍵の長さが違うものは ARG_INVALID(保留も状態も作らない)。
func TestKeysInitRejectsBadApprovalKey(t *testing.T) {
	blob := func(typ string, keyLen int) string {
		b := binary.BigEndian.AppendUint32(nil, uint32(len(typ)))
		b = append(b, typ...)
		b = binary.BigEndian.AppendUint32(b, uint32(keyLen))
		return base64.StdEncoding.EncodeToString(append(b, make([]byte, keyLen)...))
	}
	good := blob("ssh-ed25519", 32)
	for name, v := range map[string]string{"base64 でない": "!!!", "生の 32 バイト": base64.StdEncoding.EncodeToString(make([]byte, 32)), "別の種類": blob("ssh-rsa", 32),
		"鍵の長さが違う": blob("ssh-ed25519", 31), "末尾に余り": good + "AAAA", "空": ""} {
		e, call := initPhraseEnv()
		call.Args["approval-pubkey"] = []string{v}
		_, err := e.k.Init(ctx, call)
		harness.ExpectCode(t, err, contract.CodeArgInvalid)
		if _, ok := readState(e.dir, filepath.Join(contract.KeysPendingDir, contract.KeysPendingMeta)); ok || len(e.st.Items) != 0 {
			t.Errorf("%s: 拒否したのに保留か状態を作った", name)
		}
	}
	e, call := initPhraseEnv()
	call.Args["approval-pubkey"] = []string{good}
	if r, err := e.k.Init(ctx, call); err != nil {
		t.Errorf("正しい鍵を拒否した: %v %v", err, r)
	}
}

// 結線の漏れ(Secrets・OSProbe が nil)は INTERNAL で止まり、合格に見えない。
func TestKeysNilWiringIsInternal(t *testing.T) {
	e := newEnv()
	e.k.Secrets = nil
	harness.NilDeps(t, "keys", func() error { _, err := e.k.BucketCheck(ctx, bucketCall()); return err })
	harness.NilDeps(t, "keys", func() error { _, err := e.k.InitStorage(ctx, initCall()); return err })
	e = newEnv()
	e.k.OS = nil
	harness.NilDeps(t, "keys", func() error { _, err := e.k.Doctor(ctx, contract.Call{Words: []string{"doctor"}}); return err })
}

// アダプタの終了 1 の error.orphaned_key_ids[](Error.Adapter)は IssueKeys の orphaned で返り、
// Keys は写す先のコードを問わず台帳に orphaned: true で記録する。
func TestKeysOrphanedKeyIDsFromAdapterError(t *testing.T) {
	r := fake.NewAdapterRunner()
	r.Script("provision", fake.Step{Outcome: fake.OutcomeError, ErrCode: "quota",
		ErrAdapter: &contract.AdapterError{Code: "quota", Detail: json.RawMessage(`{}`), OrphanedKeyIDs: []string{"K5"}}})
	_, orph, err := provisionKS(r).IssueKeys(ctx, issueReq("write", "read"))
	if err == nil || len(orph) != 1 || orph[0] != "K5" {
		t.Fatalf("orphaned を運んでいない: %v %v", orph, err)
	}
	e := initEnv(`{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[]}`)
	e.ks.FailRole, e.ks.Orphans = "write", []string{"K6"}
	if _, err := e.k.InitStorage(ctx, initCall()); err == nil {
		t.Fatal("成功した")
	}
	found := false
	for _, l := range e.st.Ledger {
		found = found || (jsonField(t, l, "key_id") == "K6" && jsonField(t, l, "orphaned") == true)
	}
	if !found {
		t.Errorf("台帳に K6 が無い: %s", e.st.Ledger)
	}
}

// 候補の定義が無ければ init は DEF_INVALID で、保留も状態も作らない(spec 3.1.2.1)。
func TestKeysInitNeedsCandidateDefinition(t *testing.T) {
	e, call := initPhraseEnv()
	e.st.cand = false
	_, err := e.k.Init(ctx, call)
	harness.ExpectCode(t, err, contract.CodeDefInvalid)
	if _, ok := readState(e.dir, filepath.Join(contract.KeysPendingDir, contract.KeysPendingMeta)); ok || len(e.st.Items) != 0 {
		t.Error("候補が無いのに保留か状態を作った")
	}
}
