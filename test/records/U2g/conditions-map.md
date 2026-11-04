# U2g 完了条件 → 試験の対応表(作業指示書 5.13。テスト仕様書 6.1 の 1)

作成: 2026-11-04(期間中)。作業ツリー wp/maps(開始 commit f503da6)。試験は変えていない。
実行: `make shell-linux CMD='go test -count=1 -v ./adapters/...'`(全体の `-v` 実行の一部。既定のタグと `-tags sitate_testbuild`)。adapters/provision/b2 は `ok`、FAIL 0。ログは作業の scratchpad の `maps-go-default.log`・`maps-go-tb.log`。`make test-adapters ADAPTER=...` そのものは実行していない(同じ `go test` の題で代替)。SKIP は緑に数えない。
試験名は関数名で、場所は `adapters/provision/b2/` の `*_test.go`。

| # | 試験 | 結果 | 備考 |
|---|---|---|---|
| 共通(契約) | `TestContractAdapter`、`TestAdapterJSONDeclaresTheSlotsTheAdapterReads` | 緑 | |
| `issue-keys` の `secret` が結果 JSON 以外に現れない | `TestIssueKeysSecretOnlyInResultJSON`、`TestNoSecretsInAnyErrorOutput` | 緑 | |
| `issue-keys` の結果が `issued[]` だけで保持の上限の可否を返さない | `TestIssueKeysAllOrNothingSuccess` | 一部 | `retention_cap` を返さないことの専用の題は特定できなかった |
| 鍵の名前が `sitate-` で始まり英数字と `-` だけ | `TestIssueKeysNameRules` | 緑 | |
| `check` の `keys` が印の付いた鍵だけ | `TestCheckAdminFiltersProductKeys`、`TestCheckWithKeyUsesOnlyThatKey`、`TestCheckWithKeyRejected`、`TestCheckAdminListFailureReturnsError` | 緑 | |
| 書き込み鍵で版指定なしの削除を拒否できないことを核の `bucket check` が `KEY_ALLOWS_UNVERSIONED_DELETE` で受ける | U1g の対応表(項目 g)。受入試験 A2 の手順 5 でこの警告が出て導入が止まらない確認 | 未実行 | A2 の手順 5 の確認は A2 の記録(`test/records/A2/`、本調査は読んでいない) |
| 偽の API サーバーでのバケット作成・失効・再試行 | `TestCreateBucketNew`、`...UpdateFailsThenResumes`、`...Existing`、`...MismatchActualValues`、`...TakenByOtherAccount`、`TestRevokeKeys*`、`TestRetryOnlyTransient`、`TestRetryGivesUpAfterMax`、`TestErrorMappingTable`、`TestIssueKeysPartialFailureRevokesIssued`、`TestIssueKeysRollbackFailureReportsOrphans` | 緑 | 実装の題(参考) |

## 集計(行ごと 7 行)
- 緑 5 / 一部 1 / 未実行 1 / 試験なし 0。
