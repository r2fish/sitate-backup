#!/usr/bin/env bash
# 結線の試験(アプリの実物の部品 × 実物の核 × 受入試験の環境)を走らせる。make test-app-real NAME=<名前>。
# 前提: bash test/env/bin/accept-env.sh up <名前> と LOCK_DEFAULT_DAYS=90 make test-env-up NAME=<名前> と make build-test sign-test build-app-test(app-bundle)。
# 下ごしらえは開発用コンテナ(Go)、試験の本体は Node 24 のコンテナ(受入試験の環境と試験用の保管先の両方のネットワークに入れる)。
set -euo pipefail
name="${1:?NAME}"
cd "$(dirname "$0")/../.."
root="$PWD"
NAME="$name" ACCEPT="$name" bash test/env/bin/in-linux.sh 'bash test/wiring/prep.sh'
# 試験ビルドの核は信頼の起点を /etc/sitate-test/trust.pub(環境変数 SITATE_TEST_TRUST_PUBKEY_FILE)から読む。これは試験の環境の準備で、
# アプリの bootstrap の仕事ではない(台本 host.prerequisites と同じ)。バックアップサーバーと復元先に置く。
for r in backup dest; do
  c="sitate-acc-$name-$r"
  docker exec "$c" mkdir -p /etc/sitate-test
  docker exec -i "$c" sh -c 'cat > /etc/sitate-test/trust.pub && chmod 0644 /etc/sitate-test/trust.pub' < test/env/.run/keys/sitate-test.pub
  docker exec "$c" sh -c "grep -q '^SITATE_TEST_TRUST_PUBKEY_FILE=' /etc/environment || echo 'SITATE_TEST_TRUST_PUBKEY_FILE=/etc/sitate-test/trust.pub' >> /etc/environment"
done
# bootstrap は最後に管理アクセスを閉じ、同梱の b2 アダプタで libexec の b2 を上書きする。保管先準備アダプタの代役は、その後に root で置く
# (第 2・3 段の provisionDouble と同じ)。試験が dist/test/wiring/need-doubles を作ったら、ここで置いて doubles-done を作る。
rm -f dist/test/wiring/need-doubles dist/test/wiring/doubles-done
(
  until [ -f dist/test/wiring/need-doubles ]; do sleep 1; done
  c="sitate-acc-$name-backup"
  # 診断: bootstrap の直後の状態(段 (8) の記録が残ったか)。
  { docker exec "$c" sh -c 'ls -la /var/lib/sitate | head -20; grep -o "admin_access_[a-z_]*\"[^,}]*" /var/lib/sitate/state.json 2>&1; for e in "" "SITATE_SIM_TIME_FILE=/opt/sitate-test/shared/simtime SITATE_TEST_TRUST_PUBKEY_FILE=/etc/sitate-test/trust.pub"; do echo "-- attest --dry-run env=[$e]"; sudo -n -u sitate env $e /usr/local/bin/sitate attest --step admin_access_closed --dry-run 2>&1 | tail -1 | cut -c1-400; done' ; } > dist/test/wiring/post-bootstrap.txt 2>&1 || true
  for n in minio b2; do
    d="/usr/libexec/sitate/adapters/provision/$n"
    docker exec "$c" mkdir -p "$d"
    for f in "minio:$d/minio:755" "keys.json:$d/keys.json:644" "adapter-$n/adapter.json:$d/adapter.json:644" "adapter-$n/adapter.json.sig:$d/adapter.json.sig:644"; do
      src="dist/test/wiring/${f%%:*}"; rest="${f#*:}"; dest="${rest%%:*}"; mode="${rest##*:}"
      docker exec -i "$c" sh -c "cat > $dest && chmod $mode $dest && chown root:root $dest" < "$src"
    done
  done
  touch dist/test/wiring/doubles-done
) &
watcher=$!
cid="$(docker create --network sitate-test --env-file "$root/test/env/.run/accept-$name/env" --env-file "$root/test/env/.run/$name/env" -v "$root:$root" -w "$root/app" -e HOME=/root node:24-bookworm-slim npm run test:real)"
trap 'docker rm -f "$cid" >/dev/null 2>&1 || true; kill $watcher 2>/dev/null || true' EXIT
docker network connect "sitate-acc-$name" "$cid"
docker start -a "$cid"
