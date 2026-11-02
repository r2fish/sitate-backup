#!/usr/bin/env bash
# make sign-installer / make sign-test-installer: アプリのインストーラーだけに署名し、<ファイル名>.sig を並べて置く。
# 使い方: sign-installer.sh product|test <インストーラーのパス>
# product は SECRET_KEY(リポジトリの外)、test は使い捨ての鍵。作業端末(インストーラーを作る場所)で走らせる。
# product は呼ぶコマンドと対象を先に表示し、パスワードを read -rs で 1 回だけ読んで minisign の標準入力へ渡す(環境変数と引数に置かない)。失敗したら終了する。
set -euo pipefail
mode="${1:?product|test}"; inst="${2:?INSTALLER=<インストーラーのパス>}"
cd "$(dirname "$0")/.."
if [ "$mode" = test ]; then
  pub="$(bash test/env/bin/gen-test-key.sh)"; key="${pub%.pub}.key"
else
  key="${SECRET_KEY:?SECRET_KEY が要ります}"
  case "$(cd "$(dirname "$key")" && pwd)/" in "$PWD"/*) echo "秘密鍵がリポジトリの中にあります" >&2; exit 1;; esac
fi
version="${VERSION:-0.1.0-dev}"
tcomment="version=$version arch=any path=$(basename "$inst")"
if [ "$mode" = test ]; then
  minisign -S -s "$key" -m "$inst" -x "$inst.sig" -t "$tcomment" </dev/null >/dev/null
else
  echo "呼ぶコマンド: minisign -S -s <SECRET_KEY> -m $inst -x $inst.sig -t \"$tcomment\""
  IFS= read -rs -p "秘密鍵のパスワード(1 回だけ。画面に出ません): " pw || { echo; echo "sign-installer: パスワードを読めなかった(標準入力が閉じている)" >&2; exit 1; }
  echo
  # minisign の「Password:」の催促は出さない(入力は 1 回だけ)。エラーの文は失敗のときだけ出す(sign.sh と同じ)。
  rc=0; err="$(printf '%s\n' "$pw" | minisign -S -s "$key" -m "$inst" -x "$inst.sig" -t "$tcomment" 2>&1 >/dev/null)" || rc=$?
  pw=""; unset pw
  [ "$rc" -eq 0 ] || { [ -z "$err" ] || echo "${err//Password: /}" >&2; echo "sign-installer: 署名に失敗(minisign の終了 $rc)。$inst.sig は信用しない" >&2; rm -f "$inst.sig"; exit 1; }
fi
echo "sign-installer($mode): $inst.sig"
