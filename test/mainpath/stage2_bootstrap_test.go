//go:build mainpath

package mainpath

// 第 2 段の導入(bootstrap の段 (4)〜(7) を app/src/main/bootstrap/bootstrap.ts と同じ順・同じ値で置く)。
// アプリの代わりに台本が、管理アクセス(管理用アカウントの SSH と sudo)で行う。署名の検証は核が実行時に行う。

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// bootstrap.ts の remote.ts の定数と同じ値。
const (
	sitateUser  = "sitate"
	sitateBin   = "/usr/local/bin/sitate"
	stateDir    = "/var/lib/sitate"
	libexecDir  = "/usr/libexec/sitate"
	testTrustFn = "/etc/sitate-test/trust.pub"
)

func (h *host) arch() string {
	switch strings.TrimSpace(h.root("uname -m")) {
	case "aarch64", "arm64":
		return "linux-arm64"
	}
	return "linux-x86_64"
}

// buildAndSign は試験ビルドの配布物(make build-test と make sign-test と同じ工程)を作り、版ディレクトリを返す。
func buildAndSign(t *testing.T) string {
	t.Helper()
	for _, a := range [][]string{{"tools/build.sh", "test", "0.1.0-dev", "sitate_testbuild"}, {"tools/sign.sh", "test", "0.1.0-dev"}} {
		c := exec.Command("bash", a...)
		c.Dir = repoRoot
		if b, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", a, err, b)
		}
	}
	return filepath.Join(repoRoot, "dist/test/bundle/0.1.0-dev")
}

