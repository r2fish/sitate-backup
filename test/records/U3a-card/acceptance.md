# U3a-card の受け入れの記録

- 再実行: 2026-11-03 JST、監督が最終の作業ツリーで実行。照らした文書は仕様書 1.2.7(「承認カードの書式」、3.1.2.9)。

| コマンド | 結果 |
|---|---|
| `npm --prefix app run test:u3a` | 184 件 pass、fail 0、skip 0 |
| `npm --prefix app run test:contract` | 42 件 pass |
| `typecheck`、`lint`、`lint:size` | 終了 0 |

- 変異(監督が実行): 見出しのキーの代わりに生の欄名を返すと `card.test.ts` の 4 件が赤。
- 範囲の検査の `protected` は `app/src/i18n/keys.json` だけで、監督が追加を許可した。
- 規則ファイルの直し(`revoke_key_ids`、`def_hash_after`)は仕様 3.1.2.9 に合わせたもの。
- 別の起動のレビューはしていない。まとめレビューで見る。実機の画面は未確認。
