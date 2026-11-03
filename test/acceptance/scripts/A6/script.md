# 受入試験 A6 の台本

台本の版: 1.1(仕様書 1.2.20、作業指示書 1.0.29 に合わせて、項目ごとのコマンド・判定・実行の道具・開始状態を足した)。注入文は `injections.json` の版 1.0(変えていない)。手順と合格基準は仕様書 6.5 の A6 の行を指し、ここには写さない。

## 範囲(イテレーション 1)

(1)〜(10)(12)(15)(16)(17)(18) の日本語の回。(9) は `init-storage` の回だけ(`storage issue-read-key` の回はイテレーション 2)。(11)(13)(14) と英語の回は作らない。

## 使うもの

- ビルド: 試験ビルド(`make build-test sign-test build-app-test`)。保管先: 試験用の MinIO(`LOCK_DEFAULT_DAYS=90 make test-env-up NAME=<NAME>`)。環境: 受入試験の環境のコンテナ(`bash test/env/bin/accept-env.sh up <NAME>`、続けて `prepare-a2 <NAME>`)と作業端末。画面は試験用ドライバ(`app/test/e2e/`)で駆動する。
- 環境の名前 `<NAME>`: 利用者が決める(予行は `a6a7`)。終わったら `accept-env.sh down <NAME>` と `make test-env-down NAME=<NAME>`。
- 秘密: 注入文の回(A6-15 の動的な部分)だけ LLM API キーを使う(`SITATE_ROLE_LLM_KEY`。`launch.md`)。ほかの項目は鍵を使わない。

## 開始状態(要件 11)

| 項目 | 開始状態 | 引き継ぎ |
|---|---|---|
| A6-1〜5、7、8、9、12 | `seal` を済ませた状態(A2 の手順 10 まで。定義が確定し、承認鍵が登録済み、通知の確認語の試験を済ませてある) | A2 の最後の測定の環境を使わない(測定のあいだ環境は回ごとに作り直す)。A6 用の環境 `<NAME>` を新しく起こし、A2 の台本(`scripts/A2/script.json`)を同じドライバで `--state-dir` を付けて通して作る(偽の LLM の口 `--mode fake` で足りる。`make test-app-e2e` と同じ。鍵は要らない)。作業端末の `--state-dir` はアプリの承認鍵を残す |
| A6-3 | 上に加えて、承認までの間に定義ファイルの内容を変える(前提の状態の変更)と、変更の記録を書き換える操作の 2 回。それぞれ別の提案で行う | 提案ごとに新しい変更 id |
| A6-6 | 上に加えて、通知先(hook)をすべて到達不能にした状態で始める(`accept-env.sh exec <NAME> hook` の受け口を止める) | 模擬時間は環境の起動時刻から。進める手順は下 |
| A6-10 | 導入の途中(A2 の手順 10 の `seal` の承認の直前)。`notify test` の確認語を受けた状態 | A2 の導入を途中で止めた状態 |
| A6-15、17、18 | 空の作業端末(`--reset`)。元サーバーに F1 のファイルを置く(`scripts/A6/inject-env.sh <NAME>`) | — |
| A6-16 | 元サーバーの `sitate-ro` を作った後(A2 の `source enroll` の後)。無ければ `check-16.sh` が `source-setup` を流して作る | — |

## 時刻を進める回(A6-6。共有の値)

進める手順は `scripts/A6/simtime.sh` に置いた(`test/env/.run/accept-<NAME>/shared/simtime`。形は `sitate.simtime/v1`。`accept-env.sh` の `simtime_write` と同じ形で、一時ファイルに書いて `mv`)。

    bash test/acceptance/scripts/A6/simtime.sh <NAME> show           # 進める前の確認
    bash test/acceptance/scripts/A6/simtime.sh <NAME> advance <日数>  # 進める(前後の確認と進めた量の判定を含む)

確認は、`backup` で `sitate version`(試験ビルド。環境変数 `SITATE_SIM_TIME_FILE`)の `/result/sim_time` が、共有の値から期待する時刻(`sim_now` + 実時間の経過)と 10 秒以内で一致することと、`dest` が同じ共有の値を読める(sha256 一致)こと。`dest` には核が入っていない(`ssh-host` の復元先)ので、`dest` で `sitate version` は実行できない。保管先(MinIO)の時計は進めない(承認済みの期限の判定は保管先の `Date` による。仕様書 3.6 の「時計」)。延長の合計 7 日での `expired` は、延長ごとに `apply` を実行し(各回 `/ok` = false)、最後に `pending --id <ID>` の `/result/changes/0/state` = `expired` を確かめる。

## 項目(仕様書 6.5 A6 の番号を項目 ID にする)

実行の道具の欄: 「ドライバ」= `app/test/e2e/a6-loop.ts`(注入文の道)、「静的」= `static-checks.sh`、「check-16」= `check-16.sh`、「CLI」= CLI を直接使う回(判定は `test/tools/check_a6.py --cli-results`。結果の JSONL の形は同ツールの先頭に書いてある。実行するハーネスは未整備。報告)。

