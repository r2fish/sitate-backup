# U1f-2 の受け入れ・その 1(2026-11-03。`verify`・`extend`・`prune` と `Deps.Prices`)

- 範囲は完了条件の一部。残りの完了条件 2〜6、8 は続きの作業で行う。
- 再実行(新しい環境): 第 3 段 `--- PASS: TestStage3MainPath (75.00s)`(`verify`・`extend`・`prune`、模擬時間 +91 日、削除停止中を実物の MinIO で通す)。Linux 容器で `go vet ./...`、`go test ./...`、`-tags sitate_testbuild ./internal/...`、`internal/cli`・`store` の `-race` は終了 0。`make lint-size` は合格。
- 監督の変異は本文の末尾の結果のとおり(下の追記)。`保護中の世代` の変異は件数の分岐だけを壊す形で、削除の分岐そのものは担当の試験 `TestPruneIncompleteAndEmptyGenerationsExpireProtectedStays` の読みによる。
- 未実装(仕様の範囲を次の作業で確かめる): `verify` の `sampled`、`snapshots[].refs_verified`、`storage.noncurrent_surge`、`journal_chain_ok` のハッシュと先頭の欠け、`journal verify` の入口。
- 第 3 段の台本の 1 行 `w.retention(gens)` は監督が足した。
- 別起動のレビューは未実施。

## 監督の変異の結果

- 赤: 価格表の署名の不合格を見ない(`TestSignedPricesBadOrMissingSignatureMeansNoTable`)。削除停止を見ない(`prune` の 2 か所とも壊したとき、`TestContractRetentionPruneHeld`、`TestPruneFailsClosedHeldAndNotSealed`、`TestPruneRechecksHoldBeforeEachDelete`)。保護中の世代の件数の分岐(`TestPruneDeletesOnlyExpiredUnreferenced` ほか)。
- 緑: 削除停止の検査を始めの 1 か所だけ壊したとき。各削除の直前にも同じ検査があり、結果は変わらないので等価の変異。

## その 2(完了条件 1〜6、8 と D-0168)

- 再実行(main に rebase した後、新しい環境): 第 3 段 2 回とも `--- PASS: TestStage3MainPath`(293.92s、297.03s)。第 2 段は終了 0、第 1 段 `ok`(9.204s)。`make test` は終了 0(`ok` 73 行、FAIL なし)。`make build`、`make build-test`、`make sign-test` は終了 0。`make lint-size`、`make lint-install` は合格。
- 第 3 段の所要時間: 担当の 770 秒は監督の環境で約 295 秒だった(並行の負荷と見る)。76 秒から伸びたのは、調査、`test-tick`、根の変更、手順書の照合を足したため。
- 監督の修正: `proof_confirmed` の実装が mp-core3 と重なったので 1 つにした。`stage3_tick_test.go` は模擬時間が実時間でも進むので 24 時間ちょうどの判定で赤になった。1 日と 2 分の余裕の範囲に直した。
- 担当の変異のログは監督が再現していない(その 1 の分を除く)。
- 未実装(仕様の置き場が無いと担当が報告): `storage.noncurrent_surge`、`head_missing`、保管先の `journal/` の版の照合。`journal verify` はイテレーション 2。`extend`・`prune` の `--dry-run` は `NOT_IMPLEMENTED`。
- `doctor` の `passed` は、台本が管理アクセスを使い続けるので `admin_access_closed` だけ外れる形で判定している。閉じた状態の合格は結線の試験(`make test-app-real`)で確かめた。
- 別起動のレビューは未実施。
