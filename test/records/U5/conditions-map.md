# U5 完了条件 → 試験の対応表(作業指示書 5.15。テスト仕様書 6.1 の 1)

作成: 2026-11-04(期間中)。作業ツリー wp/maps(開始 commit f503da6)。試験は変えていない。
実行: `make shell-linux CMD='make _test-contract PART=prices'`(`make test-contract PART=prices` の中身)。終了の出力(逐語): `ok  	github.com/r2fish/sitate-backup/data/prices	0.027s`。全体の `-v` 実行でも `data/prices` は `ok`(`maps-go-default.log`)。ログは scratchpad の `maps-prices.log`。
試験名は関数名で、場所は `data/prices/*_test.go`(他は明記)。

| # | 試験 | 結果 | 備考 |
|---|---|---|---|
| 共通: スキーマ | `TestPricesSchema`(`prices_test.go`)、`TestContractPricesImplemented`(`contract_prices_test.go`) | 緑 | |
| 共通: 使い捨ての鍵での署名と trusted comment | `TestPricesSignature`(`sign_test.go`) | 緑 | 配布用の鍵での署名は利用者の工程(`make sign`。未実施) |
| 共通: 入力の各行(すべての欄)と変換後の値の一致((a)〜(d)) | `TestPricesEveryInputFieldAppears`、`TestPricesRowFieldsMatchInput`、`TestPricesEveryNumberUsed`、`TestPricesConditionFromInput`、`TestPricesChargeUnitsAndCurrency`、`TestPricesCharges`、`TestPricesUpToDate` | 緑 | (a)〜(d) の各定義を 1 つずつ題に割り当てる所までは読んでいない |
| D-0159: `provider`・`endpoint_patterns` が入力と文字列として等しい、`source_url` の例外だけ残る | `TestPricesRowFieldsMatchInput`、`TestPricesEndpointDefault` | 緑 | `source_url` の例外(空白の `%20`)を明示する題は特定できなかった |
| D-0159: 幅で書く段の `tier_upto` が累積 | `TestPricesChargeUnitsAndCurrency`、`TestPricesCharges` | 緑 | 本文で確認(maps2)。`TestPricesCharges` の `checkTierQty` が、入力の「次の N TB」の段の `tier_upto` を「前の段の上限 + N」と照らす(入力の実データに 3 段ある。照合を `got != w` に壊すと赤) |
| D-0159: 月額の基本料金か最低料金がある 5 行の `minimums` が `skipped[]`(`unmapped`) | `TestPricesMinimumFeeRowsSkipped`、`TestPricesMinimums`、`TestPricesMinimumsAppear`、`TestPricesSkippedExact`、`TestPricesSkippedReasons` | 緑 | |
| D-0159: `b2-payg` の `ops`(class_a/b/c の amount 0、class_d と無料枠が残る)、Wasabi 以外の「無料」の行も同じ | `TestPricesFreeRequestCharges` | 緑 | |
| D-0159: `provider` の字種が価格表のスキーマと `cli/results.json` の `prices[].provider` の両方で通る | `TestPriceTableRowsPassResultsSchema`(`internal/cli/estimate_prices_test.go`)、`TestPricesSchema` | 緑 | |

## 集計(行ごと 8 行)
- 緑 8(maps2 で `tier_upto` の累積を本文で確認して緑) / 一部 0 / 試験なし 0 / 範囲外 0。
- 注: 試験の題と欄の網羅は題名からの対応。受け入れの前に `TestPricesEveryInputFieldAppears` が全欄を数えることなどを本文で確認する。
