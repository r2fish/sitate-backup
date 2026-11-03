# mp-mlock の受け入れ(2026-11-03。D-0163)

- 範囲: `bootstrap` の段 (5) の `/etc/security/limits.d/sitate.conf`(`sitate - memlock unlimited`)と linger の試験、導入中に飛ばした結果の形を `backup`・`restore_test` のスキーマに置く。
- 再実行: Linux 容器の `internal/contract`・`internal/cli` が両タグで ok、`data/prices` が無印で ok。`npm --prefix app test`: tests 223、pass 223、fail 0、skipped 0。
- 変異: 監督は `memlock unlimited` を別の値に変えて app の試験の 1 件が赤になることを確かめた。担当の記録では `reason` の `const` を壊すと `TestResultSamples` と `TestTimerSkippedResultPassesEnvelopeContract` が赤(監督は再現していない)。
- 監督の作業の誤り: 変異を `git checkout` で戻して未 commit の定数の追加まで消した。差分の表示から書き直し、app の試験を流し直した。
- 修正前の赤は取っていない(変異で代えた)。別起動のレビューは未実施。
- ファイル名 `sitate.conf` と書式は仕様に指定が無く、担当の判断。効くかは第 3 段で実物の `sshd` を確かめる。
