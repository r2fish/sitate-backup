# mp-card の受け入れ(2026-11-03。核の再レビューの指摘)

- 範囲: F1(seal のカードの確認語の欄)、F2(seal のカードの保管先・復元先・通知先)、F3(提案の節の厳密な検査。`ſ` などの畳み込みと未知のキー)、F4(`host_key_fp` だけの変更)、F6(提案の秘密の置き方)、F7(`timeout` の分岐)、U-d(`apply` の管理用資格情報を消す)。F5、U-a、U-b は D-0167 で実装を変えない。
- 再実行(新しい環境、最終の作業ツリー): 第 3 段 `--- PASS: TestStage3MainPath (74.32s)`。Linux 容器で `go vet ./...`、`go test ./...`、`-tags sitate_testbuild ./internal/... ./cmd/...`、`internal/approval`・`cli`・`restore` の `-race` は終了 0。`npm --prefix app test`: tests 231、pass 231、fail 0。`make lint-size` は合格。
- 監督の変異(4 件すべて赤): 秘密の接頭辞と参照名を見ない、管理用資格情報を消さない、`notify` の節を検査しない、`source` などの節を検査しない。
- 契約の変更: `internal/contract/seams.go` の `SSHReasonTimeout` だけ(監督が差分を読んだ)。
- 未検証: アプリの画面での通し(Electron)、F4 の主経路の台本(単体の試験だけ)。実物の `approve` まで通すアプリの試験は wp/mp-app の結線の試験で行う。
- 第 2 段と第 1 段は流していない。第 3 段が第 2 段の経路を含む。
- 別起動のレビューはこの版に未実施。
- 急がない疑義: F1 はアプリが `plan.definition.notify.to` から確認語の欄を作る(核に `proof_required` を足す案もある)。F6 の取り消しの後に残る秘密ファイルは裁定なし。
