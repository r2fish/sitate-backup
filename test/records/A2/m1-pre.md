# A2 の測定 1 の前の `make test-e2e`(2026-11-03、main 7cc551a)

- `make test-e2e NAME=e2em1`: 終了 0。`ok github.com/r2fish/sitate-backup/test/e2e`、`--- PASS: TestStage3MainPath (290.81s)`(手順書の手順 1〜13 の完了確認を実物の核で判定する)。
- 測定 1 の 1 回目の環境は `a2r1`(変種 add)。

## 1 回目(無効)と直し(D-0170)

- 1 回目(環境 a2r1、変種 add、実物の LLM と B2): ドライバの終了コード 1。seal の前の `doctor`(段階 `defined`)で手順 13 が合格の印になり、ドライバが手順 13 の印で終えたため、手順 8〜12 の前に打ち切られた。エージェントは Q2 を正しく聞いていた。手順 1〜7 は合格、問い 3 回(台本外 0)、続行の介入 3 回、人の作業 H1〜H3、秘密の漏れ 0。製品の失敗に数えず、無効として同じ回をやり直す(記録は accept の worktree の `test/acceptance/records/A2/m1/1-driver-defect/`)。
- 直し `5a6c69c`: 手順 13 の 2 つ目は `/result/stage` = `sealed` かつ `/result/passed` = `true`(D-0170 と一致)。ドライバは手順 1〜13 すべての合格で終える。`app/test/u3a/mp-steps.test.ts` の試験は条件を外すと赤。
- 直しの後の main で `make test-app-e2e`(偽の LLM、アプリと実物の核の通し): A2・A6 とも終了コード 0。段階 `sealed` の `doctor` は seal の後にしか返らないので、手順 13 の印が seal の前に付かないことはこの通しが含む。
- 試験ビルドは作り直し、`install.md.orig.sha256` と `docs/install.md` の sha256 は一致(`6eb317f95733f344...`)。
