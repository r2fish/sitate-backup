#!/usr/bin/env bash
# WP の試験用の保管先の起動と停止。
# 使い方: test-env.sh up|down <NAME> [KIND]     KIND は minio(既定)か fake。
# 割り当て:
#   - 接続先は同じ名前付きネットワーク(sitate-test)の中のコンテナ名 sitate-<NAME>-s3:9000(NAME ごとに別のコンテナ)。
#     ホストのポートは公開しない(複数インスタンスでポートが衝突しない)。開発用コンテナも同じネットワークに入る。
#   - バケット名: sitate-<name>-main(Object Lock 有効。LOCK_DEFAULT_DAYS=<n> を付けると既定保持 COMPLIANCE <n> 日も設定する)。接頭辞: sitate-<name>-(<name> は NAME の小文字化)
#   - 資格情報: 起動のたびに乱数で生成し test/env/.run/<NAME>/env(git 管理外)に書く。リポジトリには置かない。
#   - 同じ NAME の再起動: 先に同じ NAME のコンテナと .run/<NAME>/ を消してから新しく起動する(資格情報とバケットは新しくなる)。
#   - 模擬時間の共有の値: test/env/.run/<NAME>/simtime(JSON。形は internal/contract の SimTime)。fake は毎回読む。
set -eu
op="${1:?up|down}"; name="${2:?NAME}"; kind="${3:-minio}"
case "$name" in *[!A-Za-z0-9-]*|"") echo "NAME は英数字・ハイフンだけ" >&2; exit 64;; esac
root="$(cd "$(dirname "$0")/../../.." && pwd)"
run="$root/test/env/.run/$name"
network="${SITATE_TEST_NETWORK:-sitate-test}"
MINIO_IMAGE="cgr.dev/chainguard/minio@sha256:0a01100085e30fb08d2f39e39c9f5c9d83a95707f220b007d813b690cd41622d"
MC_IMAGE="cgr.dev/chainguard/minio-client@sha256:3b67177c0e12f44f646a5cf21b20fc5a79dd2d7a6b4db632b436f0b9e2e46717"
PG_IMAGE="${PG_IMAGE:-postgres@sha256:839fbb5a4942a7758dd93e18b42ea5649807491a0545b5eeeb8f0b8bb11857a5}" # PostgreSQL 17.11
s3="sitate-$name-s3"; pg="sitate-$name-pg"
down() {
  docker rm -f "$s3" "$pg" >/dev/null 2>&1 || true
  rm -rf "$run"
}
if [ "$op" = "down" ]; then down; echo "停止: $name"; exit 0; fi
[ "$op" = "up" ] || { echo "op: up|down" >&2; exit 64; }
down
docker network inspect "$network" >/dev/null 2>&1 || docker network create "$network" >/dev/null
mkdir -p "$run"; chmod 0777 "$run"
rnd() { LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c "$1"; }
ak="$(rnd 20)"; sk="$(rnd 40)"; pgpw="$(rnd 24)"
wak="$(rnd 20)"; wsk="$(rnd 40)"; rak="$(rnd 20)"; rsk="$(rnd 40)"
lname="$(printf %s "$name" | tr A-Z a-z)"   # バケット名は小文字だけ(S3 の規則)。NAME に大文字(U1b など)を使えるようにする
bucket="sitate-$lname-main"
now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
printf '{"format":"sitate.simtime/v1","sim_now":"%s","real_at":"%s"}\n' "$now" "$now" > "$run/simtime"
chmod 0666 "$run/simtime"
case "$kind" in
minio)
  docker run -d --name "$s3" --network "$network" --tmpfs /data:uid=65532,gid=65532 \
    -e MINIO_ROOT_USER="$ak" -e MINIO_ROOT_PASSWORD="$sk" "$MINIO_IMAGE" server /data >/dev/null
  ;;
fake)
  img="${DEV_IMAGE:-sitate-dev:local}"
  docker run -d --name "$s3" --network "$network" --user "$(id -u):$(id -g)" -v "$root:$root" -w "$root" \
    -v sitate-dev-cache:/cache -v "$run:/run/sitate" \
    -e SITATE_SIM_TIME_FILE=/run/sitate/simtime "$img" \
    go run ./internal/contract/fake/cmd/fakes3 -listen :9000 -access-key "$ak" -secret-key "$sk" -key "$wak:$wsk:write" -key "$rak:$rsk:read" ${ALLOW_UNVERSIONED_DELETE:+-allow-unversioned-delete} >/dev/null
  ;;
