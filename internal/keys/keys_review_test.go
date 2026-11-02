package keys

// レビュー(F1〜F14、T1)で足した試験: 実物の Codec と同じ厳しさの包み、実際の配布物の並び(tools/build.sh・sign.sh の出力)での確定、
// kit の成功、設定の食い違い、台帳の並び、確定の順序、孤児、署名の期待値、鍵 ID の束縛。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// strictCodec は実物の Codec と同じく h.Kind != kind を拒否する包み。
type strictCodec struct{ contract.Codec }

func (s strictCodec) Encode(kind, id string, h contract.Header, content, key []byte) ([]byte, error) {
	if h.Kind != kind {
		return nil, errors.New("strict: 見出しの種類が違う")
	}
	return s.Codec.Encode(kind, id, h, content, key)
}

type failCodec struct{ contract.Codec }

func (failCodec) Encode(string, string, contract.Header, []byte, []byte) ([]byte, error) {
	return nil, errors.New("encode failed")
}

func TestKeysInitWithStrictCodec(t *testing.T) {
	e, call := initPhraseEnv()
	e.k.Codec = strictCodec{&harness.RefCodec{}}
	r, err := e.k.Init(ctx, call)
	harness.Shape(t, []string{"init"}, r, err)
	e, call = initPhraseEnv()
	e.k.Codec = failCodec{&harness.RefCodec{}}
	_, err = e.k.Init(ctx, call)
	harness.ExpectCode(t, err, contract.CodeInternal) // 素の誤りをそのまま返さない
}

// --- 配布物(tools/build.sh・sign.sh の並び) ---

const bundleVer = "1.0.0"

var bundleKey = harness.NewMinisignKey(bytes.Repeat([]byte{0x61}, 32))

type bfile struct {
	data   []byte
	arch   string
	signed bool
}

// layout は実際の配布物の並び: 両 arch の核、提供アダプタ(adapter.json は署名つき、実行ファイルは署名なし)、版ディレクトリ直下のソースと SBOM。
func layout() map[string]bfile {
	m := map[string]bfile{}
	for _, a := range distArches {
		exe := []byte("exe-" + a)
		sum := sha256.Sum256(exe)
		aj, _ := json.Marshal(map[string]string{"kind": "provision", "name": "b2", "exe": "b2", "sha256": hex.EncodeToString(sum[:])})
		m[a+"/sitate"] = bfile{[]byte("core-" + a), a, true}
		m[a+"/adapters/provision/b2/adapter.json"] = bfile{aj, a, true}
		m[a+"/adapters/provision/b2/b2"] = bfile{exe, a, false}
		m[a+"/adapters/check/fsck/adapter.json"] = bfile{[]byte("{}"), a, true}
	}
	m["sitate-"+bundleVer+"-src.tar.gz"] = bfile{[]byte("src"), "any", true}
	m["sitate-"+bundleVer+".spdx.json"] = bfile{[]byte("{}"), "any", true}
	return m
}

func (b bfile) sign(path string) contract.BundleFile {
	f := contract.BundleFile{Path: path, Data: b.data, Want: contract.TrustedComment{Version: "x", Arch: "x", Path: "x"}} // Want は信じない
	if b.signed {
		f.Sig = bundleKey.Sign(b.data, "version="+bundleVer+" arch="+b.arch+" path="+path)
	}
	return f
}

func useBundle(e *phraseEnv, m map[string]bfile) {
	var fs []contract.BundleFile
	for p, b := range m {
		fs = append(fs, b.sign(p))
	}
	e.k.Bundle = &fake.Bundle{Version: bundleVer, FilesBy: map[string][]contract.BundleFile{"all": fs}}
	e.k.Verifier = &Verifier{Anchors: []string{bundleKey.Public()}}
}

