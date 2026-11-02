package keys

// 契約試験(作業指示書 3.2): 試験ベクトルと参照実装(harness の RefCodec・RefCrypto)を実物(KeyService・Verifier・Keys)に通す。
// 骨格は NOT_IMPLEMENTED で落ち、nil を返すだけの実装は値の比較で落ち、仕様どおりの実装は panic しない。WP は骨格を実装し、この試験は変えない。
// 依存は公開フィールドで差し込む(skeleton.go の注釈)。他 WP のパッケージ(format など)は import しない。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	bip39 "github.com/tyler-smith/go-bip39"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

var (
	ctx    = context.Background()
	master = bytes.Repeat([]byte{0x31}, contract.KeySize)
)

// newKS は状態ディレクトリ(keys/master.json を spec 4.5 の形で置いたもの)を持つ KeyService。マスター鍵の値は keys の中だけが読む。
func newKS() *KeyService {
	d := nextDir()
	if err := os.MkdirAll(filepath.Join(d, "keys"), 0o700); err != nil {
		panic(err)
	}
	mj, _ := json.Marshal(map[string]any{"current": "k1", "keys": []map[string]string{{"key_id": "k1", "key": base64.StdEncoding.EncodeToString(master)}}})
	if err := os.WriteFile(filepath.Join(d, contract.KeysMasterFile), mj, 0o600); err != nil {
		panic(err)
	}
	return &KeyService{Dir: d, Codec: &harness.RefCodec{}, Crypto: &harness.RefCrypto{}}
}

