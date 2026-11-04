# U1f-1 完了条件 → 試験の対応表(作業指示書 5.17。テスト仕様書 6.1 の 1)

作成: 2026-11-04(期間中)。作業ツリー wp/maps(開始 commit f503da6)。試験は変えていない。
実行(コンテナ内、逐語の集計は各パッケージの `ok` 行): `make shell-linux CMD='go test -count=1 -v ./internal/... ./adapters/... ./data/... ./cmd/... ./tools/...'` と、同じに `-tags sitate_testbuild`。両方とも FAIL 0。ログは作業の scratchpad の `maps-go-default.log`・`maps-go-tb.log`。
試験名は関数名。`c/` = `internal/cli/` の `*_test.go`、`s/` = `internal/state/`、`ap/` = `internal/approval/`、`a/` = `internal/acquire/`。下の「緑」は上の実行で `--- PASS`(skip を緑に数えない)。
判定: 緑 / 一部(条件の一部だけ試験あり。欠けた部分は備考) / 試験なし / 未実行 / 範囲外。

| # | 試験 | 結果 | 備考 |
|---|---|---|---|
| 1 契約試験 | `c/contract_cli_test.go`(`TestContractCLIEnvelopeSuccess` ほか 5)、`contract_gate_test.go`、`contract_product_test.go`、`contract_seams_test.go`(`TestContractLeaseAcquire`、`TestContractSecretsPut` ほか)、`contract_testbuild_test.go`、`s/contract_state_test.go`、`s/contract_nildeps_test.go` | 一部 | `TestContractStateRecordReadStart`・`TestContractStateSettleInterrupted` は SKIP(読み出しの精算。イテレーション 3。7 章)。skip は緑に数えない。受け入れの記録の「pass 31、skip 2」と一致 |
| 2 build / build-test | `c/TestContractCLIProductBuild`、`c/TestContractCLITestBuild`(`/result/build`)、`cmd/sitate: TestBinaryWarnsMlockFailedInEnvelope` | 一部 | 静的リンクと `make build` の検査(3.1)は `make build` が行う。この調査では実行していない(未実行。受け入れの記録: 監督が成功と記録) |
| 3 製品ビルドの拒否 | `c/TestContractCLITestTickRejectedInProductBuild`、`s/TestValidateDefIssues`(表に「未知のキー」の行) | 一部 | 製品ビルドが保持日数・時計のオフセット等の引数を終了 2 で拒否すること全項目の題は特定できなかった。定義ファイルの未知の項目は `s/def_test.go` の表(未知のキー)が確かめる |
| 4 test-tick --days 35 | `c/TestTestTickDays35Counts`(`testtick_days_test.go`) | 緑 | 期待回数の表との照合はこの題の本文(題から読み取り。全 11 単位の数を逐一は確認していない) |
| 5-1 同時起動の一方が LOCKED | `c/TestMultiProcLockedThenKilled`(`multiproc_test.go`)、`c/TestRunFlowLocked` | 緑 | |
| 5-2 SIGKILL 後の再実行 | `c/TestMultiProcLockedThenKilled`、`c/TestRunFlowInterruptedPrev`、`c/TestRunCleanupAfterAbortedRun`、`c/TestRunCleanupCalledEveryLockedRun` | 緑 | 4 点の SIGKILL の精算の試験はイテレーション 3(範囲外) |
| 5-3 SIGSTOP と run.stuck | `c/TestMultiProcStoppedOwnerLockedThenStuckOnce`、`c/TestRunFlowStuck`、`c/TestRunStuckThresholdIsDeadlinePlusGracePlusRecord`、`s/TestEmitMergeWarn`、`s/TestEmitChangeFailedMergeKeys` | 緑 | |
| 5-4 close-on-exec・PDEATHSIG | `c/TestLockFDIsCloseOnExec`、`c/TestMultiProcChildDiesWithOwner` | 緑 | |
| 5-5 GC | `c/TestLockSurvivesGC` | 緑 | |
| 5-6 監視の goroutine | `c/TestRunFlowWatchdogAborts`、`c/TestWatchdogWaitsForGraceAndKeepsCompletion` | 緑 | |
| 5-7 記録の締め切りの外 | `c/TestRemoteWriteFailureKeepsLocalRunEndAndNextRunSyncs`、`c/TestSyncRemoteHangIsAbortedByDeadline`、`c/TestSyncRemoteAfterMarker` | 緑 | |
| 5-8 | — | 範囲外(0.5 で削除) | |
| 5-9 ロック基盤の失敗 | `c/TestLockUnavailableOnFlockFailure` | 緑 | |
| 5-10 ロック中の cancel・hold set・propose・status | `c/TestLocklessOpsCompleteWhileRunLockHeld` | 緑 | |
| 5-11 再開で直ちに超過 | `c/TestMultiProcResumedAfterLimitAbortsImmediately` | 緑 | |
| 5-12 SIGTERM | `c/TestRunFlowSigterm` | 緑 | |
| 5-13 同じ run_id の終了の項目 | `c/TestRunFlowRunIDCompleted` | 緑 | |
| 5-14 LOCKED 経路の自動の停止 | `c/TestAutoHoldRunsOnLockedTimerRunOnly`、`c/TestAutoHoldBelowThresholdDoesNothingAndHealthySnapshotIsTheBase` | 緑 | |
| 5-15 Timers の TimeoutStartSec・timeout -k・--invoked-by | `s/TestTimersOuterLimitAndInvokedBy`、`s/TestTimersSystemdServiceHasMemoryLimits`、`s/TestTimersCronInstall`、`c/TestContractCLIInvokedByTimerAccepted` | 緑 | |
| 5-16 state.lock 取れず LOCKED | `c/TestStateLockTimeoutIsLocked` | 緑 | |
| 5-17 PENDING_CHAIN_BROKEN | `ap/TestChainBroken`(propose・approve・apply で返る) | 一部 | 「`apply` が登録の応答を止めている間に並行の `propose`」と「`verify` 経路の警告」の題は特定できなかった |
| 6 gate の秘密の 2 レコード | `c/TestGateSecretFDToFirstRecord`、`c/TestGateNoSecretOneRecord`、`c/TestDirectSecretFD`、`c/TestSecretsStdinThenFileDash`、`c/TestDeliverPhraseWipesAndNeverAttachesToFailure`、`c/TestFinishWipesPhraseOnErrorAndWithoutOutput`、`c/TestAbortWipesPhrase` | 緑 | |
| 7 approval の偽物の操作列 | `c/TestApplyRunStepPassesArgsAndApplyCallSecrets`、`c/TestApplyStepFailureIsRecordedAndListedAsFailed`、`c/TestApplySealDefineSwitchesAndWritesDefFile` | 一部 | 偽の `approval` が返す操作列を各部品の偽物に渡す配線の題は特定できなかった |
| 8 estimate --endpoint | `c/TestEstimateEndpointWithoutDefinition`、`c/TestEstimateServerMonthlyAndEndpointOverridesDefinition`、`c/TestEstimateTiersAndUnknownEndpoint`、`c/TestEstimatePriceTableAndArgs` | 一部 | `--dump-gb` の既定(直近 30 日の記録が無いとき)の題は特定できなかった。条件の後半の本文は読み切っていない |
| 9 | — | 範囲外(0.5 で削除) | |
| 10 共有の値 | `c/TestContractCLITestTickNeedsSimTime`(値が無い・壊れているとき終了 2、`detail.reason: format`)、`c/TestStoreClockUsesStoreTimePlusOffset` | 一部 | `test-tick` が書き換えた値を別に起動した核のプロセスが読むこと、起点を置くだけでタイマーを起動しないことの題は特定できなかった |
| 11 KIND=fake の偽の保管先で test-tick | `c/TestTestTickAdvancesFakeStoreRetention`(試験ビルド)、`test/mainpath: TestStage1FakeTickPrune` | 未実行 | 前者はこの実行で SKIP(「偽の保管先のサーバーが無い」。`make test-env-up NAME=U1f-1 KIND=fake` が要る)。後者も受入試験の環境が要る。skip は緑に数えない。受け入れの記録は別の機会の実行を主張 |
| 12 def validate --file - | `c/TestDefValidateStdinFileDryRunShow`、`c/TestDefValidateInvalid`、`c/TestDefValidateAfterSecretsLine`、`c/TestDefValidateBeforeSealReturnsPendingRefsAndStagesCandidate` | 緑 | `/etc/sitate/sitate.yaml` を書かないことは `c/TestApplySealDefineSwitchesAndWritesDefFile` 側。本文の後半は読み切っていない |
| 13 Notifier | `s/TestNotifierRetriesWithinRun`、`s/TestNotifierOneRecipientFailureDoesNotStopOthers`、`s/TestContractNotifierDeliver`、`s/TestContractNotifierResend`、`c/TestNotifySet`、`c/TestNotifyTest`、`c/TestNotifyTestRejected`、`s/testnotifier_*_test.go`(3 題) | 一部 | `notify.pending[]` の次の実行での再送の題は契約試験の `Resend` のみ。A6 の (6) の遅延の延長は A6 の記録 |
| 14 | — | 範囲外(0.5 で削除) | |
| 15 索引の削除 | `s/TestSwitchDefSealAndIndexRules`、`a/TestContractIndexRemove`(U1c)、`c/TestApplySealDefineSwitchesAndWritesDefFile` | 一部 | 根の変更・所在/ホスト鍵の変更・変わらない `id` の索引が残る、の 3 区別を `define` の排他の中で確かめる cli の題は特定できなかった |
| 16 hold-release | `c/TestApplyHoldReleaseMovesOnlyPlannedCausesAndKeepsHoldWhenOthersRemain`、`c/TestApplyHoldReleaseReleasesWhenAllCausesPlanned`、`c/TestAutoHoldHonoursSuppressedAndDoesNotRepeat`、`c/TestPlannerHoldReleaseAndNotifyProofStdin` | 緑 | |
| 17 run_end の targets[] | `c/TestBackupRunEndTargetsRecordsErrors`、`c/TestResultTargetsCarryDetailAndNext` | 緑 | |
| 18 define の中断後の再開 | `c/TestApplyResumeAfterSwitchBeforeDefFileWrite`、`c/TestApplyResumeBeforeDefine`、`c/TestApplyAbortedContextEndsWithRunAbortedAndKeepsApplying`、`c/TestApplyDefFileWriteFailureKeepsApplying` | 一部 | SIGKILL の実プロセスでの試験ではない(題から。文脈の取り消しと再実行)。索引の削除・`host_keys[]` の独立の比較の題は特定できなかった |
| 19 対象ごとの接続の失敗 | `c/TestBackupPartialAndAllFailed`、`c/TestBackupIngestAllFailedWritesNothing`、`c/TestBackupIngestWritesManifestAndRecordsSnapshot`、`c/TestBackupPartialOnSkippedAndLenOf`、`s/TestBackupPartialSeverityWithTargetEmpty` | 緑 | |
| 20 source 節 | `s/TestValidateDefDeclinedAndRefs`、`s/TestValidateDefTargetKindChange`、`a/contract_enroll_test.go`(`root_forbidden`) | 一部 | `seal` の後の `source.declined` の増減が `source-change` になる題は、`TestValidateDefTargetKindChange` が近いが種別まで未確認 |
| 21 source_seen[]・source_clusters | `s/def_test.go`(`source_clusters` と `receiver_hash` を登録の結果として書く行)、`s/TestContractStateUpdateGet` | 一部 | `source_seen[]` を接続先ごとに置き換える・消す `StateFiles` の操作を直接確かめる題は特定できなかった(`internal/state` に `source_seen` を扱う試験が無い) |
| 22 report --weekly | `c/TestReportWeeklyDigestCarriesDeclinedIDsNotNames` | 緑 | |
| 23 健常性チェックの無い回 | `c/TestStatusLastBackupWithoutChecks`、`c/backup_target_detail_test.go`(`checks: []`) | 緑 | |
| 24 主経路の第 2 段 | `c/TestDefaultDepsFillsEveryField`、`c/TestDefaultDepsResolvesSecretRefs`、`c/TestWireRunDefBeforeSealReadsCandidateForBackupAndRestoreTest`、`c/TestWiredProbeTriesUnversionedDeleteAndLegalHold`、`test/mainpath`(Stage1〜3) | 一部 | 結線の単体は緑。実物の通し(`test/mainpath`)は受入試験の環境が要り未実行(主経路の記録: `test/records/mainpath/`) |
| D-0159 estimate | `c/TestEstimateMissingClassIsPriceUnknown`、`c/TestEstimateOpsAllCoversEveryOperation`、`c/TestEstimateCharDigitFreeTierNotUsed`、`c/TestEstimateMinimumFeeRowIsPriceUnknown`、`c/TestEstimateBeyondLastTierIsPriceUnknown`、`c/TestEstimateRealPricesB2`、`c/TestSignedPricesVerifiedTableIsUsed` | 緑 | |
| D-0159 doctor | `c/TestProbeTimerPermissionSystemdLinger`、`c/TestProbeTimerPermissionCrontab`、`c/TestProbeTimerPermissionNoSystemctlIsCron`、`c/TestProbeAdapterDirModes`、`c/TestProbeAdapterDirMissing` | 緑 | |
| D-0160 | `c/TestDefValidateBeforeSealReturnsPendingRefsAndStagesCandidate`、`c/TestDefValidateBeforeSealResolvedRefsGiveEmptyPending`、`c/TestDefValidateBeforeSealOtherUnresolvedRefIsInvalid`、`c/TestProposeSecretsRefMustBeInNotify` | 一部 | `propose seal` が未解決で提案を作らない・`bucket check` が `--key-ref` 省略で候補を使う題は特定できなかった |
| D-0162 | `s/TestSshHostDestRefResolvesByEnrollmentRecord`、`s/TestSshHostDestRefIsNotCheckedAfterSeal`、`s/TestOtherDestAdapterStillResolvesBySecretFile`、`internal/keys/journal_master_test.go`(`journal/pre-master`) | 緑 | `journal_master_test.go` は U1g の領域。題名は未列挙 |
| D-0163 | `cmd/sitate: TestBinaryWarnsMlockFailedInEnvelope`、`internal/keys/memguard_linux_test.go`(`MLOCK_FAILED`)、`c/TestTimerBackupSkippedBeforeSealWhileSetupIncomplete`、`c/TestTimerSkippedResultPassesEnvelopeContract`、`test/mainpath/stage2_bootstrap_test.go` | 一部 | `limits.d` の memlock と実物の sshd 経由で `MLOCK_FAILED` が出ない確かめは第 3 段か A2 の実機(未実行) |

## 集計(行ごと。完了条件 24 + 5 の細目 17 + 追加 4 = 45 行)
- 緑 25 / 一部 16 / 未実行 1(#11) / 範囲外 3(#9・#14・5-8) / 試験が全く無い行 0。
- 未実行: #11(偽の保管先のサーバーが要る)、#2 の `make build` の検査、#24 と D-0163 の実物の部分(受入試験の環境が要る)。
- 「一部」の欠け(上の備考)は、題から対応する試験を特定できなかった部分で、推測では埋めていない。特に #21(`source_seen[]` の `StateFiles` 操作)は試験が見当たらない。
