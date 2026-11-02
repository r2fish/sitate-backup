#!/usr/bin/env bash
# 開発用コンテナの中でコマンドを 1 つ走らせる(非対話)。終了コードはコンテナの中のものをそのまま返す。
# 使い方: in-linux.sh '<コマンド>'      空なら対話のシェル(TTY があるとき)。
# 環境変数:
#   SITATE_IN_DEV=1     すでにコンテナの中。直接実行する。
#   NAME=<WP>           test/env/.run/<NAME>/env があれば、試験用の保管先の環境変数を渡し、同じネットワークに入れる。
#   SITATE_NESTED=1     入れ子の rootless 容器の試験用。ユーザー rootless(1000)で入り、SYS_ADMIN・seccomp/apparmor unconfined・
#                       systempaths unconfined・/dev/fuse・/dev/net/tun を足す(実測で要った最小の組。test/env/bin/accept-env.sh check)。既定は付けない。
#   ACCEPT=<NAME>       受入試験の環境(accept-env.sh up <NAME>)のネットワークにも入れ、その env ファイルを渡す。
set -u
cmd="${1:-}"
if [ "${SITATE_IN_DEV:-}" = "1" ]; then
  if [ -z "$cmd" ]; then exec bash; fi
  exec bash -c "$cmd"
fi
root="$(cd "$(dirname "$0")/../../.." && pwd)"
image="${DEV_IMAGE:-sitate-dev:local}"
network="${SITATE_TEST_NETWORK:-sitate-test}"
# Dockerfile が変わったら作り直す(ラベルに Dockerfile のハッシュを持つ)。
dsha="$(shasum -a 256 "$root/test/env/dev/Dockerfile" | cut -c1-16)"
have="$(docker image inspect -f '{{index .Config.Labels "sitate.dockerfile"}}' "$image" 2>/dev/null || true)"
if [ "$have" != "$dsha" ]; then
  docker build -q -t "$image" --label "sitate.dockerfile=$dsha" ${GO_IMAGE:+--build-arg GO_IMAGE="$GO_IMAGE"} "$root/test/env/dev" >/dev/null || exit 70
fi
docker network inspect "$network" >/dev/null 2>&1 || docker network create "$network" >/dev/null || exit 70
common="$(cd "$root" && git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)"
uid="$(id -u)"; gid="$(id -g)"; [ "${SITATE_NESTED:-}" = "1" ] && { uid=1000; gid=1000; }
# passwd に無い uid では ssh が動かない("No user exists")ので、root で入って passwd に 1 行足し、setpriv で uid を落とす。
wrap='getent passwd "$SITATE_UID" >/dev/null || echo "dev:x:$SITATE_UID:$SITATE_GID::${HOME:-/cache/home}:/bin/bash" >> /etc/passwd; exec setpriv --reuid="$SITATE_UID" --regid="$SITATE_GID" --clear-groups -- bash -c "${SITATE_CMD:-exec bash}"'
args=(--user 0:0 -e "SITATE_UID=$uid" -e "SITATE_GID=$gid" -v "$root:$root" -w "$root" -v sitate-dev-cache:/cache --network "$network" -e SITATE_IN_DEV=1 -e GIT_CONFIG_COUNT=1 -e GIT_CONFIG_KEY_0=safe.directory -e GIT_CONFIG_VALUE_0='*')
if [ -n "$common" ] && [ "$common" != "$root/.git" ]; then args+=(-v "$common:$common"); fi
if [ -n "${NAME:-}" ] && [ -f "$root/test/env/.run/$NAME/env" ]; then args+=(--env-file "$root/test/env/.run/$NAME/env"); fi
if [ "${SITATE_NESTED:-}" = "1" ]; then
  args+=(--cap-add SYS_ADMIN --security-opt seccomp=unconfined --security-opt apparmor=unconfined --security-opt systempaths=unconfined --device /dev/fuse --device /dev/net/tun -e HOME=/home/rootless)
fi
accnet=""
if [ -n "${ACCEPT:-}" ]; then
  [ -f "$root/test/env/.run/accept-$ACCEPT/env" ] || { echo "受入試験の環境が起動していません: accept-env.sh up $ACCEPT" >&2; exit 64; }
  args+=(--env-file "$root/test/env/.run/accept-$ACCEPT/env"); accnet="sitate-acc-$ACCEPT"
fi
if [ -z "$cmd" ]; then
  if [ -t 0 ]; then exec docker run --rm -it "${args[@]}" "$image" bash -c "$wrap"; fi
  echo "TTY が無いので CMD= を付けてください" >&2; exit 64
fi
args+=(-e "SITATE_CMD=$cmd")
if [ -z "$accnet" ]; then exec docker run --rm "${args[@]}" "$image" bash -c "$wrap"; fi
# 2 つ目のネットワークには作ってから接続する(docker run は --network を 1 つしか取れない)。
cid="$(docker create "${args[@]}" "$image" bash -c "$wrap")" || exit 70
trap 'docker rm -f "$cid" >/dev/null 2>&1 || true' EXIT
docker network connect "$accnet" "$cid" || exit 70
docker start -a "$cid"
