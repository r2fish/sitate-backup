# U2e 完了条件 → 試験の対応表(作業指示書 5.12。テスト仕様書 6.1 の 1)

作成: 2026-11-04(期間中)。作業ツリー wp/maps(開始 commit f503da6)。試験は変えていない。
実行: `make shell-linux CMD='go test -count=1 -v ./adapters/...'`(全体の `-v` 実行の一部。既定のタグと `-tags sitate_testbuild`)。adapters/notify/webhook は `ok`、FAIL 0。ログは作業の scratchpad の `maps-go-default.log`・`maps-go-tb.log`。`make test-adapters ADAPTER=...` そのものは実行していない(同じ `go test` の題で代替)。SKIP は緑に数えない。
試験名は関数名で、場所は `adapters/notify/webhook/` の `*_test.go`。

| # | 試験 | 結果 | 備考 |
|---|---|---|---|
| 共通(契約) | `TestContractAdapter`、`TestAdapterJSONDeclaresTheSlotsTheAdapterReads` | 緑 | |
| 通知先の URL が標準出力・標準エラー・結果に現れない | `TestSecretURLNeverInOutput`、`TestSecretURLNotInStdoutStderrOfBinary` | 緑 | |
| CA の環境変数が無いとき OS の信頼ストアだけで検証し、自己署名の受け手で `delivered: false`・`reason: tls` | `TestSelfSignedWithoutCA`、`TestBrokenCAFileFailsClosed` | 緑 | |
| 環境変数で受け手の CA を与えると配送できる | `TestDeliveredWithExtraCA` | 緑 | 「この変数は試験ビルドの核だけが渡す」は `internal/adapter` の `TestProductBuildDoesNotPassTestVars`(U1f-0) |
| 検証を無効にする経路と特定のホストを除く経路を持たない(GB-36) | `TestNoInsecureEscapeHatch` | 緑 | |
| 配送の挙動 | `TestHeadersExactlyThree`、`TestHTTPStatus`、`TestRedirectNotFollowed`、`TestURLInvalid`、`TestConnectFailure`、`TestTimeout`、`TestNoProxy`、`TestResponseBodyCapped`、`TestRequestErrors` | 緑 | 条件の字面にない実装の題(参考) |

## 集計(行ごと 6 行)
- 緑 6 / 他 0。受入試験の環境での実機の配送は A6 の記録。
