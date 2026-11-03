# 直し 2 の上での偽の LLM の A2・A6(変種 add・lo・nocluster。2026-11-04、監督)

branch `wp/fix-db` の 1d581ab(直し 2 の 92ab692 に、偽の LLM の変種の対応 `wp/fake-var` の ae72588 を合わせたもの)。main には入れていない(測定 1 が閉じるまで。D-0172)。LLM API と B2 は使わない(保管先は試験用の MinIO と保管先準備アダプタの代役)。

手順: `make build-test sign-test build-app-test`(`dist/test/install.md.orig.sha256` は `docs/install.md` と一致)、変種ごとに `bash test/env/bin/accept-env.sh up fix-db --variant <v>`、`LOCK_DEFAULT_DAYS=90 make test-env-up NAME=fix-db`、`VARIANT=<v> make test-app-e2e NAME=fix-db`、終わったら両方 down。

| 変種 | A2 の終了コード | A2 の合格した手順(記録の順) | `backup` の対象 | A6 の終了コード |
|---|---|---|---|---|
| add | 0 | 1, 2, 3, 4, 5, 8, 9, 6, 7, 10, 11, 12, 13 | t1〜t5 がすべて `ok`(手順 8 の `actionable` の mount と db を足した分を含む) | 0 |
| lo | 0 | 1, 2, 3, 4, 5, 8, 9, 6, 7, 10, 11, 12, 13 | t1〜t4 がすべて `ok`(ラージオブジェクトを持つ DB を含む。`uncovered` の `large_object` は知らせるだけ) | 0 |
| nocluster | 0 | 1, 2, 3, 4, 5, 8, 9, 6, 7, 10, 11, 12, 13 | t1〜t3 がすべて `ok`(5433 のクラスタは `source.declined`) | 0 |

- 測定 1 の 1 回目で止まった手順 11(DB の対象が `INTERNAL`)は、3 変種とも DB の対象を含んで合格した。
- 偽の LLM は台本の Q4 の文言を見ず、調査の結果の形で定義を作る(`app/test/e2e/fake-definition.ts`)。実物の LLM の回でどの DB が対象になったかは、記録で確かめる。
- 記録の原本は監督の作業用の場所に置き、リポジトリには入れていない(偽の LLM の記録で、秘密の値を含まない)。
