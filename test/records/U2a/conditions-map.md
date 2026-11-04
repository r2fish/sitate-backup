# U2a 完了条件 → 試験の対応表(作業指示書 5.8。テスト仕様書 6.1 の 1)

作成: 2026-11-04(期間中)。作業ツリー wp/maps(開始 commit f503da6)。試験は変えていない。
実行: `make shell-linux CMD='go test -count=1 -v ./adapters/...'` を含む全体(既定のタグと `-tags sitate_testbuild`)。`adapters/acquire/files` と `adapters/acquire/recv` は `ok`、FAIL 0。ログは作業の scratchpad の `maps-go-default.log`・`maps-go-tb.log`。
試験名は関数名で、場所は `adapters/acquire/files/`(以下 files)と `adapters/acquire/recv/`(以下 recv)。SKIP は緑に数えない。root と PostgreSQL と容器が要る受け口の題は、この実行(開発用コンテナ、root でない・PostgreSQL 無し)で SKIP になり、受入試験の環境が要る。

| # | 試験 | 結果 | 備考 |
|---|---|---|---|
| 共通(files のアダプタ) | files: `TestFilesAcquireBasic`、`TestFilesHeaderValues`、`TestFilesIncremental`、`TestFilesNewAndRemoved`、`TestFilesRequestErrors`、`TestFilesPermission`、`TestFilesSeparatorNameIsPermission`(追加 19-b)、`TestFilesRetry*`、`TestFilesBigIndex`、`TestFilesTruncatedTar`、`TestFilesInvalidUTF8Path` ほか、`contract_test.go: TestContractAdapter` | 一部 | `TestContractAdapter`(files)はこの実行では緑(recv の側の `TestContractAdapter` は別パッケージで SKIP)。`make test-adapters ADAPTER=acquire/files` そのものは実行していない(上の `go test` で代替。同じ題) |
| 受け口: 許可した操作だけ・他は終了 126 | recv: `TestRecvRefusesWithAllowlist`、`TestContractRecvRefuses` | 緑 | |
| 受け口: 根の外・シンボリックリンクの先を読まない | recv: `TestRecvReadBatchSafety`、`TestRecvReadBatchInput`、`TestRecvListRefusesSeparatorInLinkOrDirName` | 緑 | |
| 受け口: 読む前後の `stat` の変化の一覧 | recv: `TestRecvReadBatchChanged`、files: `TestFilesIncremental` | 緑 | |
| 受け口: `source-setup` の再実行で何も変わらない | recv: `TestSetupViaSudoThenRootIdempotent` | 未実行 | SKIP(root で実行する受入試験の容器が要る) |
| 受け口: `--rotate` の後に authorized_keys が 1 行 | — | 試験なし | `adapters/acquire/recv` の試験に `rotate` を含むものが無い(`grep -i rotate` が 0 件) |
| 受け口: `selftest`、`dump-pg` の途中失敗、読み取り専用ロールの書き込み拒否 | recv: `TestRecvSelftestTargets`、`TestContractRecvSelftest`(緑)。`TestSelftestConnectRevoked`、`TestSetupAndDBRole`、`TestSetupDBNamesToolsTablespace`、`TestRealPostgreSQL`(adapters/acquire/postgresql) | 未実行 | 緑は `selftest` の対象の確認まで。`dump-pg` の失敗の終了状態・書き込み拒否・DB のロールの権限(`pg_authid` を読めない等)は PostgreSQL が要る題が SKIP |
| 1 調査の処理 | recv: `TestSurveyOutputAndNoResidue`(緑)、`TestRecvScriptsSyntaxNoEval`、`TestSurveyPrereqMissing`、`TestSurveyExit77NotLinux`、`TestSetupPrereqToolMissing`、`TestSetupPrereqUnsupportedOS`。`TestSurveyViaRootAndSudoWritesNothing`、`TestSurveyDuTimeout` | 一部 | `sh -n`・`eval` 無し・出力のスキーマ・prereq・終了状態 77 は緑。root と `sudo -n` で何も書かないこと、`du` の打ち切りで `bytes` が `null` は SKIP(未実行) |
| 2 クラスタの読み方 | recv: `TestSetupAndDBRole`(SKIP)。`TestSetupForbiddenRootsMatchContract`(緑) | 未実行 | 2 クラスタ・WAL を別の場所に置いた構成は PostgreSQL の環境が要る。緑の題は禁止する根の一覧が契約と一致する確認 |
| 3 受け口の `survey` | recv: `TestSurveyOutputAndNoResidue`、`TestContractRecvSelftest` | 一部 | `sitate.recv-survey/v1` のスキーマの適合と DB 名の `null` の題名は特定できなかった |
| 4 `source-setup` の `roots`・`db_role` | recv: `TestSetupFromAndForbiddenAndAncestor`(SKIP)、`TestSetupForbiddenRootsMatchContract`(緑) | 一部 | `root_forbidden` の材料を返して止まり ACL を付けない部分は SKIP(未実行) |
| 追加 D-0159: `\001` を含む名前 | files: `TestFilesSeparatorNameIsPermission`、recv: `TestRecvListSeparatorFileNameIsUnreadableRecord`、`TestRecvListRefusesSeparatorInLinkOrDirName` | 緑 | |
| 追加 D-0159: リンクの根の `realpath` が (i)〜(iii) で `root_forbidden` | recv: `TestSetupFromAndForbiddenAndAncestor`(SKIP) | 未実行 | |
| 追加 D-0159: `from=` が空・不正で `from_missing` | recv: `TestSetupFromAndForbiddenAndAncestor`(SKIP) | 未実行 | 題名からの推測。本文を読んでいない |

## 集計(行ごと 14 行)
- 緑 4 / 一部 4 / 未実行 5 / 試験なし 1(`--rotate`) / 範囲外 0。
- 受け口の root と PostgreSQL が要る題は、受入試験の環境でしか緑にならない(この実行で SKIP)。U2a の受け入れの記録(`test/records/U2a/acceptance.md`、`gaps-2026-11-04.md`)に別の実行がある場合は、そちらの結果で上の「未実行」を置き換える(この調査は読んでいない)。
