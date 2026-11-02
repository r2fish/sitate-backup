# U1g-kit の受け入れの記録

- 再実行: 2026-11-03 JST、監督が最終の作業ツリーで実行。照らした文書は仕様書 1.2.7(4.7)。

| コマンド | 結果 |
|---|---|
| `go test -count=1 -race ./internal/keys/ ./internal/contract/` | 両方 `ok` |
| `check_scope.py --allow internal/keys/` | `PASS` |
| `make lint-size` | 合格 |

- 変異: 手順 (5) の `--to` を外すと `TestKitFieldsSteps` が赤。修正前のコードでも同じ試験が赤になったと担当が報告した。
- 確かめていないこと: 紙のコマンドの核での実行。`$RUN` の引用は描画部(U3b)の責任。
- 残したもの: `storage.country` は値の出所が無く `null` のまま(設計の監督へ)。別の起動のレビューは小さな修正なので省いた。差分は監督が読んだ。
