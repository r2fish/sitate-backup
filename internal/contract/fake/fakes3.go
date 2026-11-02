package fake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Crypto は contract.Crypto の偽物。暗号は行わず、平文に印を付けるだけ(契約の形を試すためで、安全ではない)。
type Crypto struct{}

// AEADSeal は contract.Crypto を満たす。
func (Crypto) AEADSeal(p, _, _ []byte) ([]byte, error) { return append([]byte("fake:"), p...), nil }

// AEADOpen は contract.Crypto を満たす。
func (Crypto) AEADOpen(s, _, _ []byte) ([]byte, error) {
	if len(s) < 5 || string(s[:5]) != "fake:" {
		return nil, &contract.Error{Code: contract.CodeDecryptFailed, Class: "verification", Detail: json.RawMessage(`{}`), Next: []contract.Next{}}
	}
	return s[5:], nil
}

// HKDF は contract.Crypto を満たす。
func (Crypto) HKDF(ikm, info []byte, n int) ([]byte, error) {
	h := sha256.Sum256(append(append([]byte{}, ikm...), info...))
	return append([]byte(nil), h[:min(n, len(h))]...), nil
}

// Argon2id は contract.Crypto を満たす。
func (Crypto) Argon2id(phrase, salt []byte, _, _, _ int) ([]byte, error) {
	h := sha256.Sum256(append(append([]byte{}, phrase...), salt...))
	return h[:], nil
}

// BLAKE3 は contract.Crypto を満たす(偽物は SHA-256)。
func (Crypto) BLAKE3(c []byte) []byte { h := sha256.Sum256(c); return h[:] }

// SignProof は contract.Crypto を満たす。
// 偽の署名は Ed25519 と同じ 64 バイト(SHA-256 を 2 回並べたもの)。
func (Crypto) SignProof(r, _ []byte) ([]byte, error) { return fakeSig(r), nil }

func fakeSig(r []byte) []byte {
	h := sha256.Sum256(r)
	h2 := sha256.Sum256(h[:])
	return append(h[:], h2[:]...)
}

// VerifyProof は contract.Crypto を満たす。
func (Crypto) VerifyProof(r, sig, _ []byte) (bool, error) {
	return hex.EncodeToString(fakeSig(r)) == hex.EncodeToString(sig), nil
}

// Codec は contract.Codec の偽物(固定長のチャンク化。平文のまま包む)。
type Codec struct{ Size int }

// Chunk は contract.Codec を満たす。
func (c Codec) Chunk(ctx context.Context, r io.Reader, emit func(contract.Chunk) error) error {
	n := c.Size
	if n == 0 {
		n = contract.ChunkAvg
	}
	buf := make([]byte, n)
	for {
		k, err := io.ReadFull(r, buf)
		if k > 0 {
			if e := emit(contract.Chunk{Data: append([]byte(nil), buf[:k]...), Length: k}); e != nil {
				return e
			}
		}
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return err
		}
		if e := ctx.Err(); e != nil {
			return e
		}
	}
}

// Seal は contract.Codec を満たす。
func (Codec) Seal(p, _ []byte, _ string) ([]byte, error) { return append([]byte("fake:"), p...), nil }

// Open は contract.Codec を満たす。
func (Codec) Open(s, _ []byte, _ string) ([]byte, error) { return Crypto{}.AEADOpen(s, nil, nil) }

// Encode は contract.Codec を満たす。
func (Codec) Encode(_, _ string, _ contract.Header, c, _ []byte) ([]byte, error) {
	return append([]byte("fake:"), c...), nil
}

// Decode は contract.Codec を満たす。
func (Codec) Decode(_, _ string, s, _ []byte) ([]byte, error) { return Crypto{}.AEADOpen(s, nil, nil) }

// Header は contract.Codec を満たす。
func (Codec) Header([]byte) (contract.Header, error) {
	return contract.Header{FormatVersion: contract.FormatVersion}, nil
}

// AAD は contract.Codec を満たす。
func (Codec) AAD(kind, id string, _ contract.Header) ([]byte, error) {
	return []byte(kind + "/" + id), nil
}

// Hash は contract.Codec を満たす。
func (Codec) Hash(c []byte) string { h := sha256.Sum256(c); return hex.EncodeToString(h[:]) }