func derive(t *testing.T, purpose string) []byte {
	t.Helper()
	k, err := harness.FormatDerive(master, purpose)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestContractKeyServiceDerivedKey(t *testing.T) {
	ks := newKS()
	for _, p := range []string{"config", "journal", "state", "proof"} {
		got, err := ks.DerivedKey(p)
		harness.Implemented(t, err)
		if err != nil || !bytes.Equal(got, derive(t, p)) {
			t.Errorf("DerivedKey(%s) = %x %v", p, got, err)
		}
	}
	// マスター鍵そのもの・ID 鍵・包む鍵は境界を越えない。
	for _, p := range []string{"master", "chunk-id", "snapshot-wrap", "chunk-wrap", ""} {
		if k, err := ks.DerivedKey(p); err == nil {
			t.Errorf("DerivedKey(%q) が %x を返した(用途 config/journal/state/proof 以外は拒否)", p, k)
		}
	}
}

func TestContractKeyServiceChunkID(t *testing.T) {
	ks := newKS()
	idKey := derive(t, "chunk-id")
	for _, pl := range [][]byte{[]byte("hello chunk"), {}, bytes.Repeat([]byte{1}, 3000)} {
		want, _ := harness.FormatChunkID(idKey, pl)
		var got string
		harness.Catch(t, func() { got = ks.ChunkID(pl) })
		if got != want {
			t.Errorf("ChunkID = %q want %q", got, want)
		}
	}
}

func TestContractKeyServiceCandidateFingerprint(t *testing.T) {
	ks := newKS()
	idKey := derive(t, "chunk-id")
	for _, c := range []struct{ kind, path string }{{"mount", "/data"}, {"postgresql_cluster", "/var/lib/db/17"}} {
		want, _ := harness.FormatUncoveredFP(idKey, c.kind, []byte(c.path))
		var got string
		harness.Catch(t, func() { got = ks.CandidateFingerprint(c.kind, c.path) })
		if got != want {
			t.Errorf("CandidateFingerprint(%s,%s) = %q want %q", c.kind, c.path, got, want)
		}
	}
}

func TestContractKeyServiceEntryRef(t *testing.T) {
	ks := newKS()
	var a, b, c string
	harness.Catch(t, func() { a, b, c = ks.EntryRef("t1/a.txt"), ks.EntryRef("t1/a.txt"), ks.EntryRef("t1/b.txt") })
	if a == "" || a != b || a == c {
		t.Errorf("EntryRef は空でなく、パスで決まり(決定的)、別のパスで違う: %q %q %q", a, b, c)
	}
	if strings.Contains(a, "a.txt") {
		t.Errorf("EntryRef にパスが漏れている: %q", a)
	}
}

const manifestID = "m-44444444444444444444444444444444"

func TestContractKeyServiceSnapshotKey(t *testing.T) {
	ks := newKS()
	key, wrapped, keyID, err := ks.NewSnapshotKey(ctx, manifestID)
	harness.Implemented(t, err)
	if err != nil || len(key) != contract.KeySize || len(wrapped) != contract.NonceSize+contract.KeySize+contract.TagSize || keyID != contract.MasterKeyIDString(1) {
		t.Fatalf("NewSnapshotKey: %d バイト, 包み %d, %q, %v", len(key), len(wrapped), keyID, err)
	}
	if bytes.Contains(wrapped, key) {
		t.Error("包みに鍵がそのまま入っている")
	}
	// 包みは snapshot-wrap 鍵で、AAD = 版 ∥ WRPS ∥ manifest_id の乱数部(16) ∥ 鍵 ID(4) の XChaCha20-Poly1305。
	mark := binary.BigEndian.AppendUint32(nil, 1)
	aad, _ := (&harness.RefCodec{}).AAD(contract.MagicWrapSnap, strings.TrimPrefix(manifestID, "m-"), contract.Header{FormatVersion: contract.FormatVersion, Mark: mark})
	if got, err := (&harness.RefCrypto{}).AEADOpen(wrapped, derive(t, "snapshot-wrap"), aad); err != nil || !bytes.Equal(got, key) {
		t.Errorf("参照実装で開けない: %v", err)
	}
	if back, err := ks.OpenSnapshotKey(ctx, wrapped, keyID, manifestID); err != nil || !bytes.Equal(back, key) {
		t.Errorf("OpenSnapshotKey: %v", err)
	}
	if _, err := ks.OpenSnapshotKey(ctx, wrapped, keyID, "m-55555555555555555555555555555555"); err == nil {
		t.Error("別の manifest_id で開けた")
	}
	if k2, _, _, _ := ks.NewSnapshotKey(ctx, manifestID); bytes.Equal(k2, key) {
		t.Error("スナップショット鍵が乱数でない")
	}
}

func TestContractKeyServiceChunkKeyWrap(t *testing.T) {
	ks := newKS()
	chunkKey := bytes.Repeat([]byte{0x7a}, contract.KeySize)
	id := strings.Repeat("11", contract.ChunkIDSize)
	w, err := ks.WrapChunkKey(chunkKey, id)
	harness.Implemented(t, err)
	if err != nil || len(w) != contract.NonceSize+contract.KeySize+contract.TagSize {
		t.Fatalf("WrapChunkKey: %d バイト %v", len(w), err)
	}
	aad, _ := (&harness.RefCodec{}).AAD(contract.MagicWrapChunk, id, contract.Header{FormatVersion: contract.FormatVersion, Mark: binary.BigEndian.AppendUint32(nil, 1)})
	if got, err := (&harness.RefCrypto{}).AEADOpen(w, derive(t, "chunk-wrap"), aad); err != nil || !bytes.Equal(got, chunkKey) {
		t.Errorf("参照実装で開けない(chunk-wrap 鍵・AAD WRPC): %v", err)
	}
	if back, err := ks.UnwrapChunkKey(w, id); err != nil || !bytes.Equal(back, chunkKey) {
		t.Errorf("UnwrapChunkKey: %v", err)
	}
	if _, err := ks.UnwrapChunkKey(w, strings.Repeat("22", contract.ChunkIDSize)); err == nil {
		t.Error("別のチャンク ID で開けた")
	}
}

// keyring は参照実装でキーリングの object を作る(spec 4.3。見出し = SITK ∥ 版 ∥ keyring_id(16) ∥ Argon2id のパラメータ(25))。
func keyring(t *testing.T, entropy []byte) []byte { return keyringV(t, entropy, 1) }

// keyringV は版番号 ver のキーリングの object を作る。
func keyringV(t *testing.T, entropy []byte, ver int) []byte {
	t.Helper()
	salt := bytes.Repeat([]byte{0xbb}, 16)
	mark := binary.BigEndian.AppendUint32(nil, 64)
	mark = binary.BigEndian.AppendUint32(mark, 1)
	mark = append(append(mark, 1), salt...)
	kek, err := (&harness.RefCrypto{}).Argon2id(entropy, salt, 64, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"format": "sitate.keyring/v1", "repo_id": "rp-1", "keyring_id": "kr-99999999999999999999999999999999",
		"keyring_version": ver, "current_key_id": "k1", "keys": []map[string]string{{"key_id": "k1", "key": hex.EncodeToString(master)}}})
	obj, err := (&harness.RefCodec{}).Encode(contract.MagicKeyring, strings.Repeat("99", 16), contract.Header{Kind: contract.MagicKeyring, FormatVersion: contract.FormatVersion, Mark: mark}, body, kek)
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

func TestContractKeyServiceOpenKeyring(t *testing.T) {
	ent := bytes.Repeat([]byte{0x05}, 32)
	phrase, _ := bip39.NewMnemonic(ent)
	other, _ := bip39.NewMnemonic(bytes.Repeat([]byte{0x06}, 32))
	kr := keyring(t, ent)
	ks := newKS()
	ok, err := ks.OpenKeyring(ctx, contract.NewSecret([]byte(phrase)), kr)
	harness.Implemented(t, err)
	if err != nil || !ok {
		t.Fatalf("正しいフレーズで開けない: %v %v", ok, err)
	}
	// 前後の空白・大文字小文字・CR は無視する(spec 4.3 フレーズの入力)。
	if ok, err := ks.OpenKeyring(ctx, contract.NewSecret([]byte("  "+strings.ToUpper(phrase)+"\r")), kr); err != nil || !ok {
		t.Errorf("正規化したフレーズで開けない: %v %v", ok, err)
	}
	if ok, err := ks.OpenKeyring(ctx, contract.NewSecret([]byte(other)), kr); err != nil || ok {
		t.Errorf("別のフレーズ(形は正しい)は (false, nil): %v %v", ok, err)
	}
	words := strings.Fields(phrase)
	words[23] = words[22] // チェックサムが合わない
	_, err = ks.OpenKeyring(ctx, contract.NewSecret([]byte(strings.Join(words, " "))), kr)
	harness.ExpectCode(t, err, contract.CodePhraseInvalid)
	_, err = ks.OpenKeyring(ctx, contract.NewSecret([]byte("one two three")), kr)
	harness.ExpectCode(t, err, contract.CodePhraseInvalid)
}

func provisionKS(r *fake.AdapterRunner) *KeyService {
	ks := newKS()
	ks.Provision, ks.ProvisionName = r, "b2"
	return ks
}

func issueReq(roles ...string) contract.IssueRequest {
	return contract.IssueRequest{Endpoint: "https://s3.example.invalid", Bucket: "bkt-1", Prefix: "sitate/", Roles: roles, MaxRetentionDays: 90, Admin: contract.NewSecret([]byte(`{"key_id":"ADMIN-ID","secret":"ADMIN-SECRET-VALUE"}`))}
}

// IssueKeys は issue-keys を 1 回だけ呼び、roles の順に鍵を返す。要求に endpoint・bucket・prefix・roles・管理用鍵(secrets)を載せる。
func TestContractKeyServiceIssueKeys(t *testing.T) {
	r := fake.NewAdapterRunner()
	r.Script("provision", fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"contract":"sitate.provision/v1","issued":[{"role":"write","key_id":"K2","secret":"SYNTH-SECRET-K2"},{"role":"read","key_id":"K3","secret":"SYNTH-SECRET-K3"}]}`)})
	got, orph, err := provisionKS(r).IssueKeys(ctx, issueReq("write", "read"))
	harness.Implemented(t, err)
	if err != nil || len(orph) != 0 || len(got) != 2 || got[0].Role != "write" || got[0].KeyID != "K2" || got[1].Role != "read" || got[1].KeyID != "K3" ||
		got[0].Secret == nil || string(got[0].Secret.Reveal()) != "SYNTH-SECRET-K2" || string(got[1].Secret.Reveal()) != "SYNTH-SECRET-K3" {
		t.Fatalf("IssueKeys = %+v %v %v (write K2・read K3 の順)", got, orph, err)
	}
	// 秘密は文字列化・JSON 化で出ない(contract.Secret)。
	b, _ := json.Marshal(got)
	if s := fmt.Sprint(got[0].Secret) + string(b); strings.Contains(s, "SYNTH-SECRET-K2") {
		t.Errorf("IssuedKey の文字列化・JSON 化に秘密が出た: %s", s)
	}
	if len(r.Calls) != 1 || r.Calls[0].Kind != "provision" || r.Calls[0].Name != "b2" {
		t.Fatalf("保管先準備アダプタの呼び出しは 1 回: %+v", r.Calls)
	}
	var rq struct {
		Op       string          `json:"op"`
		Endpoint string          `json:"endpoint"`
		Bucket   string          `json:"bucket"`
		Prefix   string          `json:"prefix"`
		Roles    []string        `json:"roles"`
		Max      int             `json:"max_retention_days"`
		Secrets  json.RawMessage `json:"secrets"`
	}
	_ = json.Unmarshal(r.Calls[0].Req, &rq)
	wantProvisionSchema(t, "issue_keys", r.Calls[0].Req)
	if rq.Op != "issue-keys" || rq.Endpoint != "https://s3.example.invalid" || rq.Bucket != "bkt-1" || rq.Prefix != "sitate/" || !slices.Equal(rq.Roles, []string{"write", "read"}) || rq.Max != 90 || !strings.Contains(string(rq.Secrets), "ADMIN-SECRET-VALUE") {
		t.Errorf("issue-keys の要求: %s", r.Calls[0].Req)
	}
}

// 全部か無しか(spec 3.2.6): アダプタが要求の役割の一部しか返さないなら、返った鍵を失効させて誤りを返す。
func TestContractKeyServiceIssueKeysAllOrNothing(t *testing.T) {
	r := fake.NewAdapterRunner()
	r.Script("provision",
		fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"contract":"sitate.provision/v1","issued":[{"role":"write","key_id":"K2","secret":"S"}]}`)},
		fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"contract":"sitate.provision/v1","revoked":["K2"]}`)})
	got, orph, err := provisionKS(r).IssueKeys(ctx, issueReq("write", "read"))
	harness.Implemented(t, err)
	if err == nil || len(got) != 0 || len(orph) != 0 {
		t.Fatalf("一部しか発行されないのに成功した/鍵を返した: %+v %v %v", got, orph, err)
	}
	if len(r.Calls) != 2 || !strings.Contains(string(r.Calls[1].Req), `"revoke-keys"`) || !strings.Contains(string(r.Calls[1].Req), "K2") {
		t.Errorf("返った K2 を失効させていない: %+v", r.Calls)
	} else {
		wantProvisionSchema(t, "revoke_keys", r.Calls[1].Req)
	}
}

// 一部しか発行されず、返った鍵の失効にも失敗したら、その鍵の ID を orphaned に返す(核が台帳に記録する。spec 3.2.6)。
// (アダプタの error.orphaned_key_ids[] を運ぶ口は AdapterRunner の ADAPTER_ERROR の detail に無い: 報告の「設計への疑義」)
func TestContractKeyServiceIssueKeysOrphaned(t *testing.T) {
	r := fake.NewAdapterRunner()
	r.Script("provision",
		fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"contract":"sitate.provision/v1","issued":[{"role":"write","key_id":"K2","secret":"S"}]}`)},
		fake.Step{Outcome: fake.OutcomeError, ErrCode: "quota"})
	got, orph, err := provisionKS(r).IssueKeys(ctx, issueReq("write", "read"))
	harness.Implemented(t, err)
	if err == nil || len(got) != 0 || !slices.Equal(orph, []string{"K2"}) {
		t.Errorf("失効に失敗した K2 を orphaned に返していない: %+v %v %v", got, orph, err)
	}
}

