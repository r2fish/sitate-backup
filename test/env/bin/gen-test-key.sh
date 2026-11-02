#!/usr/bin/env bash
# 使い捨ての署名鍵(試験ビルドの信頼の起点)を作る。生成先: test/env/.run/keys/sitate-test.{key,pub}(git 管理外)。
# 形式: minisign 互換(方式 ED の署名ができる Ed25519)。パスワードなし。`test/env/` の起動時に U0-env も呼ぶ。
set -euo pipefail
root="$(cd "$(dirname "$0")/../../.." && pwd)"
d="$root/test/env/.run/keys"; mkdir -p "$d"
if [ -f "$d/sitate-test.key" ] && [ "${1:-}" != "--force" ]; then echo "$d/sitate-test.pub"; exit 0; fi
rm -f "$d/sitate-test.key" "$d/sitate-test.pub"
minisign -G -f -W -p "$d/sitate-test.pub" -s "$d/sitate-test.key" >/dev/null
echo "$d/sitate-test.pub"
