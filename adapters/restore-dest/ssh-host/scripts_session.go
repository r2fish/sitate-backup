package main

// session_id の物を破棄・列挙する台本(stop・inspect)。
// 確かめられないこと(podman か ps の失敗・無い、ID に見えない出力)は「無い」にせず、終了 3 で終える。

// shSession は session_id が指す物を引く関数(stop と inspect で共有)。base は専用ユーザーの ~/.sitate/run。
// needtools は ps と podman が無ければ終了 3。refresh は処理の一覧 PSL を取り直す(失敗は終了 3。$(…) の中で呼ばない)。
// cids <filter> は容器 ID の一覧 CIDS を取る(podman の失敗や ID に見えない出力は終了 3)。
// psid は処理が結ばれた session_id を、cwd・実行ファイル・開いたファイル・コマンド行・環境の中の作業ディレクトリのパス(相対も)から引く。
const shSession = `base="$HOME/.sitate/run"
me=$(id -u)
needtools() { command -v ps >/dev/null 2>&1 && command -v podman >/dev/null 2>&1 || exit 3; }
cidok() { case "$1" in ""|*[!0-9a-f]*) return 1;; esac; [ ${#1} -ge 12 ] && [ ${#1} -le 64 ]; }
cids() {
  local o l
  o=$(podman ps -a -q --filter "$1" 2>/dev/null) || exit 3
  CIDS=""
  while IFS= read -r l; do [ -n "$l" ] || continue; cidok "$l" || exit 3; CIDS="$CIDS $l"; done <<< "$o"
}
refresh() { PSL=$(ps -u "$me" -o pid=,ppid=,stat=,comm= 2>/dev/null) || exit 3; }
refresh0() {
  local anc p
  anc=" $$ "; p=$$
  while [ "$p" -gt 1 ]; do p=$(ps -o ppid= -p "$p" 2>/dev/null | tr -d ' '); [ -n "$p" ] || break; anc="$anc$p "; done
  ANC=$anc
}
psid() {
  local d=/proc/$1 s rest cand l
  { readlink "$d/cwd" 2>/dev/null; readlink "$d/exe" 2>/dev/null; for l in "$d"/fd/*; do readlink "$l" 2>/dev/null; done
    tr '\0' '\n' < "$d/cmdline" 2>/dev/null; tr '\0' '\n' < "$d/environ" 2>/dev/null; } | while IFS= read -r s; do
    case "$s" in *".sitate/run/r-"*) rest=${s#*".sitate/run/"}; cand=${rest:0:23}; if isid "$cand"; then echo "$cand"; break; fi;; esac
  done | head -1
}
mounts() { awk -v w="$1" '{p=$5} p==w||index(p,w"/")==1{print p}' /proc/self/mountinfo | sort -r; }
procs() { printf '%s\n' "$PSL" | awk -v me="$$" -v anc="$ANC" '{pid[NR]=$1; par[$1]=$2; st[$1]=$3; c=$0; sub(/^ *[0-9]+ +[0-9]+ +[^ ]+ +/,"",c); cm[$1]=c; n=NR}
  END{for(i=1;i<=n;i++){p=pid[i]; skip=0; q=p
    while(q!=""&&q>1){if(q==me){skip=1;break}; q=par[q]}
    if(index(anc," " p " ")>0) skip=1
    if(st[p] ~ /^Z/) skip=1
    c=cm[p]; if(c=="systemd"||c=="(sd-pam)"||c=="dbus-daemon"||c=="dbus-broker"||c=="dbus-broker-lau"||c=="catatonit"||c=="podman pause") skip=1
    if(!skip) print p}}'; }
`

// teardownScript は session_id の物を消し、残りを標準出力に出す(1 行 1 件: "rem <type> <id>")。引数: $1 session_id。
// 順: 容器を消す → 帰属した処理を止める(TERM の後に KILL) → 領域を外す(外せなければ領域を保つ処理も KILL して再度外す)
// → 作業ディレクトリ(リンクならリンクだけ)を消す → 残存を確かめる(spec 3.2.4 の「消去」)。
const teardownScript = shPrelude + shSession + `isid "$1" || exit 64
[ -n "${HOME:-}" ] || exit 64
needtools
refresh0
sid=$1
w="$base/$sid"
cids "label=sitate-run=$sid"
for c in $CIDS; do podman rm -f -t 3 "$c" >/dev/null 2>&1; done
killmatch() { # $1: シグナル、$2: 除く comm
  local p
  refresh
  for p in $(procs); do
    [ "$(psid "$p")" = "$sid" ] || continue
    [ "$(cat /proc/$p/comm 2>/dev/null)" != "$2" ] || continue
    kill -"$1" "$p" 2>/dev/null
  done
}
unmount_all() {
  local m i
  for m in $(mounts "$w"); do
    i=0; while [ $i -lt 5 ]; do fusermount3 -u "$m" >/dev/null 2>&1 && break; sleep 1; i=$((i+1)); done
  done
}
killmatch TERM gocryptfs
sleep 1
killmatch KILL gocryptfs
unmount_all
if [ -n "$(mounts "$w")" ]; then
  killmatch KILL ""
  sleep 0.5
  unmount_all
fi
killmatch KILL ""
if [ -L "$w" ]; then
  rm -f -- "$w"
elif [ -d "$w" ] && [ -z "$(mounts "$w")" ]; then
  rm -rf --one-file-system -- "$w"
fi
sleep 0.3
if [ -n "$(mounts "$w")" ]; then echo "rem scratch $sid"; fi
if [ -e "$w" ] || [ -L "$w" ]; then echo "rem workdir $sid"; fi
cids "label=sitate-run=$sid"
for c in $CIDS; do echo "rem container $c"; done
refresh
for p in $(procs); do
  o=$(psid "$p"); if [ -z "$o" ] || [ "$o" = "$sid" ]; then echo "rem process $p"; fi # 別の session_id の処理はその session の資源で、ここでは数えない
done
exit 0
`

// inspectScript は印の付いた資源を列挙する(1 行 1 件: "res <type> <id> <session_id>")。引数: $1 session_id(空なら全部)。
const inspectScript = shPrelude + shSession + `want=$1
if [ -n "$want" ]; then isid "$want" || exit 64; fi
[ -n "${HOME:-}" ] || exit 64
needtools
refresh0
for d in "$base"/r-*; do
  { [ -L "$d" ] || [ -d "$d" ]; } || continue
  s=${d##*/}; isid "$s" || continue
  [ -z "$want" ] || [ "$want" = "$s" ] || continue
  echo "res workdir $s $s"
  if [ ! -L "$d" ] && [ -n "$(mounts "$d")" ]; then echo "res scratch $s $s"; fi
done
o=$(podman ps -a --format '{{.ID}} {{index .Labels "sitate-run"}}' --filter label=sitate-run 2>/dev/null) || exit 3
while read -r id s; do
  [ -n "$id" ] || continue
  cidok "$id" || exit 3
  isid "$s" || continue
  [ -z "$want" ] || [ "$want" = "$s" ] || continue
  echo "res container $id $s"
done <<< "$o"
refresh
for p in $(procs); do
  s=$(psid "$p"); [ -n "$s" ] || continue
  [ -z "$want" ] || [ "$want" = "$s" ] || continue
  echo "res process $p $s"
done
exit 0
`
