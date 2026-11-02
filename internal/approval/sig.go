package approval

// 承認鍵の署名の検証(spec 4.6)。署名の対象は核が記録から計算し直した canonical_hash で組む(提出された値ではない)。

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"

	"golang.org/x/crypto/ssh"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type approvalKey struct {
	KeyFP     string `json:"key_fp"`
	KeyType   string `json:"key_type"`
	PublicKey string `json:"public_key"`
}

// registeredKey は登録済みの承認鍵(state.json の approval_key)。登録が無ければ unknown_key。
func (a *Approval) registeredKey(ctx context.Context) (approvalKey, ed25519.PublicKey, error) {
	var k approvalKey
	ok, err := a.getItem(ctx, "approval_key", &k)
	if err != nil {
		return k, nil, err
	}
	if !ok {
		return k, nil, sigErr("unknown_key", nil)
	}
	if k.KeyType != "software" { // fido2 の検証はイテレーション 4 以降。検証できないものは通さない
		return k, nil, sigErr("bad_signature", nil)
	}
	pub, _, _, _, perr := ssh.ParseAuthorizedKey([]byte(k.PublicKey))
	if perr != nil || pub.Type() != ssh.KeyAlgoED25519 || ssh.FingerprintSHA256(pub) != k.KeyFP {
		return k, nil, sigErr("unknown_key", nil)
	}
	cp, ok := pub.(ssh.CryptoPublicKey)
	if !ok {
		return k, nil, sigErr("unknown_key", nil)
	}
	ed, ok := cp.CryptoPublicKey().(ed25519.PublicKey)
	if !ok {
		return k, nil, sigErr("unknown_key", nil)
	}
	return k, ed, nil
}

// verifySignature は sig(base64、64 バイトの Ed25519)が canonicalHash と signedAt で組んだ署名の対象に対する登録済みの承認鍵の署名であることを確かめる。
// 検証に通った鍵(指紋・種別)を返す。
func (a *Approval) verifySignature(ctx context.Context, canonicalHash, signedAt, sig string) (approvalKey, error) {
	k, pub, err := a.registeredKey(ctx)
	if err != nil {
		return k, err
	}
	if !sigB64Re.MatchString(sig) {
		return k, sigErr("bad_signature", nil)
	}
	raw, err := base64.StdEncoding.DecodeString(sig)
	if err != nil || len(raw) != ed25519.SignatureSize {
		return k, sigErr("bad_signature", nil)
	}
	if !ed25519.Verify(pub, signedText(canonicalHash, signedAt), raw) {
		return k, sigErr("bad_signature", nil)
	}
	return k, nil
}

// checkSkew は signed_at と受理時刻の差が SignedAtTolerance を超えれば SIGNATURE_INVALID(clock)。
func checkSkew(signedAt string, received string) error {
	s, err1 := parseTime(signedAt)
	r, err2 := parseTime(received)
	if err1 != nil || err2 != nil {
		return sigErr("clock", nil)
	}
	d := s.Sub(r)
	if d < 0 {
		d = -d
	}
	if d > contract.SignedAtTolerance {
		skew := int64(s.Sub(r).Seconds())
		return sigErr("clock", &skew)
	}
	return nil
}

// verifyStored は apply の開始前の照合(3.6 の apply の (a))。pending/ から読み直した記録の canonical_hash を計算し直し、
// 承認の canonical_hash と一致して署名がその値で検証でき、signed_at と received_at の差が許容内であることを確かめる。
func (a *Approval) verifyStored(ctx context.Context, raw []byte, rec changeRec, ap approvalRec) error {
	got, err := hashOfRaw(raw)
	if err != nil || got != rec.CanonicalHash || ap.CanonicalHash != got || ap.ChangeID != rec.ChangeID || len(ap.Approvers) != 1 {
		return sigErr("hash_mismatch", nil)
	}
	r := ap.Approvers[0]
	k, err := a.verifySignature(ctx, got, r.SignedAt, r.Sig)
	if err != nil {
		return err
	}
	if k.KeyFP != r.KeyFP || k.KeyType != r.KeyType {
		return sigErr("unknown_key", nil)
	}
	return checkSkew(r.SignedAt, r.ReceivedAt)
}
