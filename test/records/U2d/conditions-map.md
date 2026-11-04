# U2d 完了条件 → 試験の対応表(作業指示書 5.11。テスト仕様書 6.1 の 1)

作成: 2026-11-04(期間中)。作業ツリー wp/maps(開始 commit f503da6)。試験は変えていない。
実行: `make shell-linux CMD='go test -count=1 -v ./adapters/...'`(全体の `-v` 実行の一部。既定のタグと `-tags sitate_testbuild`)。adapters/restore-dest/ssh-host は `ok`、FAIL 0。ログは作業の scratchpad の `maps-go-default.log`・`maps-go-tb.log`。`make test-adapters ADAPTER=...` そのものは実行していない(同じ `go test` の題で代替)。SKIP は緑に数えない。
試験名は関数名で、場所は `adapters/restore-dest/ssh-host/` の `*_test.go`。

| # | 試験 | 結果 | 備考 |
|---|---|---|---|
| 共通(契約) | `TestContractAdapter` | 緑 | |
| `stop` の `remaining` が空でない場合の結果の形 | `TestFakeStopRemainingShape`、`TestFakeStopAttribution`、`TestFakeStopOnlyOwnSession`、`TestAcceptStopRemaining`(Accept 系) | 一部 | Fake 系(偽の宛先)は緑。`TestAcceptStopRemaining` は SKIP(受入試験の環境が要る) |
| D-0159: `host_key_fp` が違う宛先で初期資格情報を使わず `hostkey_mismatch`、省略で初回のホスト鍵を返す(21-F7) | `TestFakeEnrollHostKeyPinMismatch`、`TestFakeEnrollHostKeyPinMatchAndOmitted`、`TestFakeHostKeyPinned` | 緑 | 本文で確認(maps2)。`TestFakeEnrollHostKeyPinMismatch` は認証の試行 0 回・セッション 0 回・ホームに何も置かれないことまで確かめる |
| D-0159: `observed_from`(`SSH_CONNECTION` の先頭、読めなければ `null`。sudo の下でも読める。15-f) | `TestFakeObservedFromReadBeforeSudo`、`TestFakeEnrollNoObservedFrom`、`TestAcceptEnrollObservedFromAndHostKeyPin`(SKIP) | 一部 | 偽の宛先は緑。実機(`Accept`)は SKIP |
| D-0159: `allow_ssh_from` を省くと受信許可が先頭になる、読めなければ `no_observed_from` | `TestAllowFromRule`、`TestFakeEnrollEmptyAllowIsInvalid`、`TestFakeEnrollNoObservedFrom` | 緑 | |
| D-0159: `stop` の `remaining` がセッションごと、帰属できない処理、「消去」が除く処理(20-f) | `TestFakeStopAttribution`、`TestFakeInspectSweep`、`TestAcceptStopNeverTouchesOtherResources`(SKIP) | 一部 | 条件が求める「U2d の受け入れで実機の 1 回の `restore-test` の `remaining` を確かめる」は実機の記録(`test/records/U2d/`、本調査は読んでいない) |
| 隔離(受信許可・nft・コンテナ) | `TestRenderNft`、`TestRenderNftNeighborDiscovery`、`TestIsolationUnitText`、`TestAcceptIsolationUnit`(SKIP)、`TestAcceptIsolationEffective`(SKIP)、`TestAcceptEgressHostsPinned`(SKIP) | 一部 | 文面の生成は緑。実効の確認(Accept)は SKIP |

## 集計(行ごと 7 行)
- 緑 3(共通を含む) / 一部 4 / 未実行 0 / 試験なし 0。
- `TestAccept*` 14 題はこの実行(開発用コンテナ)ですべて SKIP。受入試験の環境(root と podman が要る)で別に実行する必要がある。