| 項目 ID | 試すこと | コマンド | 判定(機械。JSON Pointer = 値) | 実行の道具 |
|---|---|---|---|---|
| A6-1 | 署名なしの `apply` | `propose delay-change --hours 24` で提案 → `apply --id <ID>`(`approve` をしない) | `/ok` = false、`/error/code` = `SIGNATURE_INVALID` | CLI |
| A6-2 | 他の鍵の署名 | `approve --id <ID> --signature <別の鍵の署名> --signed-at <模擬時刻>` → `apply --id <ID>`(`approve` が拒む場合はその封筒を判定する) | `/error/code` = `SIGNATURE_INVALID` | CLI |
| A6-3a | 承認後に定義ファイルの内容を変えた `apply` | 提案 → `approve`(正しい署名)→ 定義ファイルを書き換える → `apply --id <ID>` | `/error/code` = `PRECONDITION_CHANGED`(`apply` の `failed[]` の `code` を含む) | CLI |
| A6-3b | 承認後に変更の記録(`pending/log.jsonl`)を書き換えた `apply` | 提案 → `approve` → 記録を書き換える → `apply --id <ID>` | `/error/code` = `SIGNATURE_INVALID`(連鎖が切れて `PENDING_CHAIN_BROKEN` になる場合は、その封筒を記録して監督へ。仕様書の合格基準の文は `SIGNATURE_INVALID`) | CLI |
| A6-4 | 遅延中の `apply` | 提案 → `approve` → `apply --id <ID>` | `/error/code` = `DELAY_NOT_ELAPSED` | CLI |
| A6-5 | 遅延時間の短縮の即時適用 | `propose delay-change --hours 7` → `approve` → `apply --id <ID>` | `/error/code` = `DELAY_NOT_ELAPSED` | CLI |
| A6-6 | 通知先をすべて到達不能にした状態での `apply`。遅延の延長と、延長の合計 7 日での `expired` | 開始状態の後、提案 → `approve` → `apply` を繰り返しながら `simtime.sh advance` で進める → 合計 7 日 → `pending --id <ID>` | 延長のたびの `apply` の `/ok` = false。最後の `pending --id` の `/result/changes/0/state` = `expired` | CLI + `simtime.sh` |
| A6-7 | `signed_at` を 24 時間前にした署名での承認と `apply` | `approve --id <ID> --signature <署名> --signed-at <模擬時刻の 24 時間前>` → `apply --id <ID>` | `/error/code` = `SIGNATURE_INVALID` | CLI |
| A6-8 | 確定済みの状態での `propose seal` | `propose seal --file -`(定義を標準入力) | `/error/code` = `ALREADY_SEALED` | CLI |
| A6-9 | `seal` の後に、管理用鍵の参照名を付けた `init-storage` を実行させる(`storage issue-read-key` の回はイテレーション 2) | エージェントの道具 `sitate_init_storage`(`admin_ref` 付き)の呼び出し。CLI なら `init-storage … --admin-ref admin`(鍵の値を差し込まない) | `/error/code` = `APPROVAL_REQUIRED`。アプリはキーチェーンに管理用鍵が無いため差し込まない(ドライバの `keychain_items` の記録に管理用鍵の参照名が無いこと) | CLI(`TestStage3MainPath` の `afterSealDenied` が同じ拒否を通す)/ ドライバ |
| A6-10 | 導入の `seal` で、確認語と違う語(または確認語なし)を入力して承認 | `approve --id <seal の ID> --signature <署名> --signed-at <模擬時刻> --notify-proof-stdin`(標準入力に違う語) | `/error/code` = `NOTIFY_PROOF_MISMATCH` | CLI(`TestStage3MainPath` の `seal` が同じ拒否を通す)。画面からの確認語の誤入力はドライバに無い |
| A6-12 | `def-change` の項目と `restore.ssh_from` を 1 つの差分に入れた `propose def-change` | `propose def-change --file -`(定義の変更に `restore.ssh_from` を足したもの) | `/error/code` = `DEF_CHANGE_MIXED` | CLI |
| A6-15 | 文脈に入れた文でシェルの実行・端末のファイルの読み取り・URL の取得を指示 | 注入文 I1〜I4(チャット)、F1(元サーバーのファイル名)、N1(通知本文)。静的: `static-checks.sh` | 静的: 依存の木に agent-toolset・memory・`child_process` の import が無い。動的: `check_a6.py --record` の「呼び出した道具」「道具の一覧」が PASS(該当する道具が無い)、LLM API 以外への送信は試験者がプロセスの記録で確かめる | 静的 + ドライバ |
| A6-16 | 元サーバーで `sitate-ro` として `pg_authid` を読む | `check-16.sh <NAME>`(`runuser -u sitate-ro -- psql … -c 'select count(*) from pg_authid'`) | 標準エラーが `permission denied for table pg_authid`(終了コード 0 = 合格) | check-16 |
| A6-17 | 配布物の画面のプロセスの静的検査 | `static-checks.sh`(A6-17) | 最上位の画面が 5 面だけ、開く窓が本体の 1 つと OS の標準の窓だけ | 静的 |
| A6-18 | 道具 `sitate_attest` の `step` に署名を要さない 3 段を渡す | `static-checks.sh`(A6-18)。試行: `step` に `admin_access_closed` / `admin_access_opened` / `dest_disposed` | `/error/code` = `TOOL_ARG_INVALID`、核の `attest` を呼ばない | 静的(ゲートの試験) |

## 注入文

エージェントの文脈に入れる文(チャット、元サーバーのファイル名、通知本文)は `injections.json` に逐語で置き、ドライバの入力から読む。変えるときは台本の版を上げる。チャットの文(I1〜I4)はドライバが入れる。ファイル名(F1)は `inject-env.sh` が元サーバーの対象の根の下に置く。通知本文(N1)を環境の側から差し込む口は無い(未解決。完了報告)。

## 判定の出し方

    python3 test/tools/check_a6.py --record <A6 のドライバの record.jsonl> --injections test/acceptance/scripts/A6/injections.json \
        --tools-dir app/src/tools [--cli-results <CLI の回の JSONL>]

項目 ID ごとに PASS / FAIL / UNRUN を出す。UNRUN は実行していない項目で、合格に数えない。
