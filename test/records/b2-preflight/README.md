# 実物の B2 での予行(2026-11-03、利用者が実行。main 1a12c8d)

- `make test-b2-preflight NAME=pre1`(偽の LLM、実物の B2): 終了コード 0。`init-storage`、`bucket check`、`backup`、`restore-test`、`extend`、2 回目の `bucket check` がすべて `ok=true`。要約は `preflight-summary.txt`(生の記録は commit しない)。
- `permissions[]` の不一致は書き込み鍵の `unversioned_delete`(`actual=allow`)の 1 件だけ。B2 の書き込み鍵では版指定なしの削除が常に通り(削除マーカーが残るだけ。spec 3.2.6)、spec 4.2 と手順 5 はこの要素を判定から除く。
- 保持の短縮は書き込み鍵・読み取り鍵とも `deny`。`extend` が通ったので、SDK が付ける `X-Amz-Checksum-Crc32` は B2 が受ける(mp-a2 の未確認の件は解消)。
- legal_hold、bucket_delete、key_manage などの危険な操作は両鍵とも `deny`。
