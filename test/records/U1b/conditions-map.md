# U1b 完了条件 → 試験の対応表(作業指示書 5.3。テスト仕様書 6.1 の 1)

作成: 2026-11-04(期間中)。作業ツリー wp/maps(開始 commit f503da6)。試験は変えていない。
実行: `make shell-linux CMD='go test -count=1 -v ./internal/... ./adapters/... ./data/... ./cmd/... ./tools/...'`(既定のタグと `-tags sitate_testbuild`)。`internal/store` は `ok`、FAIL 0。ログは作業の scratchpad の `maps-go-default.log`・`maps-go-tb.log`。
試験名は関数名で、場所は `internal/store/` の `*_test.go`。SKIP は緑に数えない。

| # | 試験 | 結果 | 備考 |
|---|---|---|---|
| 共通(契約) | `contract_store_test.go`(`TestContractObjectStorePut`/`GetList`/`Lock`/`BucketConfigStoreTime`/`PrefixOwner`、`TestContractRetentionVerify`/`MissingRefs`/`ExtendTarget`/`Aborted`/`HoldSet`/`Class`、`TestContractStoreProbe`)、`contract_retention_test.go`、`contract_nildeps_test.go` | 緑 | |
| 1 試験用の MinIO | 保管先の偽物に対して: `TestPutExplicitRetainUntilAndContentMD5`、`TestPutMonthlyRetainUntil`(`objectstore`)、`TestHoldSetRepeatedIsIdempotent`、`TestVersionIDRequiredAndDeleteMarkers`。MinIO に対して: `minio_test.go`(`TestMinIOPutLockExtend`、`TestMinIOVerifyUnexpectedAndDeleteMarker`) | 一部 | MinIO に対する 2 題と `TestMinIOExtendTargetB`・`TestMinIOProbeTry` は SKIP(試験用の MinIO が要る。未実行)。保持中の版の削除・保持の短縮の拒否、`UNEXPECTED_VERSION`、`DELETE_MARKER_FOUND` を実物の MinIO で確かめた記録はここに無い。U1f-2・主経路の記録(`stage3_retention_test.go`)が受入試験の環境で確かめる |
| 2 extend と prune | `TestExtendAOnlyRetainedReferences`、`TestExtendBProtectedAndSharedObjects`、`TestExtendAbortKeepsRecordsOfFinished`、`TestPruneDeletesOnlyExpiredUnreferenced`、`TestPruneFailsClosedHeldAndNotSealed`、`TestPruneRechecksHoldBeforeEachDelete`、`TestPruneIncompleteAndEmptyGenerationsExpireProtectedStays`、`TestPruneLiveLockVersionStays`、`TestContractRetentionPruneSkippedLocked`、`TestContractRetentionPruneHeld` | 緑 | |
| 3 時計のずれ | `TestTimeSkewStopsWritesAndRetentionOps`、`TestOffsetClockRetainUntil`(`objectstore`)、`TestRetentionOpsStopOnSkew`、`TestExtendStopsOnSkewMidway`(`review2`)、`TestContractRetentionPruneUsesDatePlusOffset`、`TestContractObjectStoreBucketConfigStoreTime` | 緑 | |
| 4 取り消された文脈で RUN_ABORTED | `TestPutAbortedWritesNothing`(`objectstore`)、`TestContractRetentionAborted`、`TestExtendAbortKeepsRecordsOfFinished`、a/TestContractAcquireRunAborted | 緑 | 本文で確認(maps2)。保管先: `TestPutAbortedWritesNothing`(要求を 1 件も送らない)・`TestContractRetentionAborted`(保管先が変わらない)。取得と索引: `a/TestContractAcquireRunAborted`(`internal/acquire/contract_acquire_test.go`。保管先・アダプタ・索引・台帳へ進まず `RUN_ABORTED`。`Lease` の偽物は使っていない) |
| 5 | — | 範囲外(0.5 で削除) | |
| 6 verify | `TestVerifyReadsNewVersionsAllAndOldBySample`、`TestVerifyRetentionShortAndLong`、`TestVerifyAbortedKeepsLastVerify`、`TestVerifyMissingSizeAndCounts`、`TestVerifyBoundaryVersionIsFresh`、`TestContractRetentionVerify` | 緑 | |

## 集計(行ごと 7 行)
- 緑 5(共通・2・3・4・6。#4 は maps2 で本文を確認して緑) / 一部 1(#1) / 範囲外 1(#5) / 試験なし 0。
- 未実行: 試験用の MinIO を使う `minio_test.go` の 4 題(SKIP)。
