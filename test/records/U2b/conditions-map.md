# U2b 完了条件 → 試験の対応表(作業指示書 5.9。テスト仕様書 6.1 の 1)

作成: 2026-11-04(期間中)。作業ツリー wp/maps(開始 commit f503da6)。試験は変えていない。
実行: `make shell-linux CMD='go test -count=1 -v ./adapters/...'`(全体の `-v` 実行の一部。既定のタグと `-tags sitate_testbuild`)。adapters/acquire/postgresql は `ok`、FAIL 0。ログは作業の scratchpad の `maps-go-default.log`・`maps-go-tb.log`。`make test-adapters ADAPTER=...` そのものは実行していない(同じ `go test` の題で代替)。SKIP は緑に数えない。
試験名は関数名で、場所は `adapters/acquire/postgresql/` の `*_test.go`。

| # | 試験 | 結果 | 備考 |
|---|---|---|---|
| 共通(契約) | `TestContractAdapter` | 緑 | |
| ダンプの途中失敗を失敗として返す | `TestDumpFailsMidway` | 緑 | |
| globals を本体より先に送り、`format` が `globals`/`custom` | `TestBothItemsOK`、`TestEmptyBody`、`TestLargeBodyIsFragmented` | 緑 | 本文で確認(maps2)。`TestBothItemsOK`: 受け口の呼び出しが `dump-pg-globals` → `dump-pg` の順 1 回ずつ、dump の並びが globals(`format: globals`)→ custom、本体・ハッシュ・バイト数が一致 |
| globals の失敗は `partial`、`item_ref` が `d:<target-id>:globals` | `TestGlobalsFailureIsPartial`、`TestItemRefHasOnlyFixedParts`、`TestAllItemsFailIsFailed` | 緑 | ロール名・DB 名を使わないことは `TestItemRefHasOnlyFixedParts` |
| 受け口が無い・不一致・不達 | `TestReceiverMissingFailsTarget`、`TestMismatchAndUnreachable`、`TestBadRequests` | 緑 | |
| 実物の PostgreSQL | `TestRealPostgreSQL` | 未実行 | SKIP(PostgreSQL が要る。受入試験の環境) |

## 集計(行ごと 6 行)
- 緑 5 / 未実行 1(`TestRealPostgreSQL`) / 試験なし 0 / 範囲外 0。
- 条件の本文の後半(`materialize` の置き場は核の `restore` が持つ)は U1e の対応表。
