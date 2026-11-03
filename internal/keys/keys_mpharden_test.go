package keys

// まとめレビュー C1(保留の差し込み)・H2(確定前後の init の差し替え)の試験。

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// 確定済みの状態に、別のキーリングの保留(restore などで差し込まれたもの)があっても確定しない。
func TestPhraseVerifyRejectsPlantedPendingAfterConfirm(t *testing.T) {
	e := newPhraseEnv(false)
	realKR := []byte("REAL-KEYRING")
	if err := os.MkdirAll(filepath.Join(e.dir, "keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	real := []byte(`{"current":"k1","keys":[]}`)
	if err := os.WriteFile(filepath.Join(e.dir, contract.KeysMasterFile), real, 0o600); err != nil {
		t.Fatal(err)
	}
	e.st.Items["phrase_confirmed"] = json.RawMessage(`true`)
	e.st.Items["repo_id"] = json.RawMessage(`"` + testRepoID + `"`)
	e.st.Items["last_phrase_verify"] = json.RawMessage(`{"keyring_hash":"sha256:` + hashHex(realKR) + `","keyring_version_id":"v1"}`)
	writePending(e.dir, "key-rotate", []byte("ATTACKER-KEYRING"), nil, []byte(`{"current":"k9","keys":[]}`))
	_ = os.Remove(filepath.Join(e.dir, contract.KeysPendingDir, contract.KeysPendingRepo)) // repo.json の無い保留(key-rotate の形)
	_, err := e.k.PhraseVerify(ctx, phraseCall(phraseTestPhrase))
	harness.ExpectCode(t, err, contract.CodeAlreadyInitialized)
	if b, _ := os.ReadFile(filepath.Join(e.dir, contract.KeysMasterFile)); string(b) != string(real) {
		t.Errorf("master.json が差し替わった: %s", b)
	}
	if bodies := storeBodies(t, e); len(bodies) != 0 {
		t.Errorf("保管先へ書いた: %v", bodies)
	}
}

// 保留の repo_id が状態の repo_id と違えば確定しない。
func TestPhraseVerifyRejectsPendingOfOtherRepo(t *testing.T) {
	e := newPhraseEnv(true)
	e.st.Items["repo_id"] = json.RawMessage(`"rp-other"`)
	e.st.Items["phrase_confirmed"] = json.RawMessage(`false`)
	_, err := e.k.PhraseVerify(ctx, phraseCall(phraseTestPhrase))
	harness.ExpectCode(t, err, contract.CodeAlreadyInitialized)
	if bodies := storeBodies(t, e); len(bodies) != 0 {
		t.Errorf("保管先へ書いた: %v", bodies)
	}
}

func hashHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func otherPubkey() string {
	b := binary.BigEndian.AppendUint32(nil, 11)
	b = binary.BigEndian.AppendUint32(append(b, "ssh-ed25519"...), 32)
	return base64.StdEncoding.EncodeToString(append(b, bytes.Repeat([]byte{0x09}, 32)...))
}

// H2: 確定前でも、登録済みの承認鍵と指紋が違う鍵での init は拒否し、承認鍵もフレーズも保留も変えない。
func TestKeysInitRerunWithOtherApprovalKeyRejected(t *testing.T) {
	e, call := initPhraseEnv()
	useBundle(e, layout())
	if _, err := e.k.Init(ctx, call); err != nil {
		t.Fatal(err)
	}
	before := string(e.st.Items["approval_key"])
	pendBefore, _ := os.ReadFile(filepath.Join(e.dir, contract.KeysPendingDir, contract.KeysPendingKeyring))
	call2 := call
	call2.Args = map[string][]string{"approval-pubkey": {otherPubkey()}}
	r, err := e.k.Init(ctx, call2)
	harness.ExpectCode(t, err, contract.CodeAlreadyInitialized)
	if r.Phrase != nil {
		t.Error("拒否したのにフレーズを返した")
	}
	if string(e.st.Items["approval_key"]) != before || before == "" {
		t.Errorf("approval_key が変わった: %s -> %s", before, e.st.Items["approval_key"])
	}
	if now, _ := os.ReadFile(filepath.Join(e.dir, contract.KeysPendingDir, contract.KeysPendingKeyring)); !bytes.Equal(now, pendBefore) {
		t.Error("保留が作り直された")
	}
}

// H2: seal の後は確定前でも init は ALREADY_INITIALIZED で何も変えない。
func TestKeysInitAfterSealRejected(t *testing.T) {
	e, call := initPhraseEnv()
	useBundle(e, layout())
	if _, err := e.k.Init(ctx, call); err != nil {
		t.Fatal(err)
	}
	e.st.Items["sealed"] = json.RawMessage(`true`)
	before := string(e.st.Items["approval_key"])
	pendBefore, _ := os.ReadFile(filepath.Join(e.dir, contract.KeysPendingDir, contract.KeysPendingKeyring))
	r, err := e.k.Init(ctx, call)
	harness.ExpectCode(t, err, contract.CodeAlreadyInitialized)
	if r.Phrase != nil || string(e.st.Items["approval_key"]) != before {
		t.Error("seal の後の init が状態を変えた")
	}
	if now, _ := os.ReadFile(filepath.Join(e.dir, contract.KeysPendingDir, contract.KeysPendingKeyring)); !bytes.Equal(now, pendBefore) {
		t.Error("保留が作り直された")
	}
}
