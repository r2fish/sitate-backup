#!/usr/bin/env bash
# 試験用ドライバの通しの試験(LLM の代役): 実物の Electron の試験ビルド × 実物の utilityProcess のエージェント × 実物の Tool Runner × 実物の核 × 受入試験の環境を、
# 偽の LLM の口(ドライバが立てるローカルの偽のサーバー)で手順 1〜13 まで通し、続けて A6 の注入文の道を通す。LLM API キーも B2 も使わない(保管先は試験用の MinIO と代役のアダプタ)。
# 実行: make test-app-e2e NAME=<名前>
# 前提: make build-test sign-test build-app-test、node app/node_modules/electron/install.js、bash test/env/bin/accept-env.sh up <名前> --variant <変種>、LOCK_DEFAULT_DAYS=90 make test-env-up NAME=<名前>。
# 変種: 環境変数 VARIANT(既定 base。base|lo|nocluster)。accept-env.sh up の --variant と同じ値にする(元サーバーの中身と、偽の LLM が調査から作る定義の対象を合わせる)。
#   例: VARIANT=nocluster make test-app-e2e NAME=<名前>(環境変数は make の recipe にそのまま渡る)。add は偽の LLM が足せる候補に反応しないため未対応。
# 終わったら make test-env-down NAME=<名前> と bash test/env/bin/accept-env.sh down <名前>。記録は test/records/driver/ (OUT で変える)。
set -euo pipefail
name="${1:?NAME}"
cd "$(dirname "$0")/../.."
root="$PWD"
out="${OUT:-test/records/driver}"
variant="${VARIANT:-base}"
case "$variant" in base|lo|nocluster) ;; *) echo "VARIANT は base|lo|nocluster(add は偽の LLM が未対応)" >&2; exit 64;; esac
NAME="$name" ACCEPT="$name" bash test/env/bin/in-linux.sh 'bash test/wiring/prep.sh'
# 試験ビルドの核の信頼の起点と sudo の環境(試験の環境の準備。A2 の実行前にも同じ 1 コマンド)。
bash test/env/bin/accept-env.sh prepare-a2 "$name"
# bootstrap の後に、保管先準備アダプタの代役を root で置く(ドライバが dist/test/wiring/need-doubles を作ったら置いて doubles-done を作る)。
rm -f dist/test/wiring/need-doubles dist/test/wiring/doubles-done
bash test/wiring/doubles-watch.sh "$name" &
watcher=$!
trap 'kill $watcher 2>/dev/null || true' EXIT
rc=0
node app/test/e2e/cli.ts --mode fake --name "$name" --variant "$variant" --script test/acceptance/scripts/A2/script.json --doubles dist/test/wiring --out "$out/fake-a2" || rc=$?
echo "A2(偽の LLM)の終了コード: $rc"
[ "$rc" = 0 ] || exit "$rc"
rc=0
node app/test/e2e/cli.ts --mode fake --name "$name" --script test/acceptance/scripts/A6/injections.json --out "$out/fake-a6" || rc=$?
echo "A6(偽の LLM)の終了コード: $rc"
exit "$rc"
