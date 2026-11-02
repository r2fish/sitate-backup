# U2b の受け入れの記録

- 再実行: 2026-11-03 JST、対象の作業ツリーで監督が実行。照らした文書は仕様書 1.2.7、作業指示書 1.0.16。

| コマンド | 結果 |
|---|---|
| `make test-adapters ADAPTER=acquire/postgresql` 相当、`go test ./internal/contract/` | `ok`。`TestRealPostgreSQL` はこの実行では skip |
| 実物の PostgreSQL 17.11 のイメージの中で `TestRealPostgreSQL`(手順は下) | PASS |
| `check_scope.py`、`make lint-size` | PASS、終了 0 |

実物の試験の手順: 起動定義の PostgreSQL のイメージへ `docker cp` で試験の実行ファイルとアダプタを入れ、root でない postgres ユーザーで走らせる。手順の台本はリポジトリに置いていない(U2a の試験の続きで `test/` に置く)。

## 変異

実装担当の記録: 終了状態 1〜125 の分類を壊すと 4 件が赤。監督は再実行していない。

## 確かめていない範囲

- 別の起動のレビューは受けていない。DB の対象の結合は主経路の第 2 段以降。U1c の `target_stream` に実際に流す結合は未実施。
- `sitate-ro` の権限、ラージオブジェクト、読めない表は U2a の試験の残り。
