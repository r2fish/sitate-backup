package main

// リモートで走らせるシェルの断片。`bash -c <script> sitate <引数…>` で実行する(conn.run)。
// 破棄の対象は session_id(run_id の形)から決まる作業ディレクトリ ~/.sitate/run/<session_id>/ とそのタグの付いた物だけで、
// 要求の resources[] の id から対象を決めない。session_id が形に合わなければ何もせず終了 64。
// 台本は scripts.go(共通・確認・登録)、scripts_scratch.go(領域の作成)、scripts_session.go(破棄と列挙)に分ける。

// shPrelude は全スクリプトの先頭。
const shPrelude = `set -u
export LC_ALL=C
PATH="$PATH:/usr/sbin:/sbin"
isid() { case "$1" in r-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9]Z-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]) return 0;; esac; return 1; }
`

// probeScript は到達後の確認(enroll は管理用アカウントの root、start は専用ユーザー)。出力は 1 行 1 項目。
const probeScript = shPrelude + `chk() { n=$1; shift; if "$@" >/dev/null 2>&1; then echo "p $n ok"; else echo "p $n ng"; fi; }
echo "uid $(id -u)"
echo "user $(id -un)"
echo "home ${HOME:-}"
echo "groups $(id -nG | tr ' ' ',')"
chk podman command -v podman
chk gocryptfs command -v gocryptfs
chk fusermount3 command -v fusermount3
chk dev_fuse test -c /dev/fuse -a -r /dev/fuse -a -w /dev/fuse
chk nft command -v nft
case "$(uname -s)/$(uname -m)" in Linux/x86_64|Linux/aarch64|Linux/arm64) chk platform true;; *) chk platform false;; esac
chk subuid grep -q '^sitate-restore:' /etc/subuid
if [ "$(id -u)" != 0 ]; then
  s=none
  if sudo -n -l >/dev/null 2>&1; then s=yes; fi
  echo "sudo $s"
fi
echo "free_kb $(df -Pk "${HOME:-/}" 2>/dev/null | awk 'NR==2{print $4}')"
awk '/^nameserver/{print "ns " $2}' /etc/resolv.conf 2>/dev/null
exit 0
`

// enrollSetupScript は管理用アカウントの root で、専用ユーザー・鍵・/etc/hosts の固定・隔離の設定を置く。
// 引数: $1 authorized_keys の 1 行、$2 egress_host、$3 固定する IP アドレス(空白区切り)。標準入力: nft のルールセット。
// 専用ユーザーの所有する場所(.ssh・.sitate・authorized_keys)は専用ユーザーの権限で置く(root でシンボリックリンクを辿らない)。
// 隔離のユニットはネットワークの設定より前(sysinit.target の段)に動かす。登録の時点で起動して、動くことまで確かめる。
// 途中で失敗したら戻さず終了コード非 0(再登録が同じ状態に揃える)。
const enrollSetupScript = shPrelude + `set -e
u=sitate-restore
key=$1; host=$2; ips=$3
id "$u" >/dev/null 2>&1 || useradd -m -s /bin/bash "$u"
usermod -p '*' "$u"
h=$(getent passwd "$u" | cut -d: -f6)
[ -n "$h" ] && [ -d "$h" ] && [ ! -L "$h" ]
cd /
runuser -u "$u" -- bash -s "$h" "$key" <<'USEREOF'
set -e
cd /
umask 077
h=$1; key=$2
for d in "$h/.ssh" "$h/.sitate"; do if [ -L "$d" ]; then rm -f -- "$d"; fi; done
mkdir -p -m 0700 "$h/.ssh" "$h/.sitate"
if [ -L "$h/.sitate/run" ]; then rm -f -- "$h/.sitate/run"; fi
mkdir -p -m 0700 "$h/.sitate/run"
rm -f -- "$h/.ssh/authorized_keys.new"
( set -C; printf '%s\n' "$key" > "$h/.ssh/authorized_keys.new" )
chmod 0600 "$h/.ssh/authorized_keys.new"
mv -T -f -- "$h/.ssh/authorized_keys.new" "$h/.ssh/authorized_keys"
USEREOF
if [ -n "$host" ]; then
  tmp=$(mktemp)
  { grep -v '# sitate-egress$' /etc/hosts || true; for ip in $ips; do printf '%s %s # sitate-egress\n' "$ip" "$host"; done; } > "$tmp"
  cat "$tmp" > /etc/hosts; rm -f "$tmp"
fi
install -d -m 0755 /etc/sitate
cat > /etc/sitate/isolation.nft.new
chmod 0644 /etc/sitate/isolation.nft.new
nft -c -f /etc/sitate/isolation.nft.new
mv -f /etc/sitate/isolation.nft.new /etc/sitate/isolation.nft
nftbin=$(command -v nft)
systemctl disable sitate-isolation.service >/dev/null 2>&1 || true
cat > /etc/systemd/system/sitate-isolation.service <<EOF
[Unit]
Description=Sitate restore-dest isolation
DefaultDependencies=no
Wants=network-pre.target
Before=network-pre.target shutdown.target
Conflicts=shutdown.target
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=$nftbin -f /etc/sitate/isolation.nft
[Install]
WantedBy=sysinit.target
EOF
systemctl daemon-reload
systemctl enable sitate-isolation.service >/dev/null 2>&1
systemctl restart sitate-isolation.service
systemctl is-active --quiet sitate-isolation.service
`