func TestKeysPhraseVerifyRealLayout(t *testing.T) {
	e := newPhraseEnv(true)
	useBundle(e, layout())
	r, err := e.k.PhraseVerify(ctx, phraseCall(phraseTestPhrase))
	harness.Shape(t, []string{"phrase", "verify"}, r, err)
	bodies := storeBodies(t, e)
	exe := "bin/" + bundleVer + "/linux-x86_64/adapters/provision/b2/b2"
	if bodies[exe] != "exe-linux-x86_64" {
		t.Errorf("実行ファイルを書いていない: %v", bodies)
	}
	if _, ok := bodies[exe+".sig"]; ok {
		t.Error("実行ファイルに .sig を書いた")
	}
	for _, k := range []string{"linux-x86_64/sitate.sig", "linux-arm64/adapters/provision/b2/adapter.json.sig", "sitate-" + bundleVer + "-src.tar.gz.sig", "sitate-" + bundleVer + ".spdx.json.sig"} {
		if _, ok := bodies["bin/"+bundleVer+"/"+k]; !ok {
			t.Errorf("%s が無い", k)
		}
	}
	for k := range bodies {
		if strings.Contains(k, "adapters/check/") {
			t.Errorf("check を写した: %s", k)
		}
	}
}

func TestKeysPhraseVerifyBundleRejects(t *testing.T) {
	cases := map[string]struct {
		mutate func(map[string]bfile)
		code   contract.Code
	}{
		"実行ファイルの sha256 が違う": {func(m map[string]bfile) {
			m["linux-arm64/adapters/provision/b2/b2"] = bfile{[]byte("tampered"), "linux-arm64", false}
		}, contract.CodeSignatureInvalid},
		"adapter.json の署名が違う": {func(m map[string]bfile) {
			b := m["linux-x86_64/adapters/provision/b2/adapter.json"]
			b.arch = "linux-arm64"
			m["linux-x86_64/adapters/provision/b2/adapter.json"] = b
		}, contract.CodeSignatureInvalid},
		"片方の arch の核が無い": {func(m map[string]bfile) { delete(m, "linux-arm64/sitate") }, contract.CodeInternal},
		"提供アダプタが無い":      {func(m map[string]bfile) { delete(m, "linux-x86_64/adapters/provision/b2/adapter.json") }, contract.CodeInternal},
		"実行ファイルが無い":      {func(m map[string]bfile) { delete(m, "linux-x86_64/adapters/provision/b2/b2") }, contract.CodeInternal},
		"ソースが無い":         {func(m map[string]bfile) { delete(m, "sitate-"+bundleVer+"-src.tar.gz") }, contract.CodeInternal},
		"SBOM が無い":       {func(m map[string]bfile) { delete(m, "sitate-"+bundleVer+".spdx.json") }, contract.CodeInternal},
		"ソースの署名が違う": {func(m map[string]bfile) {
			m["sitate-"+bundleVer+"-src.tar.gz"] = bfile{[]byte("src"), "linux-x86_64", true}
		}, contract.CodeSignatureInvalid},
	}
	for name, c := range cases {
		e := newPhraseEnv(true)
		m := layout()
		c.mutate(m)
		useBundle(e, m)
		_, err := e.k.PhraseVerify(ctx, phraseCall(phraseTestPhrase))
		harness.ExpectCode(t, err, c.code)
		if len(e.istore.Puts) != 0 || !pendingKept(e) {
			t.Errorf("%s: 何も書かず保留を残すはず: puts=%v", name, e.istore.Puts)
		}
	}
}

