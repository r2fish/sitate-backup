#!/usr/bin/env bash
# 結線の試験(app/test/real)の下ごしらえ。開発用コンテナの中で走る(make test-app-real)。
# 保管先準備アダプタの代役(test/mainpath/provminio。MinIO に対して動く)を、バックアップサーバーの libexec へ置く形で dist/test/wiring/ に用意する。
# 第 2・3 段の台本 provisionDouble(test/mainpath/stage2_bootstrap_test.go)と同じ中身。アプリの bootstrap が配備した後に、試験が SSH で置く。
set -euo pipefail
cd "$(dirname "$0")/../.."
out=dist/test/wiring; rm -rf "$out"; mkdir -p "$out"
case "$(uname -m)" in aarch64|arm64) goarch=arm64; arch=linux-arm64;; *) goarch=amd64; arch=linux-x86_64;; esac
CGO_ENABLED=0 GOOS=linux GOARCH=$goarch go build -trimpath -tags mainpath -o "$out/minio" ./test/mainpath/provminio
sha="$(sha256sum "$out/minio" | cut -d' ' -f1)"
secrets="$(jq -c .secrets adapters/provision/b2/adapter.json)"
jq -n --arg wk "$SITATE_TEST_S3_WRITE_ACCESS_KEY_ID" --arg ws "$SITATE_TEST_S3_WRITE_SECRET_ACCESS_KEY" --arg rk "$SITATE_TEST_S3_READ_ACCESS_KEY_ID" --arg rs "$SITATE_TEST_S3_READ_SECRET_ACCESS_KEY" \
  '{default_retention_days: 90, keys: {write: {key_id: $wk, secret: $ws}, read: {key_id: $rk, secret: $rs}}}' > "$out/keys.json"
for name in minio b2; do
  mkdir -p "$out/adapter-$name"
  jq -n --arg n "$name" --arg s "$sha" --argjson sec "$secrets" '{kind: "provision", name: $n, version: "0.1.0-dev", contract: "sitate.provision/v1", exe: "minio", secrets: $sec, network: true, sha256: $s}' > "$out/adapter-$name/adapter.json"
  minisign -S -s test/env/.run/keys/sitate-test.key -m "$out/adapter-$name/adapter.json" -x "$out/adapter-$name/adapter.json.sig" -t "version=0.1.0-dev arch=$arch path=$arch/adapters/provision/$name/adapter.json" </dev/null >/dev/null
done
echo "wiring prep: $out ($arch)"
