package keys

// キーリング(spec 4.3「キーリングと repo.json の項目」「フレーズの入力」)の作成と、フレーズによる開封。

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	bip39 "github.com/tyler-smith/go-bip39"
	"golang.org/x/crypto/argon2"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Argon2id のパラメータの上限(キーリングの見出しは保管先から来る信頼できない値。巨大な要求でメモリを使い切らせない)。
const (
	keyringSaltLen = 16
	keyringIDLen   = 16
	keyringHdrLen  = 5 + keyringIDLen + 25
	phraseWords    = 24
)

// normalizePhrase は空白とタブで語に区切り、前後の空白と行末の CR を無視し、英字を小文字にそろえる。24 語・単語表・チェックサムを検査し、
// 外れれば Argon2id を計算せずに PHRASE_INVALID(reason: format)を返す。成功すれば BIP-39 のエントロピー(32 バイト)を返す。
func normalizePhrase(p *contract.Secret) (words []string, entropy []byte, err error) {
	if p == nil {
		return nil, nil, errPhraseInvalid("format")
	}
	words = strings.Fields(strings.ToLower(strings.ReplaceAll(string(p.Reveal()), "\r", " ")))
	if len(words) != phraseWords {
		return nil, nil, errPhraseInvalid("format")
	}
	joined := strings.Join(words, " ")
	if !bip39.IsMnemonicValid(joined) {
		return nil, nil, errPhraseInvalid("format")
	}
	entropy, err = bip39.EntropyFromMnemonic(joined)
	if err != nil || len(entropy) != 32 {
		return nil, nil, errPhraseInvalid("format")
	}
	return words, entropy, nil
}

type keyringBody struct {
	Format         string `json:"format"`
	RepoID         string `json:"repo_id"`
	KeyringID      string `json:"keyring_id"`
	KeyringVersion int    `json:"keyring_version"`
	CreatedAt      string `json:"created_at,omitempty"`
	CurrentKeyID   string `json:"current_key_id"`
	Keys           []struct {
		KeyID string `json:"key_id"`
	} `json:"keys"`
}

// openKeyring はキーリングの object を entropy で開き、本体を返す。開けなければ ok = false(認証付き復号の失敗)。
func (s *KeyService) openKeyring(entropy, obj []byte) (body keyringBody, ok bool, err error) {
	if s.Codec == nil || s.Crypto == nil {
		return body, false, errInternal()
	}
	if len(obj) < keyringHdrLen+contract.NonceSize+contract.TagSize || string(obj[:4]) != contract.MagicKeyring {
		return body, false, nil
	}
	mark := obj[5+keyringIDLen : keyringHdrLen]
	mem, iter, par := int(binary.BigEndian.Uint32(mark[0:4])), int(binary.BigEndian.Uint32(mark[4:8])), int(mark[8])
	if mem < 1 || mem > contract.Argon2idMemoryMaxKiB || iter < 1 || iter > contract.Argon2idIterationsMax || par < 1 || par > contract.Argon2idParallelismMax {
		return body, false, nil
	}
	kek, err := s.Crypto.Argon2id(entropy, mark[9:25], mem, iter, par)
	if err != nil {
		var fe *contract.FormatError
		if errors.As(err, &fe) && fe.Reason == "header" {
			return body, false, nil
		}
		return body, false, errInternal()
	}
	defer wipeBytes(kek)
	plain, err := s.Codec.Decode(contract.MagicKeyring, hex.EncodeToString(obj[5:5+keyringIDLen]), obj, kek)
	defer wipeBytes(plain)
	if err != nil {
		var fe *contract.FormatError
		if errors.As(err, &fe) && (fe.Reason == "tag" || fe.Reason == "header") {
			return body, false, nil
		}
		return body, false, err
	}
	if json.Unmarshal(plain, &body) != nil || body.Format != "sitate.keyring/v1" {
		return body, false, nil
	}
	return body, true, nil
}

// OpenKeyring はフレーズでキーリングを開けるか。形の外れたフレーズは PHRASE_INVALID、形が合って開けないときは (false, nil)。
func (s *KeyService) OpenKeyring(_ context.Context, phrase *contract.Secret, keyring []byte) (bool, error) {
	_, ent, err := normalizePhrase(phrase)
	if err != nil {
		return false, err
	}
	defer wipeBytes(ent)
	_, ok, err := s.openKeyring(ent, keyring)
	return ok, err
}

// OpenedKeyring は開けたキーリングの要約を返す。開けなければ PHRASE_INVALID(reason: no_match)。
func (s *KeyService) OpenedKeyring(_ context.Context, phrase *contract.Secret, keyring []byte) (contract.KeyringInfo, error) {
	_, ent, err := normalizePhrase(phrase)
	if err != nil {
		return contract.KeyringInfo{}, err
	}
	defer wipeBytes(ent)
	body, ok, err := s.openKeyring(ent, keyring)
	if err != nil {
		return contract.KeyringInfo{}, err
	}
	if !ok {
		return contract.KeyringInfo{}, errPhraseInvalid("no_match")
	}
	info := contract.KeyringInfo{Version: body.KeyringVersion, KeyringID: body.KeyringID, RepoID: body.RepoID, KeyIDs: []string{}}
	for _, k := range body.Keys {
		info.KeyIDs = append(info.KeyIDs, k.KeyID)
	}
	return info, nil
}

// newKeyring は新しいフレーズ(Argon2id は golang.org/x/crypto/argon2 を直接使う。init を呼ぶ Keys は contract.Crypto を受けないため)(24 語)とキーリングの object を作る(init。キーリングの版は 1)。マスター鍵 master は鍵 ID k1 として入れる。
func newKeyring(codec contract.Codec, repoID string, master []byte, now time.Time) (phrase []string, object []byte, keyringID string, err error) {
	entropy := make([]byte, 32)
	salt := make([]byte, keyringSaltLen)
	idRaw := make([]byte, keyringIDLen)
	defer wipeBytes(entropy)
	for _, b := range [][]byte{entropy, salt, idRaw} {
		if _, err = rand.Read(b); err != nil {
			return nil, nil, "", errInternal()
		}
	}
	mn, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return nil, nil, "", errInternal()
	}
	kek := argon2.IDKey(entropy, salt, contract.Argon2idIterations, contract.Argon2idMemoryKiB, contract.Argon2idParallelism, contract.KeySize)
	defer wipeBytes(kek)
	keyringID = "kr-" + hex.EncodeToString(idRaw)
	body, err := json.Marshal(map[string]any{
		"format": "sitate.keyring/v1", "repo_id": repoID, "keyring_id": keyringID, "keyring_version": 1, "created_at": fmtTime(now),
		"current_key_id": "k1", "keys": []map[string]string{{"key_id": "k1", "key": base64.StdEncoding.EncodeToString(master), "created_at": fmtTime(now)}},
	})
	defer wipeBytes(body)
	if err != nil {
		return nil, nil, "", errInternal()
	}
	mark := binary.BigEndian.AppendUint32(nil, contract.Argon2idMemoryKiB)
	mark = binary.BigEndian.AppendUint32(mark, contract.Argon2idIterations)
	mark = append(append(mark, contract.Argon2idParallelism), salt...)
	object, err = codec.Encode(contract.MagicKeyring, hex.EncodeToString(idRaw), contract.Header{Kind: contract.MagicKeyring, FormatVersion: contract.FormatVersion, Mark: mark}, body, kek)
	if err != nil {
		return nil, nil, "", errInternal()
	}
	return strings.Fields(mn), object, keyringID, nil
}