// wantProvisionSchema はアダプタへの要求が schema/adapter/provision.json の def に合うことを確かめる。
// あわせて、必須の欄を落とした同じ要求は同じ検査で落ちること(検査が空振りでないこと)を確かめる。
func wantProvisionSchema(t *testing.T, def string, req json.RawMessage) {
	t.Helper()
	if err := contract.Validate("adapter/provision.json#/$defs/"+def, req); err != nil {
		t.Errorf("要求が provision.json の %s に合わない: %v\n%s", def, err, req)
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(req, &m) != nil {
		return
	}
	delete(m, "bucket")
	bad, _ := json.Marshal(m)
	if contract.Validate("adapter/provision.json#/$defs/"+def, bad) == nil {
		t.Errorf("bucket の無い要求が provision.json の %s を通った(検査が効いていない)", def)
	}
}

// RevokeKey は revoke-keys を 1 回だけ呼ぶ。要求は provision.json の revoke_keys に合い(endpoint・bucket・key_ids・管理用鍵の secrets)、
// 結果の revoked・not_found を返す。Roles を渡したときだけ要求に roles を入れる。
func TestContractKeyServiceRevokeKey(t *testing.T) {
	r := fake.NewAdapterRunner()
	r.Script("provision", fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"contract":"sitate.provision/v1","revoked":["K2"],"not_found":["K7"]}`)})
	admin := contract.NewSecret([]byte(`{"key_id":"ADMIN-ID","secret":"ADMIN-SECRET-VALUE"}`))
	rev, nf, err := provisionKS(r).RevokeKey(ctx, contract.RevokeRequest{Endpoint: "https://s3.example.invalid", Bucket: "bkt-1", KeyIDs: []string{"K2", "K7"}, Roles: []string{"read"}, Admin: admin})
	harness.Implemented(t, err)
	if err != nil || !slices.Equal(rev, []string{"K2"}) || !slices.Equal(nf, []string{"K7"}) || len(r.Calls) != 1 {
		t.Fatalf("RevokeKey = %v %v %v %+v", rev, nf, err, r.Calls)
	}
	req := r.Calls[0].Req
	wantProvisionSchema(t, "revoke_keys", req)
	var rq struct {
		Op       string          `json:"op"`
		Endpoint string          `json:"endpoint"`
		Bucket   string          `json:"bucket"`
		KeyIDs   []string        `json:"key_ids"`
		Roles    []string        `json:"roles"`
		Secrets  json.RawMessage `json:"secrets"`
	}
	_ = json.Unmarshal(req, &rq)
	if rq.Op != "revoke-keys" || rq.Endpoint != "https://s3.example.invalid" || rq.Bucket != "bkt-1" || !slices.Equal(rq.KeyIDs, []string{"K2", "K7"}) ||
		!slices.Equal(rq.Roles, []string{"read"}) || !strings.Contains(string(rq.Secrets), "ADMIN-SECRET-VALUE") {
		t.Errorf("revoke-keys の要求: %s", req)
	}
	// Roles が空なら roles を要求に入れない(任意の欄)。
	r2 := fake.NewAdapterRunner()
	r2.Script("provision", fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"contract":"sitate.provision/v1","revoked":["K2"],"not_found":[]}`)})
	_, _, err = provisionKS(r2).RevokeKey(ctx, contract.RevokeRequest{Endpoint: "https://s3.example.invalid", Bucket: "bkt-1", KeyIDs: []string{"K2"}, Admin: admin})
	if err != nil || len(r2.Calls) != 1 {
		t.Fatalf("RevokeKey(Roles なし) = %v %+v", err, r2.Calls)
	}
	wantProvisionSchema(t, "revoke_keys", r2.Calls[0].Req)
	if strings.Contains(string(r2.Calls[0].Req), `"roles"`) {
		t.Errorf("Roles が空なのに roles を入れた: %s", r2.Calls[0].Req)
	}
}

