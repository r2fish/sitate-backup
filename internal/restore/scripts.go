package restore

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

var (
	safeRel  = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
	safeSID  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	safeHost = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,253}$`)
)

// runDir は復元先の作業ディレクトリ(ホームからの相対。spec 3.5 の表の 6)。
func runDir(sessionID string) string { return ".sitate/run/" + sessionID }

// probeScript は復元先で流す隔離の検査の台本(spec 3.5 の表の 5)。標準出力に DestProbeOut の JSON を 1 つ出す。
// 検査の道具(ss・timeout・bash)が無いときは「確かめられなかった」ので不合格にする(欠けを合格にしない)。
//   - inbound: 待ち受けが SSH(dest.at のポート)とループバックだけであること(ファイアウォールの規則は専用ユーザーが読めないため、振る舞いで見る)。
//   - outbound: 許可外の宛先(公開 DNS リゾルバーのアドレス)への接続を実際に試して、通らないこと。終了状態 0 は通った(不合格)、126・127 は道具の欠け(不合格)、
//     それ以外(接続の拒否・到達不能・時限 124)は通らなかった(合格)。
//   - dns_name: 復元先のホスト名・/etc/hosts に、元サーバーの名前(forbid)が無いこと。
func probeScript(forbid []string, sshPort int) []byte {
	var fb []string
	for _, n := range forbid {
		if safeHost.MatchString(n) {
			fb = append(fb, n)
		}
	}
	return []byte(fmt.Sprintf(`set -u
forbid=%q
port=%d
os=$(uname -s); arch=$(uname -m)
have() { command -v "$1" >/dev/null 2>&1; }
inbound=false
if have ss; then
  if out=$(ss -ltn 2>/dev/null); then
    bad=$(printf '%%s\n' "$out" | awk 'NR>1{print $4}' | grep -v -e '^127\.' -e '^\[::1\]' -e '^::1' -e ":$port\$" || true)
    if [ -z "$bad" ]; then inbound=true; fi
  fi
fi
outbound=false
if have timeout && have bash; then
  outbound=true
  for ip in 1.1.1.1 9.9.9.9; do
    timeout 3 bash -c 'exec 3<>/dev/tcp/$0/443' "$ip" >/dev/null 2>&1
    rc=$?
    case "$rc" in 0|126|127) outbound=false;; esac
  done
fi
dns_name=true
names="$(hostname -f 2>/dev/null) $(hostname 2>/dev/null) $(cat /etc/hosts 2>/dev/null)"
for n in $forbid; do
  case "$names" in *"$n"*) dns_name=false;; esac
done
printf '{"isolation":[{"name":"inbound","ok":%%s},{"name":"outbound","ok":%%s},{"name":"dns_name","ok":%%s}],"os":"%%s","arch":"%%s"}' "$inbound" "$outbound" "$dns_name" "$os" "$arch"
`, strings.Join(fb, " "), sshPort))
}

// placeScript は核とチェック一式を作業ディレクトリの下へ置き、復元先で計算した SHA-256 を DestPlaceOut の JSON で出す台本(spec 3.5 の表の 6)。
// 置く物は標準入力でなく台本の本体に base64 で埋める(SSH の台本は標準入力を使わない)。パスと session_id は安全な文字だけを許す。
func placeScript(sessionID string, files []contract.BundleFile) ([]byte, error) {
	if !safeSID.MatchString(sessionID) {
		return nil, internalErr("contract")
	}
	var b strings.Builder
	d := runDir(sessionID)
	fmt.Fprintf(&b, "set -eu\numask 077\nd=%s\nmkdir -p \"$d\"\n", d)
	for i, f := range files {
		if !safeRel.MatchString(f.Path) || strings.Contains(f.Path, "..") {
			return nil, internalErr("contract")
		}
		dir := ""
		if j := strings.LastIndex(f.Path, "/"); j >= 0 {
			dir = f.Path[:j]
		}
		fmt.Fprintf(&b, "mkdir -p \"$d/%s\"\nbase64 -d > \"$d/%s\" <<'SITATE_EOF_%d'\n", dir, f.Path, i)
		enc := base64.StdEncoding.EncodeToString(f.Data)
		for len(enc) > 76 {
			b.WriteString(enc[:76] + "\n")
			enc = enc[76:]
		}
		fmt.Fprintf(&b, "%s\nSITATE_EOF_%d\nchmod 0700 \"$d/%s\"\n", enc, i, f.Path)
	}
	b.WriteString("printf '{\"files\":['\nsep=''\n")
	for _, f := range files {
		fmt.Fprintf(&b, "h=$(sha256sum \"$d/%s\" | cut -d' ' -f1)\nprintf '%%s{\"path\":\"%s\",\"sha256\":\"sha256:%%s\"}' \"$sep\" \"$h\"; sep=,\n", f.Path, f.Path)
	}
	b.WriteString("printf ']}'\n")
	return []byte(b.String()), nil
}
