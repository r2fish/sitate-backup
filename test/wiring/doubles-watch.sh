#!/usr/bin/env bash
# 保管先準備アダプタの代役を backup に置く見張り(test/wiring/e2e.sh と b2-preflight.sh の偽の口の回から背景で起動)。
# ドライバが dist/test/wiring/need-doubles を作ったら、バックアップサーバーの libexec に root で置いて doubles-done を作る。
set -euo pipefail
name="${1:?NAME}"
cd "$(dirname "$0")/../.."
until [ -f dist/test/wiring/need-doubles ]; do sleep 1; done
c="sitate-acc-$name-backup"
for n in minio b2; do
  d="/usr/libexec/sitate/adapters/provision/$n"
  docker exec "$c" mkdir -p "$d"
  for f in "minio:$d/minio:755" "keys.json:$d/keys.json:644" "adapter-$n/adapter.json:$d/adapter.json:644" "adapter-$n/adapter.json.sig:$d/adapter.json.sig:644"; do
    src="dist/test/wiring/${f%%:*}"; rest="${f#*:}"; dest="${rest%%:*}"; mode="${rest##*:}"
    docker exec -i "$c" sh -c "cat > $dest && chmod $mode $dest && chown root:root $dest" < "$src"
  done
done
touch dist/test/wiring/doubles-done