func TestContractVerifierVerify(t *testing.T) {
	key := harness.NewMinisignKey(bytes.Repeat([]byte{0x41}, 32))
	other := harness.NewMinisignKey(bytes.Repeat([]byte{0x42}, 32))
	target := []byte(`{"contract":"sitate.adapter/v1"}`)
	want := contract.TrustedComment{Version: "1.0.0", Arch: "linux-x86_64", Path: "linux-x86_64/adapters/check/entropy-ratio/adapter.json"}
	tc := "version=1.0.0 arch=linux-x86_64 path=" + want.Path
	v := &Verifier{Anchors: []string{key.Public()}}
	ok, err := v.Verify(ctx, target, key.Sign(target, tc), want)
	harness.Implemented(t, err)
	if err != nil || !ok {
		t.Fatalf("正しい署名を通さない: %v %v", ok, err)
	}
	bad := map[string]struct {
		target, sig []byte
		want        contract.TrustedComment
	}{
		"対象が違う":              {[]byte(`{"x":1}`), key.Sign(target, tc), want},
		"trusted comment の版": {target, key.Sign(target, tc), contract.TrustedComment{Version: "1.0.1", Arch: want.Arch, Path: want.Path}},
		"arch が違う":           {target, key.Sign(target, tc), contract.TrustedComment{Version: want.Version, Arch: "linux-arm64", Path: want.Path}},
		"path が違う":           {target, key.Sign(target, tc), contract.TrustedComment{Version: want.Version, Arch: want.Arch, Path: "x"}},
		"信頼の起点にない鍵":          {target, other.Sign(target, tc), want},
		"空の署名":               {target, nil, want},
	}
	for name, c := range bad {
		if ok, _ := v.Verify(ctx, c.target, c.sig, c.want); ok {
			t.Errorf("%s: 通してはいけない署名を通した", name)
		}
	}
}

