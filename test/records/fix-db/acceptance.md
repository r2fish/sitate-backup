# 直し 2(D-0172・D-0175)の受け入れ(2026-11-04、監督)

branch `wp/fix-db`(b3e1143、cbc2bc6、92ab692、1d581ab、1288d3d)。統合の branch `wp/integrate-2` に入れた。main へは測定 1 が閉じた後に入れる(D-0172)。

## 完了条件と試験

| 条件(裁定) | 試験(ファイルと関数) |
|---|---|
| postgresql の対象の取得の要求は `spec.path` を持たない。定義に port があれば port だけを置く(D-0172、仕様 3.2.2) | `internal/acquire/request_spec_test.go` の `TestTargetSpecRequestShapeByKind`、`internal/contract` の `TestAdapterVectors`(`valid/acquire-request-pg-no-port.json`、`invalid/acquire-request-pg-with-path.json`) |
| DB の対象が実物の環境で取れ、復元先にダンプが届く | `test/mainpath/stage3_db_test.go` の `TestStageDB`(`make test-mainpath STAGE=DB`) |
| 対象の失敗は `detail`・`next` を持つ(D-0172、3.1.2 の `targets[]`) | `internal/acquire/request_spec_test.go` の `TestFailedTargetCarriesDetailAndNext` |
| init の前の `bucket check` は `NOT_INITIALIZED` を返さない(3.8 の手順 5) | `internal/cli/bucket_check_preinit_test.go` の `TestBucketCheckBeforeInitIsNotNotInitialized` |
| `init-storage` の `--adapter` は必須(D-0175、3.1.2、3.8 の手順 2) | 同ファイルの `TestInitStorageRequiresAdapter` |
| `--host` の形 `ssh://<user>@<host>[:22]`(3.1.2、3.3) | `internal/acquire/hostarg_test.go` の `TestParseHostArgForms` |
| `status.estimate` の `cost_ceiling_yen` は実効値(既定 3,000)、記録からの入力が無い間は `monthly_jpy`・`over_ceiling` が null で警告なし、価格表に無い保管先で `PRICE_UNKNOWN`(D-0175、3.1.2.7) | `internal/cli/status_estimate_test.go` の `TestStatusEstimateDefaultCeilingAndPriceUnknown`・`TestStatusEstimateKnownRowNoWarning` |
| `estimate` は定義が無くても既定の上限と比べる、`over_ceiling` が null は `PRICE_UNKNOWN` のときだけ(3.1.2.8) | `internal/cli/estimate_test.go` の `TestEstimateEndpointWithoutDefinition`、`status_estimate_test.go` の `TestEstimateDefaultCeilingIsCompared`、既存の `TestEstimateMissingClassIsPriceUnknown` |
| 手順書の手順 2・5 の例に `--adapter b2`、手順 8 に `--host`(D-0175) | `make lint-install`(合格)。目視で 3 行 |

## 監督の再実行と変異

- `make test` 0(ok 73)、`npm --prefix app test` pass 296・fail 0、`make lint-size LINT_BASE=a6e2b60` 合格、`make test-mainpath STAGE=3` 0、`STAGE=DB` 0(92ab692)。
- 変異(すべて赤、戻した後に差分なし): status の上限を定義の値だけにする → `TestStatusEstimateDefaultCeilingAndPriceUnknown`。`ceilingState` の既定を 0 → `TestEstimateEndpointWithoutDefinition`・`TestEstimateDefaultCeilingIsCompared`。`PRICE_UNKNOWN` でも比べる → `TestEstimateMissingClassIsPriceUnknown`。`--adapter` を必須から外す → `TestInitStorageRequiresAdapter`。postgresql の要求に port を必須に戻す → `TestTargetSpecRequestShapeByKind`・`TestAdapterVectors`。
- 修正前に落ちた記録: `TestStatusEstimateDefaultCeilingAndPriceUnknown` は b3e1143 のコードで `cost_ceiling_yen:0 over_ceiling:<nil>`、`warnings: []` で落ちた(夜の洗い出しの観測と同じ)。

## レビュー

別起動のコードレビュー(2026-11-04)。重大 1 件(port の無い DB の対象が要求のスキーマで INTERNAL)を直した(92ab692)。軽微 2 件(status の `PRICE_UNKNOWN` の `since` が毎回今、`failDetail` の detail が一部のアダプタ由来の失敗で `error_code` と食い違い得る)は申し送り(測定 1 の後のまとめ)。裁定待ちとして変えないもの: `next/<i>/op`、対象の失敗の `next` が空配列(43 節)。

## 試験の誤りの直し

新しい DB の試験が第 3 段と同じ環境に状態を残し、主経路の前提を壊した。`TestStageDB` として分け、`make test-mainpath STAGE=DB` で別の環境で流す(`Makefile`)。

## 確かめていないこと

実物の B2 と実物の LLM での DB の対象の取得(測定 2 で確かめる)。
