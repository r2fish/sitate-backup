# mp-a2 の覚え書き(仕様の穴・未確認)

- B2 の S3 互換が `X-Amz-Checksum-Crc32` を受けるか(未確認): SDK は `RequestChecksumCalculation=WhenRequired` でも、`PutObjectRetention`(`extend`・`retention_shorten`)と `PutObjectLegalHold` には CRC32 を付ける(送った本文のヘッダーで確認)。B2 が拒否すれば `extend` が通らない。実物の B2 は未実行(キーが無い)ので、利用者が `make test-b2-preflight`(`extend` と `bucket check` を含む)で最初に確かめる。拒否の形が 4xx なら、`retention_shorten` などの試しは拒否として合格するが、正当な `extend` は INTERNAL で止まる。
- spec 3.7 の「接続先の記録(`connection.json`)」: 実装の記録は `ssh-target.json`(`TargetStore`)。案内の出し分けはこの記録の有無で行った。仕様書の名前と実装のファイル名が違う(挙動は同じ)。
- 「会話の履歴が無い」の実装: 人の発話の行がチャット欄に 1 つも無いこと(会話の履歴は主プロセスのメモリにあり、保存しない)。エージェントの失敗の知らせ(notice)の行だけでは案内を消さない。
- 手順 5 の「書き込み鍵の版指定なしの削除の要素を除く」は手順書の注記から読む(TS は `step-judge.ts`、Go は `test/e2e/stepdoc.go`)。`internal/keys` の `bucket check` は元から `KEY_ALLOWS_UNVERSIONED_DELETE`(警告)で、`KEY_TOO_POWERFUL` に含めない。
- `LLM_RATE_LIMITED` の待ち(60 秒、5 回まで)は測って決めた値ではない。初回の実行の様子を見て直す。
