# U1f-0 完了条件 → 試験の対応表(作業指示書 5.16b。テスト仕様書 6.1 の 1)

作成: 2026-11-04(期間中)。作業ツリー wp/maps(開始 commit f503da6)。試験は変えていない。
実行: `make shell-linux CMD='go test -count=1 -v ./internal/...'`(既定のタグと `-tags sitate_testbuild`)。`internal/adapter` は `ok`、FAIL 0。ログは作業の scratchpad の `maps-go-default.log`・`maps-go-tb.log`。
試験名は関数名で、場所は `internal/adapter/` の `*_test.go`。SKIP は緑に数えない。

| # | 試験 | 結果 | 備考 |
|---|---|---|---|
| 共通(契約) | `contract_adapter_test.go`(`TestContractRunnerDeclaration`、`...RunOK`、`...RunUnsignedNotStarted`、`...RunContractMismatch`、`...RunOutputInvalid`、`...RunExit1Error`) | 一部 | `TestContractAdapter` と `TestHelperCore` の SKIP は別(`TestHelperCore` はヘルパー、`TestContractAdapter` は adapters/ 側の SKIP)。このパッケージの契約の題は緑 |
| 1 sha256・署名の不一致を起動しない | `TestRunSha256MismatchNotStarted`、`TestRunSha256MissingNotStarted`、`TestRunSignatureRejectedNotStarted`、`TestRunSymlinkExeNotStarted`、`TestContractRunnerRunUnsignedNotStarted` | 緑 | |
| 2 `secrets[]` に無い参照名を落とす | `TestRunSecretsOutsideDeclaredSlotsDropped`、`TestStderrRedactsDroppedSlotValues` | 緑 | |
| 3 標準エラーの制限と秘密の伏せ | `TestRedactForms`、`TestStderrLimitedAndRedacted`、`TestStderrAndSecretsNotInErrors`、`TestStderrTailNeverLost` | 緑 | |
| 4 kind ごとの時限 | `TestLimitForKinds`、`TestKindTimeoutKillsAfterGrace`、`TestKindTimeoutSigtermStops`、`TestRunDeadlineEarlierThanKindLimit`、`TestKindLimitEarlierThanDeadline`、`TestRunContextCancelled` | 緑 | |
| 5 `contract` の版の照合 | `TestRunResultContractMismatch`、`TestContractRunnerRunContractMismatch` | 緑 | |
| 6 終了コードの読み替え | `TestRunExitCodeMapping`、`TestRunExit1NotImplemented`、`TestRunExit1InternalAndAdapterCode`、`TestRunExit1KindSpecificCodeIsAdapterError`、`TestRunExit1AdapterCarriesWholeError`、TestMapAdapterErrorNotImplementedExceptKindCheck | 緑 | 本文で確認(maps2)。`TestRunExit1NotImplemented` は kind notify だけ。kind `check` を除く扱いは `TestMapAdapterErrorNotImplementedExceptKindCheck`(`internal/adapter/exitmap_notimpl_test.go`)を足して確認(変異で赤) |
| 7 子プロセスの環境 | `TestChildEnvIsMinimal`、`TestChildEnvAcquireRecv`、`TestProductBuildDoesNotPassTestVars`(`env_product`)、`TestTestBuildPassesTestVars`(`env_testbuild`、試験ビルドのタグ) | 緑 | `TestTestBuildPassesTestVars` は `-tags sitate_testbuild` の実行で緑 |
| 8 `adapter.json` の `contract` とスキーマ | `TestRunDeclContractAndSchemaNotStarted`、`TestRunRequestSchemaInvalidNotStarted`、`TestContractRunnerDeclaration` | 緑 | |
| 9 trusted comment の期待値 | `TestRunTrustedCommentExpectation` | 緑 | |
| 10 出力の規則 | `TestRunOutputRules`、`TestOutputViolationStopsChild`、`TestContractRunnerRunOutputInvalid`、`TestRunAcquireTruncatedBodyExit0IsOutput`、`TestRunAcquireBadHeaders` | 緑 | `ADAPTER_RESULT_MAX` の境界の試験は条件で持たない |
| 11 | — | 範囲外(0.5 で削除) | |
| 追加(実装側の不変条件: 子プロセスの回収) | `TestGroupKilledAfterNormalExit`、`TestGroupKilledAfterTimeout`、`TestPdeathSigKillsAdapterWhenCoreDies`、`TestWrapCalledAfterVerificationAndRuns`、`TestWrapNotCalledWhenVerificationFails` | 緑 | 作業指示書の条件にない実装の題(参考) |

## 集計(行ごと 13 行)
- 緑 11(追加の行を含む) / 一部 1(共通。`TestHelperCore` はヘルパーの SKIP) / 範囲外 1(#11) / 試験なし 0 / 未実行 0。
