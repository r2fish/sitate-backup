# mp-restore の受け入れ(2026-11-03。D-0159)

- 範囲: 復元先の `enroll` の `observed_from`・`host_key_fp`、`--host-key-fp`、`stop` の `remaining` と台帳、次の `restore-test` の始めの呼び直し、`seal` の後の `restore-dest enroll` の拒否。
- 再実行: 受入試験は新しい環境で単独に `ok  	github.com/r2fish/sitate-backup/adapters/restore-dest/ssh-host	111.089s`。第 2 段 `--- PASS: TestStage2MainPath (46.01s)`(`restore.ssh_from` なしの定義)。第 1 段は終了 0。`internal/restore`・`cli`・`contract`・`state` は無印で ok、`restore`・`cli`・`contract` は `sitate_testbuild` で ok、`restore` は `-race` で ok。`make lint-size` は合格。
- 監督の変異(5 件すべて赤): `process` の残存でも台帳を消す、`seal` の後も `enroll` を通す、指紋を照らさず接続する、`--host-key-fp` を無視、`observed_from` を見ない。
- 監督の最初の受入の再実行は 4 件が赤(600 秒のタイムアウト)だった。第 2 段の後の同じ環境で流したのが原因で、第 2 段が復元先の受信を絞っていた。新しい環境で単独に流すと緑なので、試験の順の誤りで製品の欠陥ではない。
- 契約の変更(担当に許可した分。監督が差分を読んだ): `schema/adapter/restore-dest.json`、`gatedata/allowlist.json`、`schema/data/state.json`(`observed_from`)、`testdata/adapter/`。
- 監督が見つけた欠陥: 担当の最初の版は `seal` の後に承認なしで `--host-key-fp` が固定値を置き換えられた。差し戻して直した。同じ種類の穴は `test/records/mainpath/approval-required-audit-2026-11-03.md` で、`source enroll`・`init-storage`・`notify set` は wp/mp-unbypass で直す。
- 別起動のレビューは未実施(まとめレビューで見る)。`stop` の `remaining` の実機の値は `test/records/mainpath/stage3-remaining-2026-11-03.md`。
- 別件: `adapters/restore-dest/ssh-host` の `Fake*Stop*` の 4 件は、同じ uid の他の処理を `remaining` に拾うので、並行の `go test ./...` で落ちる(`-p 1` では緑)。試験の分離の問題で、別に直す。
