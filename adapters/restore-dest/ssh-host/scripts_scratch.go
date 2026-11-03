package main

// 使い捨て暗号化領域を作る台本(start)。

// scratchCreateScript は作業ディレクトリと暗号文・平文のディレクトリを作る。引数: $1 session_id。
// 既にあれば終了 3(同じ session_id の物を上書きしない)。作業ディレクトリを作った後の失敗は終了 5(作った資源を返す)。
// 標準出力: 作業ディレクトリの絶対パス。
const scratchCreateScript = shPrelude + `isid "$1" || exit 64
[ -n "${HOME:-}" ] || exit 64
umask 077
mkdir -p "$HOME/.sitate/run" || exit 1
w="$HOME/.sitate/run/$1"
mkdir "$w" 2>/dev/null || { if [ -e "$w" ] || [ -L "$w" ]; then exit 3; fi; exit 1; }
mkdir "$w/cipher" "$w/scratch" || exit 5
echo "$w"
`

// scratchInitScript は gocryptfs の暗号文ディレクトリを初期化する。標準入力: パスワード(1 行。どこにも書かない)。引数: $1 session_id。
const scratchInitScript = shPrelude + `isid "$1" || exit 64
w="$HOME/.sitate/run/$1"
gocryptfs -init -q -nosyslog -- "$w/cipher" >/dev/null 2>&1
`

// scratchMountScript は暗号文を平文のディレクトリにマウントし、マウントを確かめる。標準入力: パスワード。引数: $1 session_id。
const scratchMountScript = shPrelude + `isid "$1" || exit 64
w="$HOME/.sitate/run/$1"
gocryptfs -q -nosyslog -- "$w/cipher" "$w/scratch" >/dev/null 2>&1 || exit 4
i=0
while [ $i -lt 50 ]; do
  if mountpoint -q "$w/scratch"; then exit 0; fi
  sleep 0.1; i=$((i+1))
done
exit 4
`
