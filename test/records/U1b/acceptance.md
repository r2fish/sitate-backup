# U1b の受け入れの記録

- 再実行: 2026-11-03 JST、対象の作業ツリーで監督が実行。照らした文書は仕様書 1.2.7、作業指示書 1.0.16。

| コマンド | 結果 |
|---|---|
| `make test-env-up NAME=U1b` の後に `go test -count=1 -v ./internal/store`(MinIO) | `ok`、SKIP 0 件 |
| `go test -race -count=1 ./internal/store` | `ok`(試験の補助の競合 2 件を監督が直した。本体の競合は見つからなかった) |
| `test/tools/cov_stats.py --package store`、`check_scope.py --allow internal/store/`、`make lint-size` | PASS、PASS、終了 0 |

## 変異(監督が実行)

5 通りすべて赤: 期限の判定を常に期限切れ、削除の直前の停止の確認、Compliance を Governance に、空の版 ID、`prune` の時計の確認。実装担当の記録ではほかに 26 通りが赤で、`Content-MD5` を付けない変異も赤になった(U0-core の穴を閉じた)。

## レビュー

別の起動のレビューの重大 5 件(鍵表の行の切れ、索引から外さない、チャンクの retain-until の記録が無い、古い Date、`StoreProbe` の操作の欠け)ほかを直した。差し戻しの直しの後の版は別の起動のレビューを受けていない。

## 確かめていない範囲

- 実物の `format.Codec` と `keys.KeyService` を通した鍵表の読み(第 1 段で確かめる)、B2。
- 結線で `Retention.Index`・`Intents`、実物の `*ObjectStore` を `StoreProbe` に渡すこと(U1f-2)。
- 古い `Date` の閾値(`TIME_SKEW_MAX` の半分)は設計の監督の回答待ち。
