#!/usr/bin/env bash
# make build / make build-test の本体(開発用コンテナの中で走る)。作業指示書 3.1。
# 使い方: build.sh product|test <version> <試験ビルドのタグ>
# 出力: dist/<mode>/bundle/<version>/(spec 4.2 の並び)
#   <os>-<arch>/sitate、<os>-<arch>/adapters/<kind>/<name>/{<exe>,adapter.json(sha256 つき)}、prices/、
#   sitate-<version>-src.tar.gz、sitate-<version>.spdx.json(ソースのアーカイブと SBOM は版ディレクトリの直下)
# バージョン <version> は MAJOR.MINOR.PATCH[-pre](既定 0.1.0-dev)。
set -euo pipefail
# アダプタ 1 つの工程(make build と make test-adapters が同じ工程を呼ぶ。作業指示書 3.2、D-0117): <アダプタの置き場> <goarch> <出力先>
build_adapter() {
  local a="$1" goarch="$2" d="$3"
  mkdir -p "$d"
  local exe; exe="$(jq -r .exe "adapters/$a/adapter.json")"
  CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -trimpath -ldflags "-s -w" -o "$d/$exe" "./adapters/$a"
  local sha; sha="$(sha256sum "$d/$exe" | cut -d' ' -f1)"
  jq --arg s "$sha" '. + {sha256: $s}' "adapters/$a/adapter.json" > "$d/adapter.json"
}
if [ "${1:-}" = adapter ]; then cd "$(dirname "$0")/.."; build_adapter "${2:?adapter}" "${3:?goarch}" "${4:?outdir}"; exit 0; fi
mode="${1:?mode}"; version="${2:?version}"; testtag="${3:?tag}"
cd "$(dirname "$0")/.."
case "$mode" in product) tags="";; test) tags="-tags $testtag";; *) echo "mode: product|test" >&2; exit 64;; esac
out="dist/$mode/bundle/$version"
rm -rf "dist/$mode"; mkdir -p "$out/prices"
adapters="acquire/files acquire/postgresql restore-dest/ssh-host notify/webhook provision/b2"
for pair in linux-arm64:arm64 linux-x86_64:amd64; do
  name="${pair%%:*}"; goarch="${pair##*:}"
  CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -trimpath $tags -ldflags "-s -w" -o "$out/$name/sitate" ./cmd/sitate
  for a in $adapters; do
    build_adapter "$a" "$goarch" "$out/$name/adapters/$a"
  done
done
# 製品ビルドの依存に fake と使い捨ての鍵が無いこと、試験ビルドの依存に配布用の鍵だけの定数ファイルが無いこと(3.5)。
deps="$(go list -deps $tags ./cmd/sitate ./adapters/...)"
if [ "$mode" = product ]; then
  if echo "$deps" | grep -qE 'internal/contract/(fake|harness|s3test)'; then echo "製品ビルドの依存に internal/contract/fake 系が入っている" >&2; exit 1; fi
  if go list $tags -f '{{join .GoFiles "\n"}}' ./internal/contract | grep -q '^trust_testbuild.go$'; then echo "製品ビルドに試験用の信頼の起点が入っている" >&2; exit 1; fi
else
  if go list $tags -f '{{join .GoFiles "\n"}}' ./internal/contract | grep -q '^trust_product.go$'; then echo "試験ビルドに配布用の信頼の起点が入っている" >&2; exit 1; fi
fi
echo "build($mode): 依存の検査 OK"
# ソースのアーカイブ(HEAD を固めた tar.gz)と SBOM。
src="$out/sitate-$version-src.tar.gz"
if git rev-parse --verify -q HEAD >/dev/null 2>&1; then
  git archive --format=tar.gz --prefix="sitate-backup-$version/" -o "$src" HEAD
  if [ -n "$(git status --porcelain)" ]; then echo "警告: 作業ツリーに未 commit の差がある。アーカイブは HEAD を固めた結果で、差は含まない" >&2; fi
else
  echo "警告: HEAD が無い(最初の commit の前)ため、ソースのアーカイブを飛ばす。commit の後に make build を再実行する" >&2
fi
go run ./tools/sbom "$version" "$out/sitate-$version.spdx.json"
echo "build($mode): $out"
