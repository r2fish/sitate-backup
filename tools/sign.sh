#!/usr/bin/env bash
# make sign / make sign-test の本体。作業指示書 3.1・3.5。
# 使い方: sign.sh product|test <version>
#   product: 配布用の秘密鍵(SECRET_KEY=<リポジトリの外のパス>)で署名する。実行は利用者だけ。
#     呼ぶコマンドと対象の一覧を先に表示し、そのあとパスワードを 1 回だけ読み(read -rs)、ファイルごとに minisign の標準入力へ渡す。
#     パスワードを環境変数と引数に置かない。署名に失敗したらそこで終了する(途中まで署名した状態を成功と見せない)。
#   test: test/env/.run/keys/sitate-test.key(使い捨て)。無ければ作る。
# 署名の対象(配布物と同じ形): dist/<mode>/bundle/<version>/ の核・adapter.json・価格表・ソースのアーカイブ・SBOM と、
#   アプリの同梱物 dist/<mode>/app-assets/<version>/(手順書、手順書の印の値 marks.json、道具の定義、規則ファイル、メッセージカタログ、モデルの候補の一覧、製品の説明書)。
#   make build-app[-test] は 2 つを 1 つの置き場に合わせて組む(app/scripts/build-app.mjs)。
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
# アプリの同梱物を組む。置き場の形はアプリの読み込み(app/src/main/bundle-load.ts)に合わせる:
# manual.md・install.md・models.json・marks.json は直下、tools/・rules/・i18n/<言語>/catalog.json は下。署名の path= はこの相対パスと一致する。
rm -rf "$assets"; mkdir -p "$assets/tools" "$assets/i18n/ja" "$assets/i18n/en" "$assets/rules"
cp assets/manual/manual.md "$assets/manual.md"; cp assets/rules/*.md "$assets/rules/"; cp assets/models/models.json "$assets/models.json"
cp app/src/i18n/ja/catalog.json "$assets/i18n/ja/"; cp app/src/i18n/en/catalog.json "$assets/i18n/en/"
for f in app/src/tools/*.json; do [ -e "$f" ] && cp "$f" "$assets/tools/"; done
# models.json の候補が空だとエージェントが起動しない(LLM_MODEL_NOT_FOUND)。空の一覧は署名しない。
jq -e '.models | length > 0' "$assets/models.json" >/dev/null || { echo "sign($mode): assets/models/models.json の候補が空。署名しない" >&2; exit 1; }
# 手順書の印の値。TIMER_NAMES と WARNING_ALLOWLIST は手順書の文面にも marks.json にも同じ値を入れる(手順の合格の判定は marks.json、エージェントは文面を読む)。
# TIMER_NAMES: 核の status.schedule は日次の時刻の文字列で、タイマーの単位の名前の一覧を返さない(仕様の穴として報告済み)ため、空の一覧にする。
timer_names='[]'; warning_allowlist='[]'
if [ "$mode" = test ]; then
  fp="$(bash tools/fingerprint.sh "$(sed -n 2p "$pub")")"
else
  fp="$(bash tools/fingerprint.sh "$(sed -n 2p "${PUBLIC_KEY_FILE:?PUBLIC_KEY_FILE=<配布用の公開鍵のパス> が要ります}")")"
  # 未完成の手順書には署名しない。
  if grep -q 'sitate:not-implemented' docs/install.md; then echo "sign(product): docs/install.md が未実装の印のまま。署名しない" >&2; exit 1; fi
fi
# 指紋の印が無い手順書(置換が何も起きず、利用者が確かめる指紋が無い)には署名しない。
grep -q '{{DIST_KEY_FINGERPRINT}}' docs/install.md || { echo "sign($mode): docs/install.md に {{DIST_KEY_FINGERPRINT}} の印が無い。署名しない" >&2; exit 1; }
sed -e "s#{{DIST_KEY_FINGERPRINT}}#$fp#g" -e "s#{{TIMER_NAMES}}#$timer_names#g" -e "s#{{WARNING_ALLOWLIST}}#$warning_allowlist#g" docs/install.md > "$assets/install.md"
if grep -q '{{[A-Z_]*}}' "$assets/install.md"; then echo "sign($mode): 手順書に埋まっていない印が残っている: $(grep -o '{{[A-Z_]*}}' "$assets/install.md" | sort -u | tr '\n' ' ')" >&2; exit 1; fi
jq -n --arg fp "$fp" --argjson t "$timer_names" --argjson w "$warning_allowlist" '{DIST_KEY_FINGERPRINT: $fp, TIMER_NAMES: $t, WARNING_ALLOWLIST: $w}' > "$assets/marks.json"
# 元の手順書の sha256(5.19 の完了条件で使う)は署名の対象でないので、同梱物の置き場の外に出す。
sha256sum docs/install.md | cut -d' ' -f1 > "dist/$mode/install.md.orig.sha256"
# 署名の対象を集める(署名はまだしない)
tf=(); ta=(); tb=(); tr=()
while IFS= read -r f; do
  rel="${f#$bundle/}"; arch="any"
  case "$rel" in linux-*) arch="${rel%%/*}";; esac
  case "$rel" in */sitate|*/adapter.json|prices/prices.json|*.tar.gz|*.spdx.json) tf+=("$f"); ta+=("$arch"); tb+=("$bundle"); tr+=("$rel");; esac
done < <(find "$bundle" -type f ! -name '*.sig' | sort)
while IFS= read -r f; do
  rel="${f#$assets/}"
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
