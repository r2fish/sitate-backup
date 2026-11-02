package restore

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// restoreKey は核の復元先用の SSH 鍵(参照名 sitate-restore)を返す。無ければ作って置く(登録のとき)。Secrets が無ければ空の鍵。
func restoreKey(ctx context.Context, sec contract.Secrets, create bool) (contract.SSHKeySecret, error) {
	if sec == nil {
		return contract.SSHKeySecret{}, nil
	}
	raw, err := sec.Get(ctx, contract.RestoreKeyRef)
	if err == nil {
		var k contract.SSHKeySecret
		if json.Unmarshal(raw, &k) == nil && k.PublicKey != "" {
			return k, nil
		}
	}
	var ce *contract.Error
	if err != nil && !(errors.As(err, &ce) && ce.Code == contract.CodeSecretMissing) {
		return contract.SSHKeySecret{}, err
	}
	if !create {
		return contract.SSHKeySecret{}, newErr(contract.CodeSecretMissing, map[string]any{"ref": contract.RestoreKeyRef})
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return contract.SSHKeySecret{}, internalErr("")
	}
	blk, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return contract.SSHKeySecret{}, internalErr("")
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		return contract.SSHKeySecret{}, internalErr("")
	}
	k := contract.SSHKeySecret{Kind: contract.SecretKindSSHKey, PrivateKey: string(pem.EncodeToMemory(blk)),
		PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))), Fp: ssh.FingerprintSHA256(sp)}
	b, _ := json.Marshal(k)
	if err := sec.Put(ctx, contract.RestoreKeyRef, b); err != nil {
		return contract.SSHKeySecret{}, err
	}
	return k, nil
}
