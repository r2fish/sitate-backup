package keys

// init(spec 3.1.2.1。イテレーション 1 は --adopt 以外): マスター鍵とフレーズの生成、承認鍵の登録、タイマーの登録。
// キーリングと repo.json は keys/pending/ に保留し、phrase verify の確定で初めて保管先へ書く(spec 4.3)。フレーズは Reply.Phrase に秘密のレコードで返す。

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const approvalKeyType = "ssh-ed25519"

// fingerprint は OpenSSH の blob の指紋(`SHA256:` + SHA-256 の base64 の末尾の = を除いたもの。spec 4.5)。
func fingerprint(blob []byte) string {
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// validEd25519Blob は OpenSSH の ssh-ed25519 の公開鍵の blob(長さ付きの鍵種別 ∥ 長さ付きの 32 バイトの鍵)か。別の種類の鍵は受けない。
func validEd25519Blob(b []byte) bool {
	want := append(binary.BigEndian.AppendUint32(nil, uint32(len(approvalKeyType))), approvalKeyType...)
	want = binary.BigEndian.AppendUint32(want, ed25519.PublicKeySize)
	return len(b) == len(want)+ed25519.PublicKeySize && bytes.Equal(b[:len(want)], want)
}

// initConfirmed は init の確定が済んだか(または repo.json を書いた後の確定の途中か)。済んでいれば init は何も変えない。
func (k *Keys) initConfirmed(ctx context.Context) (bool, error) {
	var confirmed bool
	if _, err := k.getItem(ctx, "phrase_confirmed", &confirmed); err != nil {
		return false, err
	}
	if confirmed {
		return true, nil
	}
	// 状態を失っても、確定後のファイルがあれば確定済みとして扱う(マスター鍵を上書きしない)。
	for _, rel := range []string{contract.KeysMasterFile, contract.KeysKeyringLatest, contract.KeysRepoLatest} {
		if _, err := os.Stat(filepath.Join(k.Dir, rel)); err == nil {
			return true, nil
		}
	}
	var objs []objRec
	if _, err := k.getItem(ctx, "objects", &objs); err != nil {
		return false, err
	}
	for _, o := range objs {
		if o.Key == "repo.json" {
			return true, nil
		}
	}
	return false, nil
}

// Init は契約 contract.Keys を満たす。
func (k *Keys) Init(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if !c.SecretOut {
		return contract.Reply{}, mkErr(contract.CodeSecretFdRequired, empty{})
	}
	if err := errNeedDeps(k.State != nil, k.Codec != nil, k.Clock != nil, k.Dir != ""); err != nil {
		return contract.Reply{}, err
	}
	if argFlag(c, "adopt") {
		return contract.Reply{}, contract.ErrNotImplemented("keys.Keys.Init --adopt")
	}
	pub, ok := argOne(c, "approval-pubkey")
	if !ok || pub == "" {
		return contract.Reply{}, errArg("--approval-pubkey", "missing")
	}
	blob, err := base64.StdEncoding.DecodeString(pub)
	if err != nil || !validEd25519Blob(blob) || base64.StdEncoding.EncodeToString(blob) != pub {
		return contract.Reply{}, errArg("--approval-pubkey", "format")
	}
	if t, found, derr := k.storageTarget(ctx, c); derr != nil {
		return contract.Reply{}, derr
	} else if !found || t.KeyRef == "" {
		return contract.Reply{}, mkErr(contract.CodeDefInvalid, map[string]any{"issues": []map[string]string{{"pointer": "", "reason": "missing"}}})
	}
	if done, err := k.initConfirmed(ctx); err != nil {
		return contract.Reply{}, err
	} else if done {
		return contract.Reply{}, mkErr(contract.CodeAlreadyInitialized, empty{})
	}
	now, err := k.now(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	return k.initCreate(ctx, now, pub, blob)
}

// newMaster は新しいマスター鍵(乱数 32 バイト、鍵 ID k1)を作り、keys/master.json の形と鍵そのものを返す。
func newMaster() (file, key []byte, err error) {
	key = make([]byte, contract.KeySize)
	if _, err = rand.Read(key); err != nil {
		return nil, nil, errInternal()
	}
	file, err = json.Marshal(map[string]any{"current": "k1", "keys": []map[string]string{{"key_id": "k1", "key": base64.StdEncoding.EncodeToString(key)}}})
	if err != nil {
		return nil, nil, errInternal()
	}
	return file, key, nil
}

func (k *Keys) initCreate(ctx context.Context, now time.Time, pub string, blob []byte) (contract.Reply, error) {
	rid := make([]byte, 16)
	if _, err := rand.Read(rid); err != nil {
		return contract.Reply{}, errInternal()
	}
	repoID := "rp-" + hex.EncodeToString(rid)
	mfile, mkey, err := newMaster()
	if err != nil {
		return contract.Reply{}, err
	}
	defer wipeBytes(mkey)
	defer wipeBytes(mfile)
	words, keyring, keyringID, err := newKeyring(k.Codec, repoID, mkey, now)
	if err != nil {
		return contract.Reply{}, err
	}
	repo, _ := json.Marshal(map[string]any{"format": "sitate.repo/v1", "repo_id": repoID, "keyring_id": keyringID, "created_at": fmtTime(now), "format_version": contract.FormatVersion})
	if err := savePending(k.Dir, pendingFiles{Op: "init", Keyring: keyring, Repo: repo, Mstr: mfile}); err != nil {
		return contract.Reply{}, errInternal()
	}
	kv, err := k.kitVersion(ctx, kitBase{RepoID: repoID, KeyringVersion: 1, FormatVersion: contract.FormatVersion})
	if err != nil {
		return contract.Reply{}, err
	}
	fp := fingerprint(blob)
	err = k.setItems(ctx, "initialized", true, "repo_id", repoID, "phrase_confirmed", false, "objects", []objRec{},
		"approval_key", map[string]string{"key_fp": fp, "key_type": "software", "public_key": approvalKeyType + " " + pub},
		"kit", map[string]any{"version": kv, "confirmed_version": nil, "location_checked_at": nil, "confirmed_core_version": nil})
	if err != nil {
		return contract.Reply{}, err
	}
	if err := k.registerTimers(ctx); err != nil {
		return contract.Reply{}, err
	}
	rec, _ := json.Marshal(map[string]any{"schema": "sitate.secret-string/v1", "kind": "recovery_phrase", "words": words, "keyring_version": 1})
	r, err := reply(map[string]any{"repo_id": repoID, "keyring_id": keyringID, "committed": false, "kit_version": kv, "approval_key_fp": fp})
	r.Phrase = contract.NewSecret(rec)
	wipeBytes(rec)
	return r, err
}

func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// registerTimers は定期実行の表(contract の timers/timers.json)の各 step を登録する(サブコマンドの語と周期)。Timers が無ければ何もしない。
func (k *Keys) registerTimers(ctx context.Context) error {
	if k.Timers == nil {
		return nil
	}
	tbl, err := contract.LoadTimerTable()
	if err != nil {
		return errInternal()
	}
	var units []contract.TimerUnit
	for _, u := range tbl.Units {
		for _, st := range u.Steps {
			units = append(units, contract.TimerUnit{Words: st.Args, Period: u.Trigger})
		}
	}
	if err := k.Timers.Register(ctx, units); err != nil {
		return err
	}
	return k.Timers.SetOuterLimit(ctx, contract.TimerSystemdTimeoutStart)
}
