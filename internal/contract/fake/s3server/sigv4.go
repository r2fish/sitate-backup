// Package s3server は偽の保管先を S3 互換のサーバーとして出す(署名 v4 の検証、パス形式のアドレス)。
// 実装する操作の範囲は作業指示書 5.1(GB-99)。範囲の外は S3 の形のエラー(NotImplemented)。
package s3server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Key は検証に使う鍵(役割つき)。
type Key struct {
	Secret string
	Role   string
}

func hm(key []byte, s string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(s))
	return h.Sum(nil)
}

func awsEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func canonicalQuery(raw string) string {
	var parts []string
	for _, kv := range strings.Split(raw, "&") {
		if kv == "" {
			continue
		}
		k, v, _ := strings.Cut(kv, "=")
		uk, _ := url.QueryUnescape(k)
		uv, _ := url.QueryUnescape(v)
		parts = append(parts, awsEscape(uk)+"="+awsEscape(uv))
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

// verify は署名 v4 を検証し、アクセスキーを返す。
func verify(r *http.Request, keys map[string]Key) (string, bool) {
	auth := r.Header.Get("Authorization")
	const pfx = "AWS4-HMAC-SHA256 "
	if !strings.HasPrefix(auth, pfx) {
		return "", false
	}
	f := map[string]string{}
	for _, p := range strings.Split(auth[len(pfx):], ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(p), "=")
		f[k] = v
	}
	cred := strings.Split(f["Credential"], "/")
	if len(cred) != 5 {
		return "", false
	}
	k, ok := keys[cred[0]]
	if !ok {
		return "", false
	}
	signed := strings.Split(f["SignedHeaders"], ";")
	var ch strings.Builder
	for _, h := range signed {
		v := r.Header.Get(h)
		if h == "host" {
			v = r.Host
		}
		ch.WriteString(h + ":" + strings.Join(strings.Fields(v), " ") + "\n")
	}
	path := r.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	creq := strings.Join([]string{r.Method, path, canonicalQuery(r.URL.RawQuery), ch.String(), f["SignedHeaders"], r.Header.Get("x-amz-content-sha256")}, "\n")
	h := sha256.Sum256([]byte(creq))
	scope := strings.Join(cred[1:], "/")
	sts := "AWS4-HMAC-SHA256\n" + r.Header.Get("x-amz-date") + "\n" + scope + "\n" + hex.EncodeToString(h[:])
	sk := hm(hm(hm(hm([]byte("AWS4"+k.Secret), cred[1]), cred[2]), cred[3]), cred[4])
	want := hex.EncodeToString(hm(sk, sts))
	return cred[0], hmac.Equal([]byte(want), []byte(f["Signature"]))
}
