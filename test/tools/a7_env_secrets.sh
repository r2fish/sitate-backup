#!/usr/bin/env bash
# A7 用: 受入試験の環境 <NAME> が生成した秘密の値を、1 行 1 値で標準出力へ出す(check_a7.py secrets の標準入力へパイプする)。
# 使い方(利用者のターミナル。環境を down する前に):
#   bash test/tools/a7_env_secrets.sh <NAME> | python3 test/tools/check_a7.py secrets --records <記録のディレクトリ>
# 値は画面にもファイルにも引数にも出さない。標準出力はパイプ先で受ける。標準エラーには件数の内訳(値なし)だけを出す。
# 出す値: 管理用 SSH の秘密鍵(本文の行ごと)、Webhook の URL、バックアップサーバーの状態ディレクトリの secrets/*.json の文字列の値
#   (書き込み鍵・読み取り鍵の secret(key_id は記録に出てよい識別子なので除く)、pull 用・復元先用の SSH 秘密鍵の本文の行、通知の資格情報)。init のフレーズは環境に残らないので出せない。
set -eu
name="${1:?NAME}"
case "$name" in *[!a-z0-9-]*|"") echo "NAME は小文字・数字・ハイフンだけ" >&2; exit 64;; esac
root="$(cd "$(dirname "$0")/../.." && pwd)"
run="$root/test/env/.run/accept-$name"
[ -f "$run/env" ] || { echo "環境が無い(down 済みか未起動): $run" >&2; exit 66; }
state="${SITATE_STATE_DIR:-/var/lib/sitate}"
cn="sitate-acc-$name-backup"
# 管理用 SSH の秘密鍵(PEM の見出し行を除いた本文の行)。
if [ -f "$run/ssh/admin_ed25519" ]; then
  grep -v '^-----' "$run/ssh/admin_ed25519" | awk 'length($0)>=8 {print; n++} END {printf "a7_env_secrets: admin_key_lines=%d\n", n+0 > "/dev/stderr"}'
fi
# Webhook の URL
hook="$(grep '^SITATE_ACCEPT_HOOK_URL=' "$run/env" | head -1 | cut -d= -f2-)"
if [ -n "$hook" ]; then printf '%s\n' "$hook"; echo "a7_env_secrets: hook_url=1" >&2; else echo "a7_env_secrets: hook_url=0" >&2; fi
# バックアップサーバーの secrets/*.json(JSON の文字列の値。kind・role・fp・public_key などの非秘密の欄は除く)
docker exec "$cn" sh -c "for f in '$state'/secrets/*.json; do [ -f \"\$f\" ] && cat \"\$f\" && echo; done" 2>/dev/null \
  | python3 -I -c '
import json, sys
SKIP = {"kind", "role", "fp", "public_key", "format", "key_id"}
text = sys.stdin.read()
dec, i, out = json.JSONDecoder(), 0, []
def walk(v, k=None):
    if isinstance(v, dict):
        for kk, vv in v.items(): walk(vv, kk)
    elif isinstance(v, list):
        for x in v: walk(x, k)
    elif isinstance(v, str) and k not in SKIP:
        for line in v.split("\n"):
            line = line.rstrip("\r")
            if len(line) >= 8 and not line.startswith("-----"): out.append(line)
while True:
    while i < len(text) and text[i].isspace(): i += 1
    if i >= len(text): break
    obj, i = dec.raw_decode(text, i)
    walk(obj)
for o in out: print(o)
' | awk '{print; n++} END {printf "a7_env_secrets: state_values=%d (init のフレーズは環境に残らないので対象外)\n", n+0 > "/dev/stderr"}'
