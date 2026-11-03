# mp-harden の受け入れ(2026-11-03。まとめレビュー(核)の指摘と D-0165、D-0166)

- 範囲: C1(`restore --to` で状態ディレクトリへ書ける穴)、H1(承認カードの plan)、H2(確定前の `init` の差し替え)、H3(`--dry-run` を無視する入口)、M1〜M5、U1、U2、手順 2・3 の dry-run、`source-change` の `admin_user`、ホスト鍵の取得、`TARGET_EMPTY`。
- 再実行(新しい環境、最終の作業ツリー): 第 3 段 `--- PASS: TestStage3MainPath (74.40s)`、第 2 段 `--- PASS: TestStage2MainPath (46.57s)`、第 1 段 `ok`(9.118s。環境を起動して流した)。Linux 容器で `go vet ./...`、`go test ./...`、`-tags sitate_testbuild ./internal/... ./cmd/...`、主なパッケージの `-race` は終了 0。`npm --prefix app test`: tests 228、pass 228、fail 0。`make lint-size` は合格。
- 監督の変異(6 件すべて赤): 相対の `--to` を通す、`init` の再実行で承認鍵を差し替えられる、食い違う保留を確定する、0 件の対象を `ok` のままにする、読み出しの失敗で古い世代へ進む、`hold set` の dry-run を実行する。
- 再実行の時点で、rebase は `notes.md` の衝突で止まっていた(コードは全部適用済み)。衝突は記録だけで、解いても試験の対象のコードは変わらない。
- `init-storage --from-apply` は `gatedata/allowlist.json` と `subcommands_table.go` に無く、エージェントは付けられない(grep で確かめた)。
- 担当の範囲の外の変更を追認した: `internal/cli/propose_secrets.go`、`apply.go`、`internal/state/emit.go`、`test/mainpath/world_test.go`。契約の変更は担当の報告のとおりで、監督は全部は読んでいない。
- `--to` が絶対パスなら、状態ディレクトリへ向いていても核は拒否しない(`gate` は絶対パスを通さない。notes 8)。`gate` を通らない直接の実行だけが届く。
- 未裁定の notes(1、2、4、6〜9、11、12)は申し送りの 37 節のとおり。別起動の再レビューは統合の後に行う。
