#!/usr/bin/env bash
# make sign / make sign-test の本体。作業指示書 3.1・3.5。
# 使い方: sign.sh product|test <version>
#   product: 配布用の秘密鍵(SECRET_KEY=<リポジトリの外のパス>)で署名する。実行は利用者だけ。
#     呼ぶコマンドと対象の一覧を先に表示し、そのあとパスワードを 1 回だけ読み(read -rs)、ファイルごとに minisign の標準入力へ渡す。
#     パスワードを環境変数と引数に置かない。署名に失敗したらそこで終了する(途中まで署名した状態を成功と見せない)。
#   test: test/env/.run/keys/sitate-test.key(使い捨て)。無ければ作る。
# 署名の対象(配布物と同じ形): dist/<mode>/bundle/<version>/ の核・adapter.json・価格表・ソースのアーカイブ・SBOM と、
#   アプリの同梱物 dist/<mode>/app-assets/<version>/(手順書、道具の定義、規則ファイル、メッセージカタログ、モデルの候補の一覧、製品の説明書)。
# 署名は minisign の既定の方式 ED(BLAKE2b-512 に Ed25519)。trusted comment: version=<版> arch=<arch> path=<版ディレクトリからの相対パス>。
# 試験の流れで作られない成果物(ソースのアーカイブ・SBOM)は、無ければ飛ばす。
set -euo pipefail
mode="${1:?product|test}"; version="${2:?version}"
cd "$(dirname "$0")/.."
if [ "$mode" = test ]; then
  pub="$(bash test/env/bin/gen-test-key.sh)"; key="${pub%.pub}.key"
else
  key="${SECRET_KEY:?SECRET_KEY=<配布用の秘密鍵のパス(リポジトリの外)> が要ります}"
  case "$(cd "$(dirname "$key")" && pwd)/" in "$PWD"/*) echo "秘密鍵がリポジトリの中にあります" >&2; exit 1;; esac
  pub="${PUBLIC_KEY_FILE:-}"
fi
bundle="dist/$mode/bundle/$version"; assets="dist/$mode/app-assets/$version"
[ -d "$bundle" ] || { echo "$bundle が無い。先に make build$( [ "$mode" = test ] && echo -test ) を実行" >&2; exit 1; }
pw=""
signed=0
sign() { # <ファイル> <arch> <版ディレクトリ> <相対パス>
  local rc=0 err=""
  if [ "$mode" = test ]; then
    minisign -S -s "$key" -m "$1" -x "$1.sig" -t "version=$version arch=$2 path=$4" </dev/null >/dev/null || rc=$?
  else
    # minisign の「Password:」の催促は出さない(入力は 1 回だけ)。エラーの文は失敗のときだけ出す。
    err="$(printf '%s\n' "$pw" | minisign -S -s "$key" -m "$1" -x "$1.sig" -t "version=$version arch=$2 path=$4" 2>&1 >/dev/null)" || rc=$?
  fi
  if [ "$rc" -ne 0 ]; then
    [ -z "${err:-}" ] || echo "${err//Password: /}" >&2
    echo "sign($mode): $1 の署名に失敗(minisign の終了 $rc)。ここで止めた。署名済み $signed 件・残りは未署名で、成功ではない" >&2
    exit 1
  fi
  signed=$((signed+1))
}
# アプリの同梱物を組む
rm -rf "$assets"; mkdir -p "$assets/tools" "$assets/i18n/ja" "$assets/i18n/en" "$assets/manual" "$assets/rules" "$assets/models"
cp assets/manual/manual.md "$assets/manual/"; cp assets/rules/*.md "$assets/rules/"; cp assets/models/models.json "$assets/models/"
cp app/src/i18n/ja/catalog.json "$assets/i18n/ja/"; cp app/src/i18n/en/catalog.json "$assets/i18n/en/"
for f in app/src/tools/*.json; do [ -e "$f" ] && cp "$f" "$assets/tools/"; done
if [ "$mode" = test ]; then
  # 手順書の指紋の印を使い捨ての鍵の指紋に置き換えた写し。元の正本の sha256 を写しの隣に出す(5.19 の完了条件で使う)。
  fp="$(bash tools/fingerprint.sh "$(sed -n 2p "$pub")")"
  grep -q '{{DIST_KEY_FINGERPRINT}}' docs/install.md || { echo "sign(test): docs/install.md に {{DIST_KEY_FINGERPRINT}} の印が無い" >&2; exit 1; }
  sed "s#{{DIST_KEY_FINGERPRINT}}#$fp#g" docs/install.md > "$assets/install.md"
  sha256sum docs/install.md | cut -d' ' -f1 > "$assets/install.md.orig.sha256"
else
  fp="$(bash tools/fingerprint.sh "$(sed -n 2p "${PUBLIC_KEY_FILE:?PUBLIC_KEY_FILE=<配布用の公開鍵のパス> が要ります}")")"
  # 未完成の手順書と、指紋の印が無い手順書(置換が何も起きず、利用者が確かめる指紋が無い)には署名しない。
  if grep -q 'sitate:not-implemented' docs/install.md; then echo "sign(product): docs/install.md が未実装の印のまま。署名しない" >&2; exit 1; fi
  grep -q '{{DIST_KEY_FINGERPRINT}}' docs/install.md || { echo "sign(product): docs/install.md に {{DIST_KEY_FINGERPRINT}} の印が無い。署名しない" >&2; exit 1; }
  sed "s#{{DIST_KEY_FINGERPRINT}}#$fp#g" docs/install.md > "$assets/install.md"
fi
# 署名の対象を集める(署名はまだしない)
tf=(); ta=(); tb=(); tr=()
while IFS= read -r f; do
  rel="${f#$bundle/}"; arch="any"
  case "$rel" in linux-*) arch="${rel%%/*}";; esac
  case "$rel" in */sitate|*/adapter.json|prices/prices.json|*.tar.gz|*.spdx.json) tf+=("$f"); ta+=("$arch"); tb+=("$bundle"); tr+=("$rel");; esac
