# mp-rest の受け入れ(2026-11-03。D-0159 の残りと鍵のメモリ)

- 範囲: 鍵のメモリ(`prctl(PR_SET_DUMPABLE, 0)`、`mlockall`、`MLOCK_FAILED`)、U2a(`\001` を含むファイル名)、`APPROVAL_DELAY_HOURS_MAX`、アダプタの署名の不一致の `reason: signature`、U3a(手書きの道の `kit confirm`、キーチェーンのサービス名の固定)、`ssh-host` の試験の分離。
- 再実行(main に rebase した後、新しい環境): 第 2 段 `--- PASS: TestStage2MainPath (48.19s)`、第 1 段は終了 0。Linux 容器で `go vet ./...`、並行の `go test ./...` を 2 回、`-tags sitate_testbuild ./...`、主なパッケージの `-race` が終了 0(`ok` の行は 82、FAIL なし)。`npm --prefix app test`: tests 222、pass 222、fail 0、skipped 0。`make lint-size` は合格。
- 監督の変異(6 件すべて赤): `sha256` の不一致でも起動する、署名の不合格を見ない、dumpable を落とさない、`mlock` の失敗を黙る、遅延の上限を緩める、封筒に警告を足さない。
- 監督が足した直し: `warnWriter` の空の書き込みの穴、`def validate` の `approval_delay_hours` の上限を `contract.ApprovalDelayHoursMax` にする(`internal/state/validate.go`、`TestValidateDefIssues` の行)。
- 担当の範囲の外の変更を追認した: `app/src/tools/sitate_restore_dest_enroll.json`。
- 別起動のレビューは未実施(まとめレビューで見る)。
- skip のままの試験: `TestHardenMemoryLocksProcess`(`memlock` が無制限のときだけ。担当が `docker run --ulimit memlock=-1:-1` で確かめた記録があり、監督は再現していない)。`TestSetupFromAndForbiddenAndAncestor`(root と `SITATE_DESTRUCTIVE_SETUP=1`)。`TestSetupAndDBRole` は未実行。
- 未検証: 実物の `gate` 経由の起動で `MLOCK_FAILED` が出るか(第 2 段の台本は判定しない)、macOS の実機のキーチェーン。
- `bootstrap` の linger の試験が無い(`bootstrap.ts:204`)。第 2 段の台本は同じ段を自分で行うので、アプリの実物では未検証。
