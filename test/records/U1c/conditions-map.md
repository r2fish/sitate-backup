# U1c 完了条件 → 試験の対応表(作業指示書 5.4。テスト仕様書 6.1 の 1)

作成: 2026-11-04(期間中)。作業ツリー wp/maps(開始 commit f503da6)。試験は変えていない。
実行: `make shell-linux CMD='go test -count=1 -v ./internal/... ./adapters/... ./data/... ./cmd/... ./tools/...'`(両方のタグ)。`internal/acquire` は `ok`、FAIL 0。ログは作業の scratchpad の `maps-go-default.log`・`maps-go-tb.log`。
試験名は関数名で、場所は `internal/acquire/` の `*_test.go`(ファイル名の接頭辞だけ書く)。「緑」はその実行で `--- PASS`。SKIP は緑に数えない。

| # | 試験 | 結果 | 備考 |
|---|---|---|---|
| 共通(契約) | `contract_acquire_test.go`(`TestContractIndexReserve`/`SetVersion`/`Resume`/`TornTail`/`Remove`/`Rebuild`、`TestContractAcquireRun*`、`TestContractRunCleanup`、`TestContractSourceEnroller`)、`contract_survey_test.go`、`contract_check_test.go`、`contract_enroll*_test.go`、`contract_hostkey_test.go`、`contract_nildeps_test.go` | 緑 | ストリーム形式と断片の最大長、`file-retry` のフレーム、`read-batch` の変化の一覧、索引の先行書き込みと中断・再実行のベクトルは `ingest*_test.go`(`TestIngestRangesAcrossChunks`、`TestIngestFileRetryReplacesEarlierRead`、`TestIndexRebuildFromManifests`)と契約の索引の題。容器の起動を伴う `container_podman_test.go` の 4 題(`TestPodmanContainerIsolation` ほか)は SKIP(rootless の容器実行環境が要る。未実行) |
| 共通(容器) | `container_test.go`(`TestContainerArgs`、`TestContainerPrepareRootIsMinimal`、`TestRecvRelay*`)、`relay_limits_test.go` | 緑 | 最小の読み取り専用ルート・外部コマンド無しは `TestContainerPrepareRootIsMinimal`。実際の podman での隔離は SKIP(未実行) |
| 1 Surveyor の調査 | `contract_survey_test.go`(`TestContractAcquireSourceSurvey`、`...Sudo`、`...Failures`)、`source_test.go`(`TestJudgeSurveyVectors`、`TestSourceCheckReportsLargeObjects`)、`TestSurveyorFetchHostKeyUsesNoCredential`(`target_empty_test.go`)、`setup_real_test.go: TestSetupScriptWithRealSourceSetup` | 一部 | 固定の調査の処理・`SURVEY_DU_TIMEOUT_S` の埋め込み・`sizes_complete` の写しを個別に確かめる題名は未確認(`TestContractAcquireSourceSurvey` の本文を読んでいない)。実物の source-setup は SKIP(未実行) |
| 2 候補の判定 | `source_test.go: TestJudgeSurveyVectors`、`TestJudgeCheckVectors`、`contract_check_rules_test.go: TestContractAcquireSourceCheckUncoveredVectors`、`contract_check_test.go`(`...Selftest`、`...Failures`、`...HostKey`) | 緑 | 両方の入口が同じ判定になることはベクトル題の名前から。`db_unsupported` 等の行ごとの確認は未読 |
| 3 ホスト鍵の値の選択 | `contract_hostkey_test.go: TestContractSurveyorSelectHostKey`、`TestContractSurveyorSelectHostKeyUnknownTiming` | 緑 | |
| 4 source survey の入口 | `contract_survey_test.go: TestContractAcquireSourceSurveyHostKey`、`TestContractAcquireSourceSurveyFailures`、`TestContractAcquireSourcePortNot22`、`hostarg_test.go: TestParseHostArgForms` | 一部 | `source_seen[]` を `StateFiles` に書く確認は、`StateFiles` の `source_seen` 操作の試験が `internal/state` に見当たらず(U1f-1 の対応表 #21)、偽物への書き込みの確認の範囲 |
| 5 source enroll の順 | `contract_enroll_test.go`(`...HostDiffers`、`...NoRecord`、`...HostKeyMismatch`、`...UnreadableCluster`、`...RootForbidden`)、`contract_enroll_ok_test.go`(`...Success`、`...SelftestFails`)、`source_enroll_sealed_test.go`、`TestSourceEnrollPortNot22`、`TestEnrollRequiresFrom`、`TestEnrollerApplyDoesNotWriteState` | 緑 | |
| 6 調査の失敗の読み替え | `contract_survey_test.go: TestContractAcquireSourceSurveyFailures`、`TestContractAcquireSourceSurveySudoOS` | 緑 | 終了状態 77・1・127・2 の各行を題が網羅するかは未読 |
| D-0172(postgresql の要求が port だけ) | `request_spec_test.go: TestTargetSpecRequestShapeByKind`、`TestFailedTargetCarriesDetailAndNext` | 緑 | アプリと実物の核をつないだ通しで DB の対象が `ok` になることは実物の環境(`test/records/mainpath/`、未実行) |

## 集計(行ごと 9 行)
- 緑 7 / 一部 2(#1・#4) / 範囲外 0 / 試験が全く無い行 0。「共通(契約)」と「共通(容器)」と D-0172 を含む。
- 未実行: `container_podman_test.go` 4 題、`setup_real_test.go`(rootless の容器実行環境・実物の source-setup が要る)。
- 条件 1・2・6 の「各行を網羅するか」は題名からの判断で、本文を読み切っていない。
