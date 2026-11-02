package acquire

// pull 用鍵(sitate-pull。kind: ssh_key。spec 4.5)の生成と読み出し。

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type pullKey struct {
	Secret contract.SSHKeySecret
	Signer ssh.Signer
}

func newPullKey() (pullKey, error) {
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return pullKey{}, errInternal("")
	}
	blk, err := ssh.MarshalPrivateKey(k, "")
	if err != nil {
		return pullKey{}, errInternal("")
	}
	return pullFromPEM(string(pemEncode(blk)))
}

func pullFromPEM(pem string) (pullKey, error) {
	sg, err := ssh.ParsePrivateKey([]byte(pem))
	if err != nil {
		return pullKey{}, errInternal("")
	}
	pub := sg.PublicKey()
	return pullKey{Signer: sg, Secret: contract.SSHKeySecret{Kind: contract.SecretKindSSHKey, PrivateKey: pem,
		PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))), Fp: ssh.FingerprintSHA256(pub)}}, nil
}

// loadPullKey は Secrets の sitate-pull を読む。
func loadPullKey(ctx context.Context, sec contract.Secrets) (pullKey, error) {
	raw, err := sec.Get(ctx, contract.SourcePullKeyRef)
	if err != nil {
		return pullKey{}, err
	}
	var s contract.SSHKeySecret
	if json.Unmarshal(raw, &s) != nil || s.PrivateKey == "" {
		return pullKey{}, errInternal("")
	}
	return pullFromPEM(s.PrivateKey)
}

func (k pullKey) put(ctx context.Context, sec contract.Secrets) error {
	b, _ := json.Marshal(k.Secret)
	return sec.Put(ctx, contract.SourcePullKeyRef, b)
}

// pullConn は sitate-ro での pull 接続(ポートは 22 固定)。
func pullConn(host string, k pullKey, pin string) contract.SSHConn {
	return contract.SSHConn{Target: contract.SSHTarget{Host: trimHost(host), Port: pullPort, User: "sitate-ro"}, CredRef: contract.SourcePullKeyRef,
		Cred: contract.NewSecret([]byte(k.Secret.PrivateKey)), PinnedFP: pin}
}