func readF(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// walkBundle は版ディレクトリの prefix の下のファイル(署名を除く)の相対パスを返す。
func walkBundle(t *testing.T, dir, prefix string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(filepath.Join(dir, prefix), func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && !strings.HasSuffix(p, ".sig") {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	return out
}

// placeBundle は段 (4): 核・アダプタ・価格表を、署名ファイルごと置く(署名は付け直さない)。
// bootstrap.ts は動いているホストの <os>-<arch> の分だけ置くが、phrase verify(keys.binFiles)は保管先の bin/ へ写すために
// 版ディレクトリの両方の arch・ソースのアーカイブ・SBOM を要求する。台本は版ディレクトリの全体を置く(契約の不足として報告)。
func (h *host) placeBundle(t *testing.T, dir string) {
	arch := h.arch()
	vdir := libexecDir + "/bundle/0.1.0-dev"
	core := readF(t, filepath.Join(dir, arch, "sitate"))
	h.put(sitateBin, core, "0755", "root")
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		mode := "0644"
		if strings.HasSuffix(rel, "/sitate") || strings.Contains(rel, "/adapters/") && !strings.HasSuffix(rel, ".json") && !strings.HasSuffix(rel, ".sig") {
			mode = "0755"
		}
		b := readF(t, p)
		h.put(vdir+"/"+rel, b, mode, "root")
		if strings.HasPrefix(rel, arch+"/adapters/") {
			h.put(libexecDir+"/"+strings.TrimPrefix(rel, arch+"/"), b, mode, "root")
		}
		return nil
	})
}

// prerequisites は段 (5)(bootstrap.ts の stepsList と同じコマンド)。試験ビルドの信頼の起点のファイルと環境変数を先に置く(linger の前)。
func (h *host) prerequisites(t *testing.T, packages bool) {
	pub := readF(t, "../../test/env/.run/keys/sitate-test.pub")
	h.put(testTrustFn, pub, "0644", "root")
	h.root(`grep -q '^SITATE_TEST_TRUST_PUBKEY_FILE=' /etc/environment || echo 'SITATE_TEST_TRUST_PUBKEY_FILE=` + testTrustFn + `' >> /etc/environment`)
	steps := []string{}
	if packages {
		steps = append(steps, "command -v podman >/dev/null 2>&1 || (apt-get update -y && DEBIAN_FRONTEND=noninteractive apt-get install -y podman uidmap slirp4netns)")
	}
	steps = append(steps,
		"id sitate >/dev/null 2>&1 || useradd --system --create-home --home-dir "+stateDir+" --shell /bin/bash sitate",
		"grep -q '^sitate:' /etc/subuid || usermod --add-subuids 200000-265535 --add-subgids 200000-265535 sitate",
		"install -d -m 0700 -o sitate -g sitate "+stateDir+" "+stateDir+"/run && for f in run.lock state.lock; do [ -e "+stateDir+"/$f ] || install -m 0600 -o sitate -g sitate /dev/null "+stateDir+"/$f; done",
		"loginctl enable-linger sitate")
	for _, s := range steps {
		h.root(s)
	}
}

// registerGateKey は段 (6): 管理用 SSH 鍵を作り、強制コマンド付きの行を authorized_keys に登録する。
func (h *host) registerGateKey(t *testing.T) ssh.Signer {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sg, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	line := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(sg.PublicKey().Marshal())
	h.root("install -d -m 0700 -o sitate -g sitate " + stateDir + "/.ssh")
	h.put(stateDir+"/.ssh/authorized_keys", []byte(`command="`+sitateBin+` gate",restrict `+line+" sitate-admin\n"), "0600", "sitate")
	return sg
}

// bootstrapBackup はバックアップサーバーに段 (4)〜(7) を行い、gate 経由の呼び出し口を返す。
func bootstrapBackup(t *testing.T, h *host, dir string) *gateCLI {
	t.Helper()
	h.prerequisites(t, true)
	h.placeBundle(t, dir)
	g := &gateCLI{h: h, key: h.registerGateKey(t), t: t}
	v := g.call("", "version").ok(t, "version")
	var r struct {
		Version string `json:"version"`
		Build   string `json:"build"`
		Fp      string `json:"signing_key_fp"`
	}
	_ = json.Unmarshal(v.Env.Result, &r)
	if r.Version == "" || r.Build != "test" || r.Fp == "" {
		t.Fatalf("version の結果が期待と違う: %s", v.Env.Result)
	}
	t.Logf("version=%s build=%s signing_key_fp=%s", r.Version, r.Build, r.Fp)
	return g
}

// provisionDouble は試験用の保管先準備アダプタ(test/mainpath/provminio)を、置き場の b2 の adapter.json と署名の差し替えとして置く。
// 実行ファイルの sha256 を入れた adapter.json に試験の鍵で署名する(署名・sha256・trusted comment の経路は同梱のアダプタと同じ)。
// 差し替えるのは置き場 /usr/libexec/sitate/adapters/provision/b2/ だけで、版ディレクトリの同梱の b2 はそのまま(bin/ へ写す対象)。
// secrets は adapter.json の secrets[](核は宣言に無い枠を落とす)。
func (h *host) provisionDouble(t *testing.T, secrets []string) {
	t.Helper()
	arch := h.arch()
	tmp := t.TempDir()
	exe := filepath.Join(tmp, "minio")
	c := exec.Command("go", "build", "-trimpath", "-tags", "mainpath", "-o", exe, "./test/mainpath/provminio")
	c.Dir = repoRoot
	c.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+map[string]string{"linux-arm64": "arm64", "linux-x86_64": "amd64"}[arch])
	if b, err := c.CombinedOutput(); err != nil {
		t.Fatalf("provminio のビルド: %v\n%s", err, b)
	}
	eb := readF(t, exe)
	decl, _ := json.Marshal(map[string]any{"kind": "provision", "name": "b2", "version": "0.1.0-dev", "contract": "sitate.provision/v1",
		"exe": "minio", "secrets": secrets, "network": true, "sha256": hexStr(sha256Of(eb))})
	dj := filepath.Join(tmp, "adapter.json")
	if err := os.WriteFile(dj, decl, 0o644); err != nil {
		t.Fatal(err)
	}
	sg := exec.Command("minisign", "-S", "-s", "../../test/env/.run/keys/sitate-test.key", "-m", dj, "-x", dj+".sig",
		"-t", "version=0.1.0-dev arch="+arch+" path="+arch+"/adapters/provision/b2/adapter.json")
	if b, err := sg.CombinedOutput(); err != nil {
		t.Fatalf("minisign: %v\n%s", err, b)
	}
	d := libexecDir + "/adapters/provision/b2/"
	h.put(d+"minio", eb, "0755", "root")
	h.put(d+"adapter.json", decl, "0644", "root")
	h.put(d+"adapter.json.sig", readF(t, dj+".sig"), "0644", "root")
}

func sha256Of(b []byte) []byte { s := sha256SumImpl(b); return s[:] }
