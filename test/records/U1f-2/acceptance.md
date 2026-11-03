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
