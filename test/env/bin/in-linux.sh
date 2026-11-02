#!/usr/bin/env bash
# 開発用コンテナの中でコマンドを 1 つ走らせる(非対話)。終了コードはコンテナの中のものをそのまま返す。
# 使い方: in-linux.sh '<コマンド>'      空なら対話のシェル(TTY があるとき)。
# 環境変数:
#   SITATE_IN_DEV=1     すでにコンテナの中。直接実行する。
#   NAME=<WP>           test/env/.run/<NAME>/env があれば、試験用の保管先の環境変数を渡し、同じネットワークに入れる。
#   SITATE_NESTED=1     入れ子の rootless 容器の試験用(seccomp=unconfined。既定は付けない)。
set -u
cmd="${1:-}"
if [ "${SITATE_IN_DEV:-}" = "1" ]; then
  if [ -z "$cmd" ]; then exec bash; fi
  exec bash -c "$cmd"
fi
root="$(cd "$(dirname "$0")/../../.." && pwd)"
image="${DEV_IMAGE:-sitate-dev:local}"
network="${SITATE_TEST_NETWORK:-sitate-test}"
if ! docker image inspect "$image" >/dev/null 2>&1; then
  docker build -q -t "$image" ${GO_IMAGE:+--build-arg GO_IMAGE="$GO_IMAGE"} "$root/test/env/dev" >/dev/null || exit 70
fi
docker network inspect "$network" >/dev/null 2>&1 || docker network create "$network" >/dev/null || exit 70
common="$(cd "$root" && git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)"
args=(--rm --user "$(id -u):$(id -g)" -v "$root:$root" -w "$root" -v sitate-dev-cache:/cache --network "$network" -e SITATE_IN_DEV=1 -e GIT_CONFIG_COUNT=1 -e GIT_CONFIG_KEY_0=safe.directory -e GIT_CONFIG_VALUE_0='*')
if [ -n "$common" ] && [ "$common" != "$root/.git" ]; then args+=(-v "$common:$common"); fi
if [ -n "${NAME:-}" ] && [ -f "$root/test/env/.run/$NAME/env" ]; then args+=(--env-file "$root/test/env/.run/$NAME/env"); fi
if [ "${SITATE_NESTED:-}" = "1" ]; then args+=(--security-opt seccomp=unconfined --device /dev/fuse); fi
if [ -z "$cmd" ]; then
  if [ -t 0 ]; then exec docker run -it "${args[@]}" "$image" bash; fi
  echo "TTY が無いので CMD= を付けてください" >&2; exit 64
fi
exec docker run "${args[@]}" "$image" bash -c "$cmd"
