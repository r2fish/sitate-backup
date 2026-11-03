#!/usr/bin/env bash
# minisign の公開鍵(base64 の 1 行)から指紋を出す(spec 3.2.1「公開鍵の符号化」: 指紋は公開鍵 32 バイトを ssh-ed25519 の blob にして SHA-256 を掛ける)。
# 形式: SHA256:<base64(= なし)>。核の version の signing_key_fp と同じ値になる。
set -euo pipefail
{ printf '\000\000\000\013ssh-ed25519\000\000\000\040'; printf '%s' "$1" | base64 -d | tail -c 32; } | openssl dgst -sha256 -binary | base64 | tr -d '=' | sed 's/^/SHA256:/'
