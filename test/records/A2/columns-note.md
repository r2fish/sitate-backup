# A2 の記録の欄の書き方の注記(回ごとの欄の導き方。D-0177)

回ごとの欄のうち核の `sitate version`(version・build・signing_key_fp)と M-20・M-22 のアプリの記録は、`a2_fields` の `core_version` と `app_record` を持つドライバの回ではその値をそのまま写す(`app_record` の `runs_elapsed_ms_total` が M-20 の所要時間、`input_tokens`・`output_tokens`・`cache_read_input_tokens`・`models` がトークン数と使用したモデル名、`request_elapsed_ms` の件数・最小・中央値・90 パーセンタイル・最大が M-22 の分布。`core_version` は回の終わりに backup の核で実行した結果で、取れなければ `error` を持つ)。この欄を足す前の回(測定 1)は `record.jsonl` から導ける欄だけを導き、導けなければ推測で埋めず「記録なし」と書く。導けるのは、道具の呼び出しの数(`summary` の `counts.tool_call`)と、所要時間のうちドライバが見た経過(`summary` の `elapsed_ms`。出どころは「ドライバの記録」と書き、M-20 のアプリの記録とは区別する)だけで、核の `sitate version` は同じ回の記録の頭(`header`)に書き写されていなければ「記録なし」、トークン数・使用したモデル名・要求ごとの応答時間は使い捨ての置き場と一緒に消えているので「記録なし」。これらの欄は判定に使わない。

## 欄 `q4_wish_asked`(D-0185)

`a2_fields` の `q4_wish_asked`: Q4 の希望を聞く問い(台本の Q4 の語に当たる問い)が定義の案の確認の前に出たか。`true` は希望を聞く問いが先、`false` は案の確認が先に来てドライバがそれを Q4 として照合し台本の希望を答えた回、`null` は Q4 に答える前に回が終わった。同じ判定の記録(`kind: "q4_wish"`)が `record.jsonl` に 1 行出る。この欄を足す前の回(測定 1・2)は「記録なし」と書く。判定に使わない。

## 欄 `start_input_reasked`(D-0188)

`a2_fields` の `start_input_reasked`: エージェントが開始時の入力(元サーバーとバックアップサーバーの所在・管理アカウント)を求め直し、ドライバが台本の Q と照合せずに開始時の入力と同じ内容を答えた回数。`record.jsonl` には `kind: "start_input_reasked"` の行(`count` 付き)が出る。問い(`questions`・`unscripted_questions`)に数えない。この欄を足す前の回は「記録なし」と書く(記録の `question` の行のうち、元サーバーの接続先を求める問いに Q3 の答えを返した行から導ける)。判定に使わない。
