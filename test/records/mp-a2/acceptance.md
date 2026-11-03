# mp-a2 の受け入れ(2026-11-03)

- 範囲: 仕上げのレビューの指摘の直し、D-0169 の「始める」、`prepare-a2`、`make test-b2-preflight`。
- 再実行は最終の main の記録 `test/records/mainpath/final-2026-11-03.md` のとおり。
- 監督の変異は流していない。担当の修正前の赤のログだけ。Go の手順 5 の除外 `TestJudgeSkipWriteUnversionedDelete` は修正前の赤が無い。
- 別起動のレビューはこの版に未実施。
- 未検証: B2 の `X-Amz-Checksum-Crc32` の扱い、実物の B2 の `permissions[]`、実物の LLM の「始める」の後の応答、`LLM_RATE_LIMITED` の待ち(60 秒、5 回)。詳細は `notes.md`。
- 仕様との差(急がない): 仕様 3.7 の `connection.json` は実装では `ssh-target.json`。挙動は同じ。