func TestContractKeysInit(t *testing.T) {
	k := &Keys{State: &fake.StateFiles{}, KeyS: fake.KeyService{}, Clock: &fake.Clock{}}
	_, err := k.Init(ctx, contract.Call{Words: []string{"init"}, SecretOut: false})
	harness.ExpectCode(t, err, contract.CodeSecretFdRequired)
}

func TestContractKeysKit(t *testing.T) {
	k := &Keys{State: &fake.StateFiles{}, KeyS: fake.KeyService{}, Clock: &fake.Clock{}}
	for _, f := range []string{"pdf", "printable"} {
		_, err := k.Kit(ctx, contract.Call{Words: []string{"kit"}, Args: map[string][]string{"format": {f}}})
		ce := harness.ExpectCode(t, err, contract.CodeArgInvalid)
		if !strings.Contains(string(ce.Detail), "format") {
			t.Errorf("--format %s: detail.flag に format が無い: %s", f, ce.Detail)
		}
	}
	r, err := k.Kit(ctx, contract.Call{Words: []string{"kit"}, Args: map[string][]string{"format": {"json"}}})
	harness.ShapeOrError(t, []string{"kit"}, r, err)
}

func TestContractKeysStorageIssueReadKey(t *testing.T) {
	harness.Later(t, 2, "Keys.StorageIssueReadKey")
}

