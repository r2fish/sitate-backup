#!/usr/bin/env bash
# 予行: LLM を使わず、保管先の管理用鍵だけで init-storage・bucket check・backup・extend・restore-test を実物の B2 に通す。
# 実行: make test-b2-preflight NAME=<名前>(偽の口の確かめ: make test-b2-preflight NAME=<名前> FAKE=1 = 試験用の MinIO と代役のアダプタ)。
# 値は環境変数 SITATE_ROLE_B2_ENDPOINT / _BUCKET / _ADMIN_KEY_ID / _ADMIN_KEY から受け(コマンドの行の前置きだけに置く)、記録にも標準出力にも出さない。
# バケット名は A2 の本番の回とは別にする(init-storage は空でないバケットに作り直さず、90 日は消せない)。何も削除しない。
# 前提: make build-test sign-test build-app-test、node app/node_modules/electron/install.js、bash test/env/bin/accept-env.sh up <名前>(偽の口は LOCK_DEFAULT_DAYS=90 make test-env-up NAME=<名前> も)。
# 偽の LLM の口でアプリを通すので、Anthropic へは何も送らない。記録: test/records/b2-preflight/(OUT で変える)。
set -euo pipefail
name="${1:?NAME}"
fake="${2:-}"
cd "$(dirname "$0")/../.."
out="${OUT:-test/records/b2-preflight}"
if [ -z "$fake" ]; then
  for v in SITATE_ROLE_B2_ENDPOINT SITATE_ROLE_B2_BUCKET SITATE_ROLE_B2_ADMIN_KEY_ID SITATE_ROLE_B2_ADMIN_KEY; do
    [ -n "${!v:-}" ] || { echo "環境変数 $v が無い(値は前置きに置く。launch.md)" >&2; exit 64; }
  done
  case "$SITATE_ROLE_B2_ENDPOINT" in https://s3.*.backblazeb2.com) ;; *) echo "SITATE_ROLE_B2_ENDPOINT は https://s3.<リージョン>.backblazeb2.com の形" >&2; exit 64;; esac
fi
if [ -s "$out/record.jsonl" ]; then # 前の回の記録は日時つきの名前に退避する(要約に混ぜない。record.<日時>.jsonl は .gitignore の対象)
  mv "$out/record.jsonl" "$out/record.$(date +%Y%m%dT%H%M%S).jsonl"
fi
bash test/env/bin/accept-env.sh prepare-a2 "$name"
rm -f dist/test/wiring/need-doubles dist/test/wiring/doubles-done
watcher=""
trap '[ -z "$watcher" ] || kill $watcher 2>/dev/null || true' EXIT
if [ -n "$fake" ]; then
  NAME="$name" ACCEPT="$name" bash test/env/bin/in-linux.sh 'bash test/wiring/prep.sh'
  bash test/wiring/doubles-watch.sh "$name" &
  watcher=$!
  args=(--mode fake --doubles dist/test/wiring --preflight)
else
  args=(--mode fake --b2 --preflight)
fi
rc=0
node app/test/e2e/cli.ts "${args[@]}" --name "$name" --variant base --script test/acceptance/scripts/A2/script.json --out "$out" || rc=$?
echo "予行(偽の LLM)の終了コード: $rc"
node test/wiring/preflight-summary.ts "$out/record.jsonl" | tee "$out/preflight-summary.txt" || rc=1
exit "$rc"
