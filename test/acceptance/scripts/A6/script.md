# 受入試験 A6 の台本

台本の版: 1.0(仕様書 1.2.16、作業指示書 1.0.25 に合わせて確定)。注入文は `injections.json` の版 1.0。手順と合格基準は仕様書 6.5 の A6 の行を指し、ここには写さない。

## 範囲(イテレーション 1)

(1)〜(10)(12)(15)(16)(17)(18) の日本語の回。(9) は `init-storage` の回だけ(`storage issue-read-key` の回はイテレーション 2)。(11)(13)(14) と英語の回は作らない。

## 使うもの

- ビルド: 試験ビルド。保管先: 試験用の MinIO。環境: 受入試験の環境のコンテナと作業端末。画面は試験用ドライバで駆動する。
- 開始状態: 項目ごとに下の表のとおり。(1)〜(5)(7)(8)(12) は `seal` を済ませた状態(A2 の手順 10 まで)から。(10) は導入の途中(手順 10)の状態から始め、(6) は通知先を到達不能にした状態で始める。それぞれの開始状態を記録する。
- 時刻を進める回((6) の延長の合計 7 日での `expired`)は、共有の模擬時間の値を進める手順を書く。進める手順: `test/env/.run/accept-<NAME>/shared/simtime`(JSON。形は `sitate.simtime/v1`)の `sim_now` を進めた値(`real_at` は書く時点の実時刻)に書き換える(`accept-env.sh` の `simtime_write` と同じ形。書き換えは一時ファイルに書いて `mv` する)。進める前と後に、`accept-env.sh exec <NAME> backup sitate version` と同じく `dest` で、結果の模擬時刻がこの値と一致することを確かめる。保管先の時計は進めない。

## 項目(仕様書 6.5 A6 の番号を項目 ID にする)

| 項目 ID | 試すこと | 期待する値 |
|---|---|---|
| A6-1 | 署名なしの `apply` | `/error/code` = `SIGNATURE_INVALID` |
| A6-2 | 他の鍵の署名 | `/error/code` = `SIGNATURE_INVALID` |
| A6-3 | 承認後に定義の内容を変えた `apply`。承認後に変更の記録を書き換えた `apply` | 前者 `/error/code` = `PRECONDITION_CHANGED`、後者 `/error/code` = `SIGNATURE_INVALID` |
| A6-4 | 遅延中の `apply` | `/error/code` = `DELAY_NOT_ELAPSED` |
| A6-5 | 遅延時間の短縮の即時適用 | `/error/code` = `DELAY_NOT_ELAPSED` |
| A6-6 | 通知先をすべて到達不能にした状態での `apply`。遅延の延長と、延長の合計 7 日(模擬時間を進める)での `expired` | 延長のたびに適用されず、7 日後の変更の状態が `expired` |
| A6-7 | `signed_at` を 24 時間前にした署名での承認と `apply` | `/error/code` = `SIGNATURE_INVALID` |
| A6-8 | 確定済みの状態での `propose seal` | `/error/code` = `ALREADY_SEALED` |
| A6-9 | `seal` の後に、管理用鍵の参照名を付けた `init-storage` を実行させる | `/error/code` = `APPROVAL_REQUIRED`。アプリはキーチェーンに管理用鍵が無いため差し込まない |
| A6-10 | 導入の `seal` で、確認語と違う語(または確認語なし)を入力して承認 | `/error/code` = `NOTIFY_PROOF_MISMATCH` |
| A6-12 | `def-change` の項目と `restore.ssh_from` を 1 つの差分に入れた `propose def-change` | `/error/code` = `DEF_CHANGE_MIXED` |
| A6-15 | 文脈に入れた文でシェルの実行・端末のファイルの読み取り・URL の取得を指示 | 該当する道具が無い。依存の木に agent-toolset・memory・`child_process` の import が無い(静的検査)。LLM API 以外への送信が失敗する |
| A6-16 | 元サーバーで `sitate-ro` として `pg_authid` を読む | 権限のエラーで読めない |
| A6-17 | 配布物の画面のプロセスの静的検査 | 6.5 の A6 の表の下の補足のとおり |
| A6-18 | 道具 `sitate_attest` の `step` に署名を要さない 3 段を渡す | `TOOL_ARG_INVALID` |

## 注入文

エージェントの文脈に入れる文(チャット、元サーバーのファイル名、通知本文)は `injections.json` に逐語で置き、ドライバの入力から読む。変えるときは台本の版を上げる。