*) echo "KIND は minio か fake" >&2; exit 64;;
esac
# 起動待ち(同じネットワークの中から)
ready=0
for _ in $(seq 1 180); do
  if [ "$kind" = fake ]; then
    if docker logs "$s3" 2>&1 | grep -q 'fakes3 listening'; then ready=1; break; fi
  elif docker run --rm --network "$network" -e "MC_HOST_t=http://$ak:$sk@$s3:9000" "$MC_IMAGE" ls t >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
[ "$ready" = 1 ] || { echo "起動待ちが時間切れ" >&2; docker logs "$s3" 2>&1 | tail -5 >&2; exit 1; }
if [ "$kind" = minio ]; then
  mcr() { docker run --rm --network "$network" -v "$run:/pol:ro" -e "MC_HOST_t=http://$ak:$sk@$s3:9000" "$MC_IMAGE" "$@" >/dev/null; }
  mcr mb --with-lock "t/$bucket"
  # LOCK_DEFAULT_DAYS=<n>: バケットの既定保持(COMPLIANCE)を設定する(主経路の第 2 段。init-storage が既定保持 90 日を照合するため)。既定は設定しない。
  if [ -n "${LOCK_DEFAULT_DAYS:-}" ]; then mcr retention set --default COMPLIANCE "${LOCK_DEFAULT_DAYS}d" "t/$bucket"; fi
  # 書き込み鍵と読み取り鍵(spec 3.2.6 の操作の表の「許す」だけ。版指定なしの削除 s3:DeleteObject・設定の変更・保持の迂回・リーガルホールドは付けない)。
  b="arn:aws:s3:::$bucket"
  printf '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObject","s3:GetObject","s3:GetObjectVersion","s3:ListBucket","s3:ListBucketVersions","s3:GetBucketVersioning","s3:GetBucketObjectLockConfiguration","s3:GetObjectRetention","s3:PutObjectRetention","s3:DeleteObjectVersion"],"Resource":["%s","%s/*"]}]}\n' "$b" "$b" > "$run/policy-write.json"
  printf '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:GetObjectVersion","s3:ListBucket","s3:ListBucketVersions"],"Resource":["%s","%s/*"]}]}\n' "$b" "$b" > "$run/policy-read.json"
  mcr admin policy create t sitate-write /pol/policy-write.json
  mcr admin policy create t sitate-read /pol/policy-read.json
  mcr admin user add t "$wak" "$wsk"
  mcr admin user add t "$rak" "$rsk"
  mcr admin policy attach t sitate-write --user "$wak"
  mcr admin policy attach t sitate-read --user "$rak"
  rm -f "$run/policy-write.json" "$run/policy-read.json"
fi
# PostgreSQL は KIND に関わらず起動定義だけ持つ(U2a の試験が使う)。PG=1 のときだけ起動する。
if [ "${PG:-}" = "1" ]; then
  docker run -d --name "$pg" --network "$network" -e POSTGRES_PASSWORD="$pgpw" "$PG_IMAGE" >/dev/null
fi
cat > "$run/env" <<EOF
SITATE_TEST_S3_KIND=$kind
SITATE_TEST_S3_ENDPOINT=http://$s3:9000
SITATE_TEST_S3_REGION=us-east-1
SITATE_TEST_S3_ACCESS_KEY_ID=$ak
SITATE_TEST_S3_SECRET_ACCESS_KEY=$sk
SITATE_TEST_S3_BUCKET=$bucket
SITATE_TEST_S3_BUCKET_PREFIX=sitate-$lname-
SITATE_SIM_TIME_FILE=$run/simtime
SITATE_TEST_PG_HOST=$pg
SITATE_TEST_PG_PORT=5432
SITATE_TEST_PG_USER=postgres
SITATE_TEST_PG_PASSWORD=$pgpw
EOF
if true; then
  cat >> "$run/env" <<EOF
SITATE_TEST_S3_WRITE_ACCESS_KEY_ID=$wak
SITATE_TEST_S3_WRITE_SECRET_ACCESS_KEY=$wsk
SITATE_TEST_S3_READ_ACCESS_KEY_ID=$rak
SITATE_TEST_S3_READ_SECRET_ACCESS_KEY=$rsk
EOF
fi
chmod 0600 "$run/env"
echo "起動: $name ($kind) endpoint=http://$s3:9000 bucket=$bucket"