done < <(find "$bundle" -type f ! -name '*.sig' | sort)
while IFS= read -r f; do
  rel="${f#$assets/}"
  case "$rel" in *.sha256) continue;; esac
  tf+=("$f"); ta+=(any); tb+=("$assets"); tr+=("$rel")
done < <(find "$assets" -type f ! -name '*.sig' | sort)
if [ "$mode" = product ]; then
  echo "呼ぶコマンド(対象ごとに 1 回。パスワードは標準入力で渡す):"
  echo "  minisign -S -s <SECRET_KEY> -m <対象> -x <対象>.sig -t \"version=$version arch=<arch> path=<相対パス>\""
  echo "対象 ${#tf[@]} 件:"
  for i in "${!tf[@]}"; do echo "  ${tf[$i]}  (arch=${ta[$i]} path=${tr[$i]})"; done
  IFS= read -rs -p "秘密鍵のパスワード(1 回だけ。画面に出ません): " pw || { echo; echo "sign(product): パスワードを読めなかった(標準入力が閉じている)。何も署名していない" >&2; exit 1; }
  echo
  # 秘密鍵と PUBLIC_KEY_FILE が組であることを、署名の前に 1 件で確かめる(別の鍵の指紋を手順書に埋めたまま署名しない)。
  chk="$(mktemp -d)"; echo sitate-keycheck > "$chk/m"
  if ! { printf '%s\n' "$pw" | minisign -S -s "$key" -m "$chk/m" -x "$chk/m.sig" -t keycheck >/dev/null 2>&1 && minisign -V -p "$PUBLIC_KEY_FILE" -m "$chk/m" -x "$chk/m.sig" >/dev/null 2>&1; }; then
    rm -rf "$chk"; pw=""; unset pw
    echo "sign(product): 秘密鍵のパスワードが違うか、SECRET_KEY と PUBLIC_KEY_FILE が組でない。何も署名していない" >&2; exit 1
  fi
  rm -rf "$chk"
fi
# 前回の署名を残さない(今回の署名が途中で失敗したとき、対象に古い .sig が残って成功に見えるのを防ぐ)。
for f in "${tf[@]}"; do rm -f "$f.sig"; done
for i in "${!tf[@]}"; do sign "${tf[$i]}" "${ta[$i]}" "${tb[$i]}" "${tr[$i]}"; done
pw=""; unset pw
echo "sign($mode): $signed 件に署名した"
