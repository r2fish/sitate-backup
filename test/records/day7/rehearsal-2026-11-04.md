# 7 日目のビルドの手順の予行(2026-11-04、監督)

配布用の `make sign`・`make sign-installer` は利用者だけが行うので、同じ順を試験の鍵(`make sign-test`)で通した。branch `wp/docs`(2c9777f。製品のコードは main の a6e2b60 と同じ)。

## 結果

| 手順 | 終了コード | 見たもの |
|---|---|---|
| `npm --prefix app ci` | 0 | |
| `make build`(製品) | 0 | `dist/product/bundle/0.1.0-dev/` に `linux-arm64`・`linux-x86_64`・`prices`・ソースのアーカイブ・SBOM(SPDX) |
| `make build-app`(製品、署名の前) | 2 | `公開鍵のファイルが無い(--pubkey-file。product は PUBLIC_KEY_FILE)` |
| 同、`PUBLIC_KEY_FILE` に試験の公開鍵の絶対パス | 1 | `dist/product/app-assets/0.1.0-dev が無い。先に make build と make sign を実行`(署名の前に止まる。期待どおり) |
| `make build-test` → `make sign-test` → `make build-app-test` | 0・0・0 | 署名のファイル 12 個 |
| `dist/test/install.md.orig.sha256` と `docs/install.md` の sha256 | 一致 | |
| `make test-app` | 0 | `ℹ pass 301`、`ℹ fail 0` |
| 作業ツリー | 変更 0 件 | |

## 7 日目の手順への申し送り

- `make sign` と `make build-app`(製品)は、どちらも環境変数 `PUBLIC_KEY_FILE`(配布用の公開鍵)を要る。`build-app` は `npm --prefix app` の下で動くので、相対パスは `app/` から解かれる。**絶対パスで渡す**(相対パスでは「公開鍵のファイルが無い」で止まる。予行で確かめた)。
- 順は、監督の `make build`、利用者の `SECRET_KEY=<絶対パス> PUBLIC_KEY_FILE=<絶対パス> make sign`、監督の `PUBLIC_KEY_FILE=<絶対パス> make build-app`、利用者の `make sign-installer` の順。`build-app` は署名済みの `dist/product/app-assets/` が無いと止まる。
- 製品の側の署名とインストーラーの検証は、利用者の鍵が要るので未実行。
