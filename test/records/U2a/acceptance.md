# U2a の受け入れの記録(主経路のための統合。残りを明記)

- 再実行: 2026-11-03 JST、対象の作業ツリーで監督が実行。照らした文書は仕様書 1.2.7、作業指示書 1.0.16。

| コマンド | 結果 |
|---|---|
| `go test -count=1 ./adapters/acquire/recv/`(root、`SITATE_DESTRUCTIVE_SETUP=1`、PostgreSQL 入りの試験用の容器) | `ok`、SKIP 0 件 |
| `make test-adapters ADAPTER=acquire/files` | `ok` |
| `go test -count=1 ./adapters/acquire/recv/ ./adapters/acquire/files/`(非 root) | `ok`。root の試験は skip |
| `make lint-size` | 終了 0 |
| 範囲の検査 | `embed.go` だけ範囲の外で、これは監督が `source-survey` を足した変更 |

## 変異

実装担当の記録: 高の指摘の 9 通りが赤。別の起動のレビューの変異のうち、次は試験が無く生存している(監督は再実行していない)。

## 未完了(主経路のために統合したが、完了条件を満たしていない)

U2a の続きの起動で試験を足す: `prereq_missing[]`、`--rotate`、`sudo -n sh -s`、CONNECT の REVOKE、ラージオブジェクトと読めない表の別々の DB、`dump-pg-globals` の失敗、終了 77、受け口の `survey`、`-xdev`、stat の 3 項目、`-` で始まる名前、全件変化の 100 万件のメモリ。

## レビュー

別の起動のレビューの高 9 件(DB 名からの任意 SQL、接続文字列、`\001` の偽造、死んだ glob、テーブルスペース、ACL が届かない成功、`file` の無い `file-retry`、リンクの根の ACL、空の `from=`)を直した。差し戻しの直しの後の版はレビューを受けていない。

## 確かめていない範囲

- 実 sshd での `restrict` の確認、`sudo -n` の経路、受入試験の環境の元サーバーでの動作(第 2 段)。
- 既定 ACL の副作用は設計の監督の回答待ち(`handoff-to-design.md` の 18 節)。
