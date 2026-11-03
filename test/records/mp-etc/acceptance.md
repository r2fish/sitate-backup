# mp-etc の受け入れ(2026-11-03。D-0164)

- 範囲: `bootstrap` の段 (5) の `/etc/sitate`(所有者 `sitate`、モード 0750)、G2(承認の前の `apply --id` は `CHANGE_STATE_INVALID`)、G3(作れない失敗を `SESSION_EXISTS` と分ける)、G5(提案の試験通知の `facts.change_id`)、`restore-test` のメモリの内訳。
- 再実行(main に rebase した後の最終の作業ツリー、新しい環境): 第 3 段 `--- PASS: TestStage3MainPath (58.19s)`(`/etc/sitate` の所有者とモード、`sitate.yaml` の書き込みを判定する)。Linux 容器で `go vet ./...`、`go test ./...`、`-tags sitate_testbuild ./internal/...`、`internal/approval`・`state` の `-race` は終了 0。`npm --prefix app test`: tests 224、pass 224、fail 0。`make lint-size` は合格。
- 監督の変異(2 件とも赤): 作れない失敗も `SESSION_EXISTS` にする、試験通知に `change_id` を載せない。G2 は監督の変異を流していない(担当の修正前の赤のログ `red-mp-etc-*` だけ)。
- 台本の `/etc/sitate` の行は `bootstrap.ts` と同じ形に直して残した。台本は `bootstrap` を自分で行うので、アプリの実物での確認は wp/mp-app の結線の試験で行う。
- `contract.TestNotifier` は `change_id` を受けないので、`approval` は任意の口 `TestFor` を使う(暗黙の結合)。契約に足す案は残す。
- 別起動のレビューは未実施。
- メモリの内訳は `test/records/mainpath/stage3-memory-2026-11-03.md`。`restore-test` の核は Argon2id を呼ばず、復元先の配布物を base64 で台本に埋める `placeScript` が主因。
