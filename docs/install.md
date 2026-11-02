# 導入手順書(骨格)

<!-- sitate:not-implemented -->

手順 1 の期待値の配布用の鍵の指紋の位置の印: {{DIST_KEY_FINGERPRINT}}

本文は WP U4 が書く。U4 は上の未実装の印の行を消し、下の記法に従って本文を書く。`make lint-install` が検査する。

## 手順の表の記法(見本)

手順の表は見出し行を下の見本と同じにし、1 手順 1 行で書く。見本の行は `tools/lintinstall` の試験が検査するので、記法を変えるときは見本も直す。見本の手順番号 A1〜A5 は本文では 1〜13 にする。

- 実行するもの: 「、」か「→」で区切った要素の並び。要素は次のどれか。丸括弧の注記(入れ子を含む)は読まない。
  - 道具: コード片の `sitate …`(公開区分 tool。`bootstrap` を含む)。前後に地の文を添えてよい。
  - `Qn`、`Hn`。
  - 面の操作: `面:` で始める(要素ごとに付ける)。`sitate …` を書くなら公開区分は tool か ui-only。
  - 製品の外の人の作業: `外:` で始める。
  - 確認だけ: `確認だけ` の 1 語。
- 完了確認コマンド: コード片の `sitate …`(公開区分 tool)。複数のときは並べて書く。
- 期待する値: 「`Pointer` 演算子 `値`」の要素を「、」で並べる。演算子は `=`(等式)、`⊇`(配列が値の集合を含む)、`⊆`(許容一覧の外を含まない)。値は `<i>` の全要素の指定か `{{名前}}` の印でもよい。配列やオブジェクトそのものの Pointer は、その下の欄が送る欄の表にあれば書ける。
- 完了確認コマンドが 2 つ以上のとき、期待する値は「1 つ目: …。2 つ目: …」と、コマンドの並びの順に区間を分ける。区間の Pointer はその番号のコマンドの結果で検査する。
- `--dry-run` のコマンドの結果の Pointer も、そのサブコマンドの結果スキーマで検査する。
- 失敗時: 原因コードをコード片で書く。核の原因コードに加え、アプリの原因コード(`SSH_UNREACHABLE` など)も書ける。
- 失敗時: 空にしない(原因コードが無い手順は `—` と書く)。
- 印: `{{DIST_KEY_FINGERPRINT}}`(`/result/signing_key_fp` の `=` の右辺。手順 1 の期待する値に必ず置く。指紋は直書きしない)、`{{TIMER_NAMES}}`(`/result/schedule` の `⊇`)、`{{WARNING_ALLOWLIST}}`(`/warnings/<i>/code` の `⊆`)の 3 つだけ。それ以外の名前の印は本文のどこに書いても落ちる。印の値の定義は本文に 1 行で書く。
- 本文は手順 1〜13 の行がすべてあること。表の見出し行は下の見本と一字も違えない(「手順」と「完了確認コマンド」を含む別の見出しの表は落ちる)。

| 手順 | 目的 | 実行するもの | 完了確認コマンド | 期待する値 | 失敗時 |
|---|---|---|---|---|---|
| A1 | 道具の結果を 1 つの等式で確かめる | 定義を組み立てて `sitate def validate --file -` に渡す | `sitate def validate --file -` | `/ok` = `true` | `DEF_INVALID` → 定義を直して出し直す |
| A2 | 完了確認が 2 つのとき、区間で分ける | `bootstrap`(括弧の注記(入れ子)は読まない)、`sitate init --approval-pubkey <base64> --secret-fd ...` | `sitate version` と `sitate doctor` | 1 つ目: `/ok` = `true`、`/result/signing_key_fp` = `{{DIST_KEY_FINGERPRINT}}`。2 つ目: `/result/passed` = `true` | `SSH_UNREACHABLE` → 接続先を確かめる |
| A3 | 実行前の確認の結果と配列そのものを確かめる | Q1 → `sitate init-storage --dry-run --endpoint ... --bucket ... --admin-ref ...` | `sitate init-storage --dry-run --endpoint ... --bucket ... --admin-ref ...` | `/result/plan/errors` = `[]` | — |
| A4 | 全要素の等式と、集合の包含を確かめる | 面: `sitate phrase verify`、外: 人が別の場所で行う作業、H2 | `sitate status` | `/result/kit/confirmed` = `true`、`/result/schedule` ⊇ `{{TIMER_NAMES}}`、`/warnings/<i>/code` ⊆ `{{WARNING_ALLOWLIST}}` | — |
| A5 | 確認だけの手順と、別の欄への参照 | 確認だけ | `sitate bucket check` | `/result/permissions/<i>/actual` = `/result/permissions/<i>/expected` | — |
