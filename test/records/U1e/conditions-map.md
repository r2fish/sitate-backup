# U1e 完了条件 → 試験の対応表(作業指示書 5.6。テスト仕様書 6.1 の 1)

作成: 2026-11-04(期間中)。作業ツリー wp/maps(開始 commit f503da6)。試験は変えていない。
実行: `make shell-linux CMD='go test -count=1 -v ./internal/... ./adapters/... ./data/... ./cmd/... ./tools/...'`(既定のタグと `-tags sitate_testbuild`)。`internal/restore` は `ok`、FAIL 0。ログは作業の scratchpad の `maps-go-default.log`・`maps-go-tb.log`。
試験名は関数名で、場所は `internal/restore/` の `*_test.go`(ファイル名の接頭辞だけ書く)。SKIP は緑に数えない。

| # | 試験 | 結果 | 備考 |
|---|---|---|---|
| 共通(契約) | `contract_restore_test.go`(`TestContractRestoreVerify`、`...Aborted`、`TestContractRunCleanup`、`...UsesLedgerDest`、`...DestEnrollerWriteState`)、`contract_restore_pass_test.go`、`contract_restore_list_test.go`、`contract_restore_dest_test.go`、`contract_restore_keyring_test.go`、`contract_restore_inplace_test.go`、`contract_restore_plan_test.go`、`contract_restore_rt_test.go`、`contract_restore_iso_test.go`、`contract_nildeps_test.go` | 一部 | `TestContractRestoreJournalVerify` は SKIP(`journal verify` はイテレーション 2。7 章)。合格記録のベクトルは `contract_restore_pass_test.go` |
| 1 | — | 範囲外(イテレーション 3。`list --anchor`) | |
| 2 | — | 範囲外(イテレーション 3。D-0178) | |
| 3 | — | 範囲外(イテレーション 3) | |
| 4 | — | 範囲外(0.5 で削除) | |
| 5 RunCleanup | `TestContractRunCleanup`、`TestContractRunCleanupUsesLedgerDest`、`TestRunCleanupUnverifiedKeepsRecord`、`TestRunCleanupOnlyOwnResources`、`TestRunCleanupUnknownInterruptedIncludesDestroyFailed`(`restore_misc`)、`TestRestoreTestTeardownUnverified`、`TestInspectAbsentWithResourcesIsUnverified`(`restore_review_rt`) | 緑 | `TEARDOWN_UNVERIFIED` は `TestRestoreTestTeardownUnverified` ほか |
| 6 試験ビルドの時計 | `TestRestoreListOffsetClock`(`restore_misc`)、`TestContractRestoreListStoredAtMismatch`(`contract_restore_list`) | 一部 | `stored_at_mismatch` を付けないことは緑。保持期限を過ぎた錨が `expired: true` になる部分は、イテレーション 3 へ送った(1.0.33。条件 2・6 の `expired`)ので範囲外 |
| 7 restore の (a) と materialize | `TestContractRestoreRestore`、`TestContractRestoreRestoreToNotEmpty`、`TestRestoreInPlaceMaterializes`、`TestRestoreMismatchOnCorruptAndMissing`、`TestRestoreToMustBeAbsoluteLocalDir`、`TestRestoreDumpWithoutAttributes`、`TestRestoreLargeFileStreams`、`TestContractRestoreVerify` | 緑 | ダンプの固定名は `TestRestoreDumpWithoutAttributes` が近い(本文は未読)。`dumps[]` はイテレーション 2 |
| 8 | — | 範囲外(イテレーション 2。`journal verify`) | |
| 9 | — | 範囲外(イテレーション 3。破棄の再試行)。ただし「次の `restore-test` の始めの呼び直しだけ」は `TestRestoreTestLeftoverSameSessionDefers`、`TestRestoreTestRetriesDestroyFailedFirst` | |
| 10 | — | 範囲外(イテレーション 3) | |
| 11 restore-dest enroll の状態 | `TestContractRestoreDestEnrollerWriteState`、`TestContractRestoreDestEnrollerKind`、`TestEnrollWritesFixedValuesBeforeProbe`、`TestEnrollAfterSealNeedsRestoreChange`、`TestRestoreDestEnrollDryRunWithoutDefinition` | 緑 | |
| 12 合格記録 | `TestContractRestoreTestProof`、`TestContractRestoreTestFailNoProof`、`TestContractRestoreTestIsolationFailed`、`TestContractRestoreTestHostKeyMismatch`、`TestContractRestoreTestPlacementMismatch` | 一部 | `checks[]` を空の配列で書くことの専用の題は特定できなかった(U1f-1 の `TestStatusLastBackupWithoutChecks` が status 側) |
| 13 復元先の uname | `TestContractRestoreTestDestArch`、`TestContractRestoreTestDestArchUnsupported`、`TestProbeScriptToolsMissingFail`(`restore_review_probe`) | 緑 | `unsupported_platform` の表 11 への進み方は `...ArchUnsupported` の本文を読んでいない |
| D-0159 | `restore_d0159_test.go`(`TestEnrollFirstOmitsHostKeyFPAndRecordsObserved`、`TestEnrollWithoutSSHFromOmitsAllowAndRecords`、`TestEnrollObservedNullRecordedNull`、`TestEnrollNoObservedFromIsDefInvalid`、`TestReenrollPinsStoredFPAndHostKeyFPFlagReplaces`、`TestHostKeyFPFlagMismatchDoesNotReplace`、`TestEnrollerPutsProposalFPInRequest`、`TestStartAllowSSHFromRule` ほか) | 緑 | 条件本文の全項目(15-… 21-F7 …)を 1 つずつは照らしていない |

## 集計(行ごと 15 行)
- 緑 5 / 一部 3(共通・#6・#12) / 範囲外 7(#1・#2・#3・#4・#8・#9・#10。#9 は一部の試験を持つが範囲外で数える) / 試験なし 0。
- 未実行: 試験用の MinIO に対する同じ判定(U1f-2 の結合の後。別の記録)。
