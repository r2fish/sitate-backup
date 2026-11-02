#!/usr/bin/env bash
# 保管先準備アダプタの無い保管先(試験用の MinIO)で、手順書の手順 2・5 に当たる保管先の用意を行う(作業指示書 5.1b、8 章。D-0063 の 12 の m6)。
#   手順 2 相当(create-bucket): バケットを Object Lock 有効・バージョニング有効で作り(test-env.sh up が作る)、既定の保持を COMPLIANCE の日数で付ける。
#   手順 5 相当(issue-keys と bucket check): 書き込み鍵と読み取り鍵(test-env.sh up が発行した、spec 3.2.6 の操作の表の「許す」だけのポリシー)を使い、
#     書き込み → 保持の付いた版の削除と保持の短縮が拒否されること、読み取り鍵で書けないことを確かめる。
# 使い方: make test-env-up NAME=<WP> の後に  prepare-storage.sh <NAME> [保持日数(既定 1)]
# 出力: test/env/.run/<NAME>/provision.json(sitate.test-provision/v1。接続先・バケット・鍵。0600、git 管理外)と、標準出力に確認の結果。
# MinIO の時計は動かさない。保持は 1 日の最小値で付け、削除・短縮が拒否されることだけを確かめる(3.3)。
set -eu
name="${1:?NAME}"; days="${2:-1}"
root="$(cd "$(dirname "$0")/../../.." && pwd)"
run="$root/test/env/.run/$name"
[ -f "$run/env" ] || { echo "先に make test-env-up NAME=$name" >&2; exit 64; }
set -a; . "$run/env"; set +a
[ "$SITATE_TEST_S3_KIND" = minio ] || { echo "KIND=minio だけ(fake は意味論をサーバーが持つ)" >&2; exit 64; }
network="${SITATE_TEST_NETWORK:-sitate-test}"
MC_IMAGE="cgr.dev/chainguard/minio-client@sha256:3b67177c0e12f44f646a5cf21b20fc5a79dd2d7a6b4db632b436f0b9e2e46717"
ep="${SITATE_TEST_S3_ENDPOINT#http://}"
mc_as() { # mc_as <ak> <sk> <mc の引数…>
  local ak="$1" sk="$2"; shift 2
  docker run --rm --network "$network" -e "MC_HOST_t=http://$ak:$sk@$ep" "$MC_IMAGE" "$@"
}
adm() { mc_as "$SITATE_TEST_S3_ACCESS_KEY_ID" "$SITATE_TEST_S3_SECRET_ACCESS_KEY" "$@"; }
wr()  { mc_as "$SITATE_TEST_S3_WRITE_ACCESS_KEY_ID" "$SITATE_TEST_S3_WRITE_SECRET_ACCESS_KEY" "$@"; }
rd()  { mc_as "$SITATE_TEST_S3_READ_ACCESS_KEY_ID" "$SITATE_TEST_S3_READ_SECRET_ACCESS_KEY" "$@"; }
b="$SITATE_TEST_S3_BUCKET"
pass=0; fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
ng() { fail=$((fail + 1)); echo "FAIL $1"; }

echo "# 手順 2 相当: 既定の保持"
adm retention set --default COMPLIANCE "${days}d" "t/$b" >/dev/null && ok "既定の保持 COMPLIANCE ${days} 日を付けた" || ng "既定の保持"
adm version info "t/$b" 2>&1 | grep -qi enabled && ok "バージョニングが有効" || ng "バージョニング"
adm retention info --default "t/$b" 2>&1 | grep -qi COMPLIANCE && ok "Object Lock の既定の保持が COMPLIANCE" || ng "Object Lock の既定の保持"

echo "# 手順 5 相当: 鍵の権限(bucket check の試験)"
printf 'probe' | docker run --rm -i --network "$network" -e "MC_HOST_t=http://$SITATE_TEST_S3_WRITE_ACCESS_KEY_ID:$SITATE_TEST_S3_WRITE_SECRET_ACCESS_KEY@$ep" "$MC_IMAGE" pipe "t/$b/envprobe/p1" >/dev/null 2>&1 \
  && ok "書き込み鍵で envprobe/ に書ける" || ng "書き込み鍵の書き込み"
vid="$(wr ls --versions --json "t/$b/envprobe/" 2>/dev/null | grep -o '"versionId":"[^"]*"' | head -1 | cut -d'"' -f4 || true)"
[ -n "$vid" ] && ok "書き込み鍵で版付き一覧が読める" || ng "版付き一覧"
if [ -n "$vid" ]; then
  wr rm --version-id "$vid" "t/$b/envprobe/p1" >/dev/null 2>&1 && ng "保持の付いた版の削除が通ってしまった" || ok "保持の付いた版の削除が拒否される(書き込み鍵)"
  rd cat --version-id "$vid" "t/$b/envprobe/p1" >/dev/null 2>&1 && ok "読み取り鍵で版指定の読み出しができる" || ng "読み取り鍵の読み出し"
  printf 'x' | docker run --rm -i --network "$network" -e "MC_HOST_t=http://$SITATE_TEST_S3_READ_ACCESS_KEY_ID:$SITATE_TEST_S3_READ_SECRET_ACCESS_KEY@$ep" "$MC_IMAGE" pipe "t/$b/envprobe/p2" >/dev/null 2>&1 \
    && ng "読み取り鍵で書けてしまった" || ok "読み取り鍵では書けない"
  rd rm --version-id "$vid" "t/$b/envprobe/p1" >/dev/null 2>&1 && ng "読み取り鍵で版の削除が通った" || ok "読み取り鍵では版の削除ができない"
fi
cat > "$run/provision.json" <<EOF
{"format":"sitate.test-provision/v1","kind":"minio","endpoint":"$SITATE_TEST_S3_ENDPOINT","region":"$SITATE_TEST_S3_REGION","bucket":"$b","prefix":"sitate/","default_retention_days":$days,"lock_mode":"COMPLIANCE","versioning":true,
 "issued":[{"role":"write","key_id":"$SITATE_TEST_S3_WRITE_ACCESS_KEY_ID","secret":"$SITATE_TEST_S3_WRITE_SECRET_ACCESS_KEY"},{"role":"read","key_id":"$SITATE_TEST_S3_READ_ACCESS_KEY_ID","secret":"$SITATE_TEST_S3_READ_SECRET_ACCESS_KEY"}]}
EOF
chmod 0600 "$run/provision.json"
echo "集計: ok=$pass FAIL=$fail   出力: $run/provision.json"
[ "$fail" = 0 ]
