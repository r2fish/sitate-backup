# U1d 完了条件 → 試験の対応表(作業指示書 5.5。テスト仕様書 6.1 の 1)

作成: 2026-11-04(期間中)。作業ツリー wp/maps(開始 commit f503da6)。試験は変えていない。
実行: `make shell-linux CMD='go test -count=1 -v ./internal/... ./adapters/... ./data/... ./cmd/... ./tools/...'`(既定のタグと `-tags sitate_testbuild`)。`internal/approval` は `ok`、FAIL 0。ログは作業の scratchpad の `maps-go-default.log`・`maps-go-tb.log`。
試験名は関数名で、場所は `internal/approval/` の `*_test.go`(他は明記)。SKIP は緑に数えない。

| # | 試験 | 結果 | 備考 |
|---|---|---|---|
| 共通(契約) | `contract_approval_test.go`(`TestContractApprovalGateDenied`/`GateAllowed`/`Propose`/`Pending`/`Cancel`/`Approve`/`Attest`/`ApplyPlan`/`ApplyEnroll`/`ApplyRecord`/`Proposer`/`PendingList`)、`contract_nildeps_test.go` | 緑 | 正規化とハッシュ(JCS・`def_hash`)のベクトルは `internal/contract` 側の契約試験(`contract_approval_test.go` の冒頭の注記)。閾値の定数を変えた核での `def_hash` 不変・既定値を明示した定義との差の題は、題名からは特定できなかった |
| 共通(A6 の核側の拒否) | `TestGateDeniesEvasions`、`TestApproveRejectsTamperedRecord`、`TestApproveSignatureRules`、`TestSealRules`、`TestSealProofRules`、`TestHostKeyPinning`、`TestProposeRejectsFoldedAndUnknownKeys` | 一部 | A6 の項目ごとの対応は A6 の記録(`test/records/A6/`)。ここでは核側の拒否の題だけ |
| 1 gate | `TestGateDeniesEvasions`、`TestGateAllowsAndRewrites`、`TestContractApprovalGateDenied`、`TestContractApprovalGateAllowed`、`internal/cli/gate_test.go`(`TestGateDenied`、`TestGateSecretFDToFirstRecord`) | 緑 | `--invoked-by` を含む引数列の拒否は `gate_test.go` の表(`invoked_by`)。バックアップサーバー自身への `restore` の拒否の題は特定できなかった(本文を読んでいない) |
| 2 pending の状態 | `TestApplyRechecksSignatureAndPrecondition`、`TestApplyResumeAndSingleApplying`、`TestRecoveryShapeAndLocking`、`TestOnlyLockedAppends`、`TestTransitionRules`、`TestAppendEventRejectsBadTransitions`、`TestSameInstanceIsNotInterrupted` | 緑 | `PRECONDITION_CHANGED` で `applying` を書かないことは `TestApplyRechecksSignatureAndPrecondition` の本文を読んでいない |
| 3 cancel は approved まで | `TestApplyCancelAfterApplyingRejected`、`TestContractApprovalCancel`、`TestCancelRacesApplyingAppend`、`TestApplyOnlyApproved` | 緑 | |
| 4 中断の検出 | `TestSameInstanceIsNotInterrupted`、`TestResumeVerifiesRecord`、`TestResumeSwitchCases`、`TestApplyResumeAndSingleApplying`、`TestApplyIDStates` | 緑 | |
| 5 排他の中での版 | `TestOnlyLockedAppends`、`TestRecoveryShapeAndLocking` | 一部 | `state.lock` を持ったまま通常の版を呼ばないことの専用の題は特定できなかった |
| 6 propose の標準入力 | `internal/approval/gate_test.go`(`propose def-change --file -` の表)、`kinds_test.go`(`def-change-unlisted` は `--file -`)、`review_test.go`(105 行付近。候補と違う `def_hash` は `DEF_INVALID`)、`TestProposeSealRefusesUnresolvedRefs`、`TestProposeSealAcceptsResolvedRefs` | 一部 | `--file -` と `--file <path>` が同じ記録になる比較の題は特定できなかった |
| 7 checks-weaken | `TestChecksWeaken`(`gate`)、`TestDefinitionPlanner`、`TestDefDiffMixed` | 緑 | `entropy_sample_bytes` 等の向きの表は `gate_test.go` の 126・131 行付近 |
| 8 define だけの列の再開 | `TestResumeSwitchCases`、`TestApplyResumeAndSingleApplying`、`internal/cli/apply_test.go`(`TestApplyResumeAfterSwitchBeforeDefFileWrite`、`TestApplyResumeBeforeDefine`) | 緑 | 「別の変更を `--id` で指定した `apply` が残った変更を先に再開」は `TestApplyIDStates` が近い(本文は未読) |
| D-0159 | `TestDefDiffMixed`、`TestKindContentMismatch`、`TestDelayBounds`、`TestDelayTable`(`kinds_test.go`)、`TestSecretsOnlyForMatchingID` | 緑 | 8760 時間超の `delay-change` の拒否は `kinds_test.go` の 22 行付近 |
| D-0172(postgresql の要求が port だけ) | U1c の `request_spec_test.go: TestTargetSpecRequestShapeByKind`(U1c の対応表) | 緑 | |

## 集計(行ごと 12 行)
- 緑 9(共通の契約を含む) / 一部 3(共通 A6・#5・#6) / 範囲外 0 / 試験なし 0。
- 題名と注記だけで対応づけた行がある(#1 の restore 拒否、#2、#8 の別 `--id`)。受け入れの前に本文の確認が要る。
