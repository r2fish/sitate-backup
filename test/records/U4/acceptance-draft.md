# U4 下書きの受け入れの記録

- 再実行: 2026-11-03 JST、対象の作業ツリーで監督が実行。照らした文書は仕様書 1.2.7、作業指示書 1.0.16。

| コマンド | 結果 |
|---|---|
| `make lint-install` | 合格 |
| `go test ./tools/lintinstall/`(骨格の前提の試験 2 件を監督が手順書の検査に置き換えた) | `ok` |
| `go test -tags sitate_testbuild ./test/e2e/` | `ok`。`TestInstallCompletionChecks` は `SITATE_E2E_BIN` が無く skip |

## 残したもの

- 中身の照合と別の起動のレビューは U4 確定(5.19)で行う。
- `card.field.<op>.<欄名>` のカタログのキーが `keys.json` に無い。監督が足す。
- `marks.json` は実装担当の案(`make sign` と `make sign-test` が署名の対象に入れて作る)を設計の監督に確かめる。
- 付録「通常の復元」「全損からの復元」は範囲の外。
