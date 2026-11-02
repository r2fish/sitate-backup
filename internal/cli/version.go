package cli

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Version は核の版(spec 3.1.2 の version)。ビルドで -ldflags "-X" により差し替えられる。
var Version = "0.1.0-dev"

// routeOwn は cli 自身が実装する入口。ほかの入口は routeParts か NOT_IMPLEMENTED(dispatch.go)。
var routeOwn = map[string]entry{
	"version": func(d *Deps, ctx context.Context, _ contract.Call) (contract.Reply, error) {
		return versionReply(ctx, d)
	},
}

type versionResult struct {
	Version        string         `json:"version"`
	FormatVersions map[string]any `json:"format_versions"`
	SigningKeyFP   string         `json:"signing_key_fp"`
	Build          string         `json:"build"`
	SimTime        *string        `json:"sim_time,omitempty"`
}

func versionReply(ctx context.Context, d *Deps) (contract.Reply, error) {
	fv := map[string]any{"write": contract.FormatVersion, "read": []int{contract.FormatVersion}}
	r := versionResult{Version: Version, FormatVersions: fv, SigningKeyFP: signingKeyFP(), Build: buildName}
	versionExtras(ctx, d, &r, fv) // 試験ビルドだけ: format_versions.test と sim_time
	b, _ := json.Marshal(r)
	return contract.Reply{Result: b}, nil
}

// signingKeyFP は信頼の起点(contract.TrustAnchors)の先頭の鍵の指紋(spec 4.5 の指紋の形: OpenSSH の blob の SHA-256)。
// 起点が無い(試験ビルドで使い捨ての鍵が未設定)ときは、全ゼロの鍵に対する指紋を返す(形を保つ。署名の検証には使わない)。
func signingKeyFP() string {
	key := make([]byte, 32)
	if as := contract.TrustAnchors(); len(as) > 0 {
		if raw, err := base64.StdEncoding.DecodeString(as[0]); err == nil && len(raw) == 42 {
			key = raw[10:] // 方式 2 バイト + 鍵 ID 8 バイトの後が公開鍵 32 バイト
		}
	}
	blob := sshString(nil, []byte("ssh-ed25519"))
	blob = sshString(blob, key)
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func sshString(dst, s []byte) []byte {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(s)))
	return append(append(dst, n[:]...), s...)
}
