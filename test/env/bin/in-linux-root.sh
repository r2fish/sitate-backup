#!/usr/bin/env bash
# root 前提の試験を、root・PostgreSQL 17 入りの容器(test/env/dev-root/Dockerfile)の中で流す。ネットワークなし。
# 使い方: in-linux-root.sh ['<go test の引数>']     既定は -count=1 -v ./adapters/acquire/recv/ ./adapters/acquire/files/
# worktree は読み取り専用で /w に載せ、/src へ複製して試験する(試験が作業ツリーを汚さない)。
# 環境変数: ROOT_IMAGE(既定 sitate-dev-root:local)、GO_IMAGE(x86_64 用)。
set -u
args="${1:--count=1 -v ./adapters/acquire/recv/ ./adapters/acquire/files/}"
root="$(cd "$(dirname "$0")/../../.." && pwd)"
image="${ROOT_IMAGE:-sitate-dev-root:local}"
dsha="$(shasum -a 256 "$root/test/env/dev-root/Dockerfile" | cut -c1-16)"
have="$(docker image inspect -f '{{index .Config.Labels "sitate.dockerfile"}}' "$image" 2>/dev/null || true)"
if [ "$have" != "$dsha" ]; then
  docker build -q -t "$image" --label "sitate.dockerfile=$dsha" ${GO_IMAGE:+--build-arg GO_IMAGE="$GO_IMAGE"} "$root/test/env/dev-root" >/dev/null || exit 70
fi
exec docker run --rm --user 0:0 --network none -v "$root:/w:ro" -v sitate-dev-cache:/cache \
  -e SITATE_IN_DEV=1 -e SITATE_DESTRUCTIVE_SETUP=1 -e "GO_TEST_ARGS=$args" "$image" bash -c '
set -e
mkdir -p /src; (cd /w && tar --exclude=.git --exclude=.claude --exclude=./app --exclude=./test/env/.run -cf - .) | tar -xf - -C /src
cd /src
set +e
go test $GO_TEST_ARGS'