// HashStream は contract.Codec を満たす。
func (Codec) HashStream(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// KeyService は contract.KeyService の偽物(決定的な値を返す)。
type KeyService struct{}

// NewSnapshotKey は contract.KeyService を満たす。
func (KeyService) NewSnapshotKey(context.Context, string) ([]byte, []byte, string, error) {
	return []byte("snap-key"), []byte("wrapped"), "kid-1", nil
}

// OpenSnapshotKey は contract.KeyService を満たす。
func (KeyService) OpenSnapshotKey(context.Context, []byte, string, string) ([]byte, error) {
	return []byte("snap-key"), nil
}

// OpenKeyring は contract.KeyService を満たす。
func (KeyService) OpenKeyring(_ context.Context, p *contract.Secret, _ []byte) (bool, error) {
	return p != nil, nil
}

// IssueKeys は contract.KeyService を満たす(役割ごとに kid-<role>・secret-<role>)。
func (KeyService) IssueKeys(_ context.Context, req contract.IssueRequest) ([]contract.IssuedKey, []string, error) {
	var r []contract.IssuedKey
	for _, role := range req.Roles {
		r = append(r, contract.IssuedKey{Role: role, KeyID: "kid-" + role, Secret: contract.NewSecret([]byte("secret-" + role))})
	}
	return r, nil, nil
}

// OpenedKeyring は contract.KeyService を満たす(偽物は版 1・鍵 ID k1)。
func (KeyService) OpenedKeyring(context.Context, *contract.Secret, []byte) (contract.KeyringInfo, error) {
	return contract.KeyringInfo{Version: 1, KeyringID: "kr-0123456789abcdef0123456789abcdef", RepoID: "rp-0123456789abcdef0123456789abcdef", KeyIDs: []string{"k1"}}, nil
}

// RevokeKey は contract.KeyService を満たす。
func (KeyService) RevokeKey(_ context.Context, req contract.RevokeRequest) ([]string, []string, error) {
	return append([]string{}, req.KeyIDs...), []string{}, nil
}

// ChunkID は contract.KeyService を満たす。
func (KeyService) ChunkID(p []byte) string { return Codec{}.Hash(p) }

// EntryRef は contract.KeyService を満たす。
func (KeyService) EntryRef(path string) string { return Codec{}.Hash([]byte(path))[:16] }

// CandidateFingerprint は contract.KeyService を満たす。
func (KeyService) CandidateFingerprint(kind, path string) string {
	return Codec{}.Hash([]byte(kind + path))[:16]
}

// WrapChunkKey は contract.KeyService を満たす。
func (KeyService) WrapChunkKey(k []byte, _ string) ([]byte, error) {
	return append([]byte("w:"), k...), nil
}

// UnwrapChunkKey は contract.KeyService を満たす。
func (KeyService) UnwrapChunkKey(w []byte, _ string) ([]byte, error) { return w[2:], nil }

// DerivedKey は contract.KeyService を満たす。
func (KeyService) DerivedKey(purpose string) ([]byte, error) {
	h := sha256.Sum256([]byte("dk-" + purpose))
	return h[:], nil // 32 バイト(AEAD の鍵として使える)
}

func fakeReply(fn func() (contract.Reply, error)) (contract.Reply, error) {
	if fn != nil {
		return fn()
	}
	return rep()
}

// Acquire・Restore・Keys は部品のサブコマンドの入口の偽物。Fn[<名前>] に台本を置く。
type (
	// Acquire は contract.Acquire の偽物。
	Acquire struct {
		Fn map[string]func() (contract.Reply, error)
	}
	// Restore は contract.Restore の偽物。
	Restore struct {
		Fn map[string]func() (contract.Reply, error)
	}
	// Keys は contract.Keys の偽物。
	Keys struct {
		Fn map[string]func() (contract.Reply, error)
	}
)

func (a *Acquire) Selftest(context.Context) (contract.Reply, error) {
	return fakeReply(a.Fn["Selftest"])
}
func (a *Acquire) Run(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(a.Fn["Run"])
}
func (a *Acquire) SourceEnroll(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(a.Fn["SourceEnroll"])
}
func (a *Acquire) SourceCheck(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(a.Fn["SourceCheck"])
}
func (a *Acquire) SourceSurvey(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(a.Fn["SourceSurvey"])
}

func (r *Restore) List(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(r.Fn["List"])
}
func (r *Restore) RestoreTest(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(r.Fn["RestoreTest"])
}
func (r *Restore) Restore(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(r.Fn["Restore"])
}
func (r *Restore) RestoreDestEnroll(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(r.Fn["RestoreDestEnroll"])
}
func (r *Restore) RestoreDestCheck(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(r.Fn["RestoreDestCheck"])
}
func (r *Restore) RestoreVerify(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(r.Fn["RestoreVerify"])
}
func (r *Restore) JournalVerify(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(r.Fn["JournalVerify"])
}

func (k *Keys) Init(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(k.Fn["Init"])
}
func (k *Keys) InitStorage(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(k.Fn["InitStorage"])
}
func (k *Keys) BucketCheck(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(k.Fn["BucketCheck"])
}
func (k *Keys) PhraseVerify(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(k.Fn["PhraseVerify"])
}
func (k *Keys) StorageIssueReadKey(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(k.Fn["StorageIssueReadKey"])
}
func (k *Keys) StorageRevokeReadKey(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(k.Fn["StorageRevokeReadKey"])
}
func (k *Keys) Kit(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(k.Fn["Kit"])
}
func (k *Keys) KitConfirm(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(k.Fn["KitConfirm"])
}
func (k *Keys) Doctor(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(k.Fn["Doctor"])
}
func (k *Keys) SecretsSet(context.Context, contract.Call) (contract.Reply, error) {
	return fakeReply(k.Fn["SecretsSet"])
}

var (
	_ contract.Crypto     = Crypto{}
	_ contract.Codec      = Codec{}
	_ contract.KeyService = KeyService{}
	_ contract.Acquire    = (*Acquire)(nil)
	_ contract.Restore    = (*Restore)(nil)
	_ contract.Keys       = (*Keys)(nil)
)
