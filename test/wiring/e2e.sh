#!/usr/bin/env bash
# 試験用ドライバの通しの試験(LLM の代役): 実物の Electron の試験ビルド × 実物の utilityProcess のエージェント × 実物の Tool Runner × 実物の核 × 受入試験の環境を、
# 偽の LLM の口(ドライバが立てるローカルの偽のサーバー)で手順 1〜13 まで通し、続けて A6 の注入文の道を通す。LLM API キーも B2 も使わない(保管先は試験用の MinIO と代役のアダプタ)。
# 実行: make test-app-e2e NAME=<名前>
# 前提: make build-test sign-test build-app-test、node app/node_modules/electron/install.js、bash test/env/bin/accept-env.sh up <名前>、LOCK_DEFAULT_DAYS=90 make test-env-up NAME=<名前>。
# 終わったら make test-env-down NAME=<名前> と bash test/env/bin/accept-env.sh down <名前>。記録は test/records/driver/ (OUT で変える)。
set -euo pipefail
name="${1:?NAME}"
cd "$(dirname "$0")/../.."
root="$PWD"
out="${OUT:-test/records/driver}"
NAME="$name" ACCEPT="$name" bash test/env/bin/in-linux.sh 'bash test/wiring/prep.sh'
# 試験ビルドの核の信頼の起点と sudo の環境(test/wiring/run.sh と同じ。試験の環境の準備で、アプリの bootstrap の仕事ではない)。
for r in backup dest; do
  c="sitate-acc-$name-$r"
  docker exec "$c" mkdir -p /etc/sitate-test
  docker exec -i "$c" sh -c 'cat > /etc/sitate-test/trust.pub && chmod 0644 /etc/sitate-test/trust.pub' < test/env/.run/keys/sitate-test.pub
  docker exec "$c" sh -c "grep -q '^SITATE_TEST_TRUST_PUBKEY_FILE=' /etc/environment || echo 'SITATE_TEST_TRUST_PUBKEY_FILE=/etc/sitate-test/trust.pub' >> /etc/environment"
done
docker exec "sitate-acc-$name-backup" sh -c 'printf "Defaults env_file=\"/etc/environment\"\n" > /etc/sudoers.d/91-sitate-test-env && chmod 0440 /etc/sudoers.d/91-sitate-test-env && visudo -c -q'
# bootstrap の後に、保管先準備アダプタの代役を root で置く(ドライバが dist/test/wiring/need-doubles を作ったら置いて doubles-done を作る)。
rm -f dist/test/wiring/need-doubles dist/test/wiring/doubles-done
(
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
) &
watcher=$!
trap 'kill $watcher 2>/dev/null || true' EXIT
rc=0
node app/test/e2e/cli.ts --mode fake --name "$name" --variant base --script test/acceptance/scripts/A2/script.json --doubles dist/test/wiring --out "$out/fake-a2" || rc=$?
echo "A2(偽の LLM)の終了コード: $rc"
[ "$rc" = 0 ] || exit "$rc"
rc=0
node app/test/e2e/cli.ts --mode fake --name "$name" --script test/acceptance/scripts/A6/injections.json --out "$out/fake-a6" || rc=$?
echo "A6(偽の LLM)の終了コード: $rc"
exit "$rc"
