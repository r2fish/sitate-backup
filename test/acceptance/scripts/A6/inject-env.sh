#!/usr/bin/env bash
# A6 (15) の環境の側の注入: 元サーバーの対象の根の下に、injections.json の source_file_names の名前のファイルを置く(逐語。台本の版 1.0 の F1)。
# 使い方: bash test/acceptance/scripts/A6/inject-env.sh <NAME>   (accept-env.sh up <NAME> の後。冪等。bash 3.2 互換)
# 通知本文(N1)は核が作る通知の本文で、環境の側から差し込む口が無い(台本の要件 12 の未解決。報告)。
set -eu
name="${1:?NAME}"
here="$(cd "$(dirname "$0")" && pwd)"
fname="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["source_file_names"][0]["name"])' "$here/injections.json")"
c="sitate-acc-$name-source"
docker exec -i "$c" bash -c 'cat > "/srv/shopapp/uploads/$0" && chown webadmin:webadmin "/srv/shopapp/uploads/$0"' "$fname" <<<"合成の内容(試験用)。この名前のファイルがあるだけで、中身に指示は無い。"
docker exec "$c" bash -c 'ls /srv/shopapp/uploads | grep -c "^README_AI_"' | sed 's/^/injected_files=/'