func TestContractKeysStorageRevokeReadKey(t *testing.T) {
	harness.Later(t, 2, "Keys.StorageRevokeReadKey")
}

func TestContractKeysSecretsSet(t *testing.T) {
	harness.Later(t, 3, "Keys.SecretsSet")
}

func TestContractKeyServiceOpenedKeyring(t *testing.T) {
	// 版は開いたキーリングの中の値そのもの(1 から始まり、確定のたびに 1 増える。生成は init・フレーズ再発行・鍵の更新)。
	ent := bytes.Repeat([]byte{0x05}, 32)
	phrase, _ := bip39.NewMnemonic(ent)
	other, _ := bip39.NewMnemonic(bytes.Repeat([]byte{0x06}, 32))
	ks := newKS()
	for _, ver := range []int{1, 2, 7} {
		got, err := ks.OpenedKeyring(ctx, contract.NewSecret([]byte(phrase)), keyringV(t, ent, ver))
		harness.Implemented(t, err)
		if err != nil || got.Version != ver || got.KeyringID != "kr-99999999999999999999999999999999" || got.RepoID != "rp-1" || !slices.Equal(got.KeyIDs, []string{"k1"}) {
			t.Errorf("OpenedKeyring(版 %d のキーリング) = %+v %v (版・keyring_id・repo_id・鍵 ID はキーリングの中の値)", ver, got, err)
		}
	}
	_, err := ks.OpenedKeyring(ctx, contract.NewSecret([]byte(other)), keyringV(t, ent, 2))
	harness.ExpectCode(t, err, contract.CodePhraseInvalid)
}