// 保留の種別が未知なら INTERNAL で何も書かない。
func TestKeysPhraseVerifyUnknownPendingOp(t *testing.T) {
	e := newPhraseEnv(true)
	useBundle(e, layout())
	if err := os.WriteFile(filepath.Join(e.dir, contract.KeysPendingDir, contract.KeysPendingMeta), []byte(`{"op":"weird","change_id":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := e.k.PhraseVerify(ctx, phraseCall(phraseTestPhrase))
	harness.ExpectCode(t, err, contract.CodeInternal)
	if len(e.istore.Puts) != 0 {
		t.Error("未知の種別で書いた")
	}
}

// --- F8: 確定のファイルが置けないときは phrase_confirmed を書かない ---
func TestKeysPhraseConfirmedNotSetWhenCommitFails(t *testing.T) {
	e := newPhraseEnv(true)
	useBundle(e, layout())
	if err := os.MkdirAll(filepath.Join(e.dir, contract.KeysMasterFile), 0o700); err != nil { // master.json の置き場を塞ぐ
		t.Fatal(err)
	}
	_, err := e.k.PhraseVerify(ctx, phraseCall(phraseTestPhrase))
	harness.ExpectCode(t, err, contract.CodeInternal)
	if string(e.st.Items["phrase_confirmed"]) == "true" {
		t.Error("確定のファイルを置けなかったのに phrase_confirmed が真")
	}
}

// --- F9: 渡されたフレーズは使い終えたら消える(成功・失敗のどちらでも) ---
func TestKeysPhraseInWiped(t *testing.T) {
	e := newPhraseEnv(true)
	useBundle(e, layout())
	c := phraseCall(phraseTestPhrase)
	if _, err := e.k.PhraseVerify(ctx, c); err != nil {
		t.Fatal(err)
	}
	if len(c.PhraseIn.Reveal()) != 0 {
		t.Error("成功の後にフレーズが残っている")
	}
	e = newPhraseEnv(true)
	e.ks.Open = false
	c = phraseCall(phraseTestPhrase)
	_, _ = e.k.PhraseVerify(ctx, c)
	if len(c.PhraseIn.Reveal()) != 0 {
		t.Error("失敗の後にフレーズが残っている")
	}
}

// --- F12: 非正規の base64 ---
func TestKeysInitRejectsNonCanonicalBase64(t *testing.T) {
	e, call := initPhraseEnv()
	p := call.Args["approval-pubkey"][0]
	call.Args["approval-pubkey"] = []string{p[:20] + "\n" + p[20:]}
	_, err := e.k.Init(ctx, call)
	harness.ExpectCode(t, err, contract.CodeArgInvalid)
}

// --- F13: 状態を失っても確定済みのマスター鍵を上書きしない ---
func TestKeysInitKeepsCommittedFilesWhenStateLost(t *testing.T) {
	e, call := initPhraseEnv()
	if err := os.MkdirAll(filepath.Join(e.dir, "keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, contract.KeysMasterFile), []byte("MASTER"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := e.k.Init(ctx, call)
	harness.ExpectCode(t, err, contract.CodeAlreadyInitialized)
	if b, _ := os.ReadFile(filepath.Join(e.dir, contract.KeysMasterFile)); string(b) != "MASTER" {
		t.Error("マスター鍵を上書きした")
	}
}

// ディレクトリのモード、状態の配列の形のフレーズ。
func TestKeysInitModesAndNoWordsArray(t *testing.T) {
	e, call := initPhraseEnv()
	r, err := e.k.Init(ctx, call)
	harness.Shape(t, []string{"init"}, r, err)
	_ = filepath.WalkDir(e.dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != e.dir {
			if info, _ := d.Info(); info.Mode().Perm() != 0o700 {
				t.Errorf("%s のモード %v", p, info.Mode().Perm())
			}
		}
		return nil
	})
	w := strings.Fields(phraseOf(t, r))
	for k, v := range e.st.Items {
		if bytes.Contains(v, []byte(`"`+w[0]+`","`+w[1]+`"`)) {
			t.Errorf("状態 %s にフレーズの配列がある", k)
		}
	}
}

// --- kit の成功(F3) ---
func TestKeysKitSuccess(t *testing.T) {
	e := newEnv()
	e.st.cand, e.st.candDef = true, candStorage
	e.st.Items = map[string]json.RawMessage{"repo_id": json.RawMessage(`"` + testRepoID + `"`)}
	if err := os.MkdirAll(filepath.Join(e.dir, "keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	repo := `{"format":"sitate.repo/v1","repo_id":"` + testRepoID + `","keyring_id":"` + testKeyringID + `","format_version":1}`
	if err := os.WriteFile(filepath.Join(e.dir, contract.KeysRepoLatest), []byte(repo), 0o600); err != nil {
		t.Fatal(err)
	}
	blob := binary.BigEndian.AppendUint32(nil, 11)
	blob = binary.BigEndian.AppendUint32(append(blob, "ssh-ed25519"...), 32)
	blob = append(blob, bytes.Repeat([]byte{9}, 32)...)
	hk := filepath.Join(e.dir, "host.pub")
	if err := os.WriteFile(hk, []byte("ssh-ed25519 "+base64.StdEncoding.EncodeToString(blob)+" root@host\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.k.HostKeyPubPath = hk
	// 試験ビルドの信頼の起点は環境変数が指す .pub から読む(製品ビルドでは使われず、埋め込みの鍵が使われる)。
	pub := filepath.Join(e.dir, "trust.pub")
	if err := os.WriteFile(pub, []byte("untrusted comment: test\n"+bundleKey.Public()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(contract.EnvTestTrustPubKeyFile, pub)
	var fs []contract.BundleFile
	for p, b := range layout() {
		fs = append(fs, b.sign(p))
	}
	e.k.Bundle = &fake.Bundle{Version: bundleVer, FilesBy: map[string][]contract.BundleFile{"all": fs}}
	r, err := e.k.Kit(ctx, contract.Call{Words: []string{"kit"}, Args: map[string][]string{"format": {"json"}}})
	harness.Shape(t, []string{"kit"}, r, err)
	if strings.Contains(string(r.Result), "legal winner") || r.Phrase != nil {
		t.Error("kit の結果にフレーズ")
	}
	if jsonField(t, e.st.Items["kit"], "version") != jsonField(t, r.Result, "kit_version") {
		t.Error("状態の kit.version が結果と違う")
	}
	// ソースと SBOM は build.sh の出力名で探す(名前に source や sbom を含む別のファイルは拾わない)。
	d, derr := e.k.kitData(ctx, kitBase{RepoID: testRepoID, KeyringVersion: 1, FormatVersion: 1})
	if derr != nil || d.Src != sha256Hex([]byte("src")) || d.SBOM != sha256Hex([]byte("{}")) {
		t.Errorf("ソースと SBOM の SHA-256: %+v %v", d, derr)
	}
}

// --- F4/F5: 設定の食い違い ---

// cfgStore は BucketConfig を固定する保管先。
type cfgStore struct {
	contract.ObjectStore
	cfg contract.BucketConfig
}

func (c cfgStore) BucketConfig(context.Context) (contract.BucketConfig, error) { return c.cfg, nil }

var goodCfg = contract.BucketConfig{Versioning: true, LockMode: "COMPLIANCE", DefaultRetention: contract.RetentionDailyDays}

func badCfgs() map[string]contract.BucketConfig {
	return map[string]contract.BucketConfig{
		"バージョニング無し":  {Versioning: false, LockMode: "COMPLIANCE", DefaultRetention: 90},
		"GOVERNANCE": {Versioning: true, LockMode: "GOVERNANCE", DefaultRetention: 90},
		"既定保持が違う":    {Versioning: true, LockMode: "COMPLIANCE", DefaultRetention: 1},
	}
}

func cfgEnv(cfg contract.BucketConfig, admin string) *kenv {
	e := initEnv(admin)
	e.k.Store = cfgStore{e.istore, cfg}
	return e
}

const adminOK = `{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[]}`

func TestKeysBucketMismatchAllRoutes(t *testing.T) {
	for name, cfg := range badCfgs() {
		// 新規の発行
		e := cfgEnv(cfg, adminOK)
		_, err := e.k.InitStorage(ctx, initCall())
		harness.ExpectCode(t, err, contract.CodeBucketMismatch)
		// 新規の発行: アダプタの応答が要件未達なら鍵を発行しない
		e = cfgEnv(goodCfg, adminOK)
		e.run.Resp["create-bucket"] = ok(`{"created":true,"versioning":` + map[bool]string{true: "true", false: "false"}[cfg.Versioning] + `,"lock_mode":"` + cfg.LockMode + `","default_retention_days":` + map[bool]string{true: "90", false: "1"}[cfg.DefaultRetention == 90] + `}`)
		_, err = e.k.InitStorage(ctx, initCall())
		harness.ExpectCode(t, err, contract.CodeBucketMismatch)
		if len(e.ks.Reqs) != 0 {
			t.Errorf("%s: 要件未達なのに鍵を発行した", name)
		}
		// --reissue
		e, _ = reissueEnv()
		e.k.Store = cfgStore{e.istore, cfg}
		_, err = e.k.InitStorage(ctx, reissueCall())
		harness.ExpectCode(t, err, contract.CodeBucketMismatch)
		if len(e.ks.Revoked) != 0 {
			t.Errorf("%s: 要件未達なのに旧鍵を失効させた", name)
		}
		// 発行済みの鍵がある経路
		e = alreadyEnv()
		e.k.Store = cfgStore{e.istore, cfg}
		_, err = e.k.InitStorage(ctx, initCall())
		harness.ExpectCode(t, err, contract.CodeBucketMismatch)
		// bucket check
		e = newEnv()
		goodBucket(e)
		e.k.Secrets = issuedSecrets{}
		e.k.Store = cfgStore{e.istore, cfg}
		_, err = e.k.BucketCheck(ctx, bucketCall())
		harness.ExpectCode(t, err, contract.CodeBucketMismatch)
	}
}

// 許すべき操作が通らない鍵は KEY_MISSING_PERMISSION。読み取り鍵の版指定なしの削除は KEY_TOO_POWERFUL(警告にしない)。
func TestKeysBucketMissingAndReadUnversionedDelete(t *testing.T) {
	e := newEnv()
	goodBucket(e)
	e.k.Secrets = issuedSecrets{}
	e.k.Store = cfgStore{e.istore, goodCfg}
	e.pr.Allowed["write/put"] = false
	ce := harness.ExpectCode(t, func() error { _, err := e.k.BucketCheck(ctx, bucketCall()); return err }(), contract.CodeKeyMissingPermission)
	if jsonField(t, ce.Detail, "role") != "write" {
		t.Errorf("detail: %s", ce.Detail)
	}
	e = newEnv()
	goodBucket(e)
	e.k.Secrets = issuedSecrets{}
	e.k.Store = cfgStore{e.istore, goodCfg}
	e.pr.Allowed["read/unversioned_delete"] = true
	ce = harness.ExpectCode(t, func() error { _, err := e.k.BucketCheck(ctx, bucketCall()); return err }(), contract.CodeKeyTooPowerful)
	if jsonField(t, ce.Detail, "role") != "read" {
		t.Errorf("detail: %s", ce.Detail)
	}
}

// last_bucket_check は seal の後だけ書く(F14)。
func TestKeysLastBucketCheckOnlyWhenSealed(t *testing.T) {
	e := newEnv()
	goodBucket(e)
	e.k.Secrets = issuedSecrets{}
	e.k.Store = cfgStore{e.istore, goodCfg}
	if _, err := e.k.BucketCheck(ctx, bucketCall()); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.st.Items["last_bucket_check"]; ok {
		t.Error("seal の前に last_bucket_check を書いた")
	}
	e.st.Items["sealed"] = json.RawMessage("true")
	if _, err := e.k.BucketCheck(ctx, bucketCall()); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.st.Items["last_bucket_check"]; !ok {
		t.Error("seal の後に last_bucket_check を書かない")
	}
}

// --- F6: 台帳の並びに依らない ---
type revState struct {
	*stateT
	reverse bool
}

func (s revState) LedgerList(c context.Context) ([]json.RawMessage, error) {
	l, _ := s.stateT.LedgerList(c)
	out := append([]json.RawMessage(nil), l...)
	if s.reverse {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, nil
}

func TestKeysLedgerMergeIsOrderIndependent(t *testing.T) {
	rows := []json.RawMessage{
		json.RawMessage(`{"kind":"issued_key","key_id":"K1","role":"write","revoked_at":null,"orphaned":false}`),
		json.RawMessage(`{"kind":"issued_key","key_id":"K1","role":null,"revoked_at":"2027-03-01T00:00:00Z","orphaned":true}`),
		json.RawMessage(`{"kind":"issued_key","key_id":"K2","role":"read","revoked_at":null,"orphaned":false}`),
	}
	for _, rev := range []bool{false, true} {
		e := newEnv()
		e.st.Ledger = rows
		e.k.State = revState{e.st, rev}
		_, led, err := e.k.ledgerKeys(ctx)
		if err != nil || led["K1"].RevokedAt == nil || !led["K1"].Orphaned || led["K1"].Role == nil || *led["K1"].Role != "write" || led["K2"].RevokedAt != nil {
			t.Errorf("reverse=%v: %+v %v", rev, led, err)
		}
	}
}

// --- F11: 孤児 ---
type failRevokeKS struct{ *recKS }

func (failRevokeKS) RevokeKey(context.Context, contract.RevokeRequest) ([]string, []string, error) {
	return nil, nil, errors.New("revoke failed")
}

// 秘密を置けず、発行した鍵の失効にも失敗したら、その鍵を台帳に orphaned で残す。
func TestKeysOrphanRecordedWhenPutAndRevokeFail(t *testing.T) {
	e := cfgEnv(goodCfg, adminOK)
	e.k.Secrets.(*memSecrets).FailPut = true
	e.k.KeyS = failRevokeKS{e.ks}
	_, err := e.k.InitStorage(ctx, initCall())
	harness.ExpectCode(t, err, contract.CodeInternal)
	n := 0
	for _, l := range e.st.Ledger {
		if jsonField(t, l, "orphaned") == true {
			n++
		}
	}
	if n != 2 {
		t.Errorf("失効できなかった 2 つの鍵が orphaned でない: %s", e.st.Ledger)
	}
	if e.run.called("check:admin") < 1 {
		t.Error("秘密の保存に失敗しても照合へ進む")
	}
}

type badLedgerState struct{ *stateT }

func (badLedgerState) LedgerList(context.Context) ([]json.RawMessage, error) {
	return nil, errors.New("ledger unreadable")
}

// 台帳を読めないときは警告 KEY_RECONCILE_UNVERIFIED で、発行の結果は有効のまま。
func TestKeysLedgerUnreadableIsWarning(t *testing.T) {
	e := cfgEnv(goodCfg, adminOK)
	e.k.State = badLedgerState{e.st}
	r, err := e.k.InitStorage(ctx, initCall())
	harness.Shape(t, []string{"init-storage"}, r, err)
	if findWarn(r, contract.CodeKeyReconcileUnverified) == nil {
		t.Errorf("警告が無い: %+v", r.Warnings)
	}
}

// 失効の結果に載らなかった id は孤児として返す。
func TestKeysIssuePartialRevokeIncompleteIsOrphaned(t *testing.T) {
	r := fake.NewAdapterRunner()
	r.Script("provision",
		fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"contract":"sitate.provision/v1","issued":[{"role":"write","key_id":"K2","secret":"S"}]}`)},
		fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"contract":"sitate.provision/v1","revoked":[],"not_found":[]}`)})
	_, orph, err := provisionKS(r).IssueKeys(ctx, issueReq("write", "read"))
	if err == nil || len(orph) != 1 || orph[0] != "K2" {
		t.Errorf("%v %v", orph, err)
	}
}

// --- 失効の記録 ---
func TestKeysReissueRecordsRevokedAt(t *testing.T) {
	e, _ := reissueEnv()
	e.k.Store = cfgStore{e.istore, goodCfg}
	if _, err := e.k.InitStorage(ctx, reissueCall()); err != nil {
		t.Fatal(err)
	}
	_, led, err := e.k.ledgerKeys(ctx)
	if err != nil || led["K1"].RevokedAt == nil || led["K2"].RevokedAt == nil || led["kid-write"].RevokedAt != nil {
		t.Errorf("失効の記録: %+v %v", led, err)
	}
}

// --- F6/T1: 鍵 ID の束縛(k1 と k2) ---
func TestKeyServiceBindsMasterKeyID(t *testing.T) {
	ks := newKS()
	k1 := master
	k2 := bytes.Repeat([]byte{0x32}, contract.KeySize)
	mj, _ := json.Marshal(map[string]any{"current": "k2", "keys": []map[string]string{
		{"key_id": "k1", "key": base64.StdEncoding.EncodeToString(k1)}, {"key_id": "k2", "key": base64.StdEncoding.EncodeToString(k2)}}})
	if err := os.WriteFile(filepath.Join(ks.Dir, contract.KeysMasterFile), mj, 0o600); err != nil {
		t.Fatal(err)
	}
	key, wrapped, kid, err := ks.NewSnapshotKey(ctx, manifestID)
	if err != nil || kid != "k2" {
		t.Fatalf("%q %v", kid, err)
	}
	if got, err := ks.OpenSnapshotKey(ctx, wrapped, "k2", manifestID); err != nil || !bytes.Equal(got, key) {
		t.Errorf("k2 で開けない: %v", err)
	}
	if _, err := ks.OpenSnapshotKey(ctx, wrapped, "k1", manifestID); err == nil {
		t.Error("k1 で開けた")
	}
	// 参照実装で k2 の鍵と AAD(鍵 ID 2)で開ける。
	d2, _ := harness.FormatDerive(k2, "snapshot-wrap")
	aad, _ := (&harness.RefCodec{}).AAD(contract.MagicWrapSnap, strings.TrimPrefix(manifestID, "m-"), contract.Header{FormatVersion: contract.FormatVersion, Mark: binary.BigEndian.AppendUint32(nil, 2)})
	if got, err := (&harness.RefCrypto{}).AEADOpen(wrapped, d2, aad); err != nil || !bytes.Equal(got, key) {
		t.Errorf("鍵 ID 2 の AAD で開けない: %v", err)
	}
	// 旧鍵(k1)で包まれたチャンク鍵の包みも、現行(k2)の下で開ける。
	ck := bytes.Repeat([]byte{0x44}, contract.KeySize)
	id := strings.Repeat("ab", contract.ChunkIDSize)
	old := newKS()
	w, _ := old.WrapChunkKey(ck, id)
	if got, err := ks.UnwrapChunkKey(w, id); err != nil || !bytes.Equal(got, ck) {
		t.Errorf("旧鍵の包みを開けない: %v", err)
	}
}

// --- F5: キーリングの見出しの上限 ---
type headerCrypto struct{ contract.Crypto }

func (headerCrypto) Argon2id([]byte, []byte, int, int, int) ([]byte, error) {
	return nil, &contract.FormatError{Reason: "header"}
}

func TestOpenKeyringLimits(t *testing.T) {
	codec := &harness.RefCodec{}
	words, obj, _, err := newKeyring(codec, testRepoID, master, kitT0)
	if err != nil {
		t.Fatal(err)
	}
	phrase := contract.NewSecret([]byte(strings.Join(words, " ")))
	ks := newKS()
	if ok, err := ks.OpenKeyring(ctx, phrase, obj); err != nil || !ok {
		t.Fatalf("上限どおりのキーリングを開けない: %v %v", ok, err)
	}
	over := append([]byte(nil), obj...)
	binary.BigEndian.PutUint32(over[21:], contract.Argon2idMemoryMaxKiB+1) // 上限のすぐ上
	_, err = ks.OpenedKeyring(ctx, phrase, over)
	harness.ExpectCode(t, err, contract.CodePhraseInvalid)
	if ok, err := ks.OpenKeyring(ctx, phrase, over); ok || err != nil {
		t.Errorf("OpenKeyring: %v %v", ok, err)
	}
	over = append([]byte(nil), obj...)
	binary.BigEndian.PutUint32(over[25:], contract.Argon2idIterationsMax+1)
	if ok, err := ks.OpenKeyring(ctx, phrase, over); ok || err != nil {
		t.Errorf("反復の上限: %v %v", ok, err)
	}
	// Crypto が header の誤りを返したら開けない扱い(INTERNAL にしない)。
	ks.Crypto = headerCrypto{ks.Crypto}
	if ok, err := ks.OpenKeyring(ctx, phrase, obj); ok || err != nil {
		t.Errorf("Crypto の header 誤り: %v %v", ok, err)
	}
}
