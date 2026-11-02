#!/usr/bin/env bash
# minisign の公開鍵(base64 の 1 行)から指紋を出す: SHA256: + 鍵の本体(方式 2 バイト + 鍵 ID 8 バイト + 鍵 32 バイト)の SHA-256 の base64(= なし)。
set -euo pipefail
printf '%s' "$1" | base64 -d | openssl dgst -sha256 -binary | base64 | tr -d '='  | sed 's/^/SHA256:/'
