# 受入試験 A7 の台本

台本の版: 1.1(仕様書 1.2.20、作業指示書 1.0.29 に合わせて、検査のコマンドと判定を足した)。手順と合格基準は仕様書 6.5 の A7 の行を指し、ここには写さない。A7 は新しい操作を行わず、A2(測定 1〜2 のすべての回)と A6 の日本語の回の記録を検査する。

## 検査の項目(A7 の番号を項目 ID にする)

| 項目 ID | 検査 | 判定 |
|---|---|---|
| A7-1 | 試験で使った秘密の値(LLM API キー、B2 の鍵、MinIO の資格情報、SSH 秘密鍵、フレーズ、Webhook の URL)を、そのままの文字列と base64 の表記の 2 つで記録から検索して、一致が 0 件 | 検索の件数。`gate` 経由でフレーズを出す操作の標準出力の 2 レコードの検索を含む |
| A7-2 | LLM API へ送った内容の送った欄の JSON Pointer の集合が、送る欄の表(仕様書 3.7)の部分集合である。原因コードごとの `detail` の行は、そのコードのときだけ表に加わる | 送信記録の区画の JSON から欄を抜き出して比べる(`app/src/data/send-fields.json` を表として使う) |
| A7-3 | 合成データの対象の根より下に置いた個人名入りのファイル名とディレクトリ名、`source survey` が返す場所(`/home`・`/srv` の直下)に置いた個人名入りのディレクトリ名と DB 名が、LLM API へ送った内容に 0 件 | 期待値の名前の一覧は `test/env/` の生成器が書き出す JSON のパスで指す |
| A7-4 | 使用したモデル名が送信記録に残る | 送信記録 |

## コマンド(項目 ID ごと)

`test/tools/check_a7.py` に 1 つにまとめた(3 つの部分コマンド)。値を出さず、件数と Pointer・連番だけを出す。

| 項目 ID | コマンド | 判定 |
|---|---|---|
| A7-2 | `python3 test/tools/check_a7.py fields --records <A2 と A6 の record.jsonl またはその親のディレクトリ>… --send-fields app/src/data/send-fields.json --names test/env/.run/accept-<NAME>/expect/source.json` | `A7-2 表に無い欄(サブコマンド, Pointer)の種類=0`、`path_ref` の欄が `pr:<16 桁>` でない=0。道具の結果の区画(`<sitate-data command="…">`)を `llm_request` の本文から抜き出し、葉の JSON Pointer(添字は `*`)を `app/src/data/send-fields.json` の許す欄(その欄の下を含む)と比べる。原因コードごとの `detail` の行は、その封筒の `error.code`・`warnings[].code` のときだけ加わる。値が空(null・空の配列・空のオブジェクト)の葉は、表の欄の親なら許す |
| A7-3 | 同上(`--names` を付ける) | `A7-3 … 一致した名前=0`。`personal_names.strings` の各語を `llm_request` の本文全体(区画の外を含む)で検索し、語は出さず連番と行の数だけ出す。`--names` が無ければ A7-3 は未実施で全体を FAIL にする |
| A7-4 | 同上 | `A7-4 使用したモデル名=[…]` が空でなく、`model` 欄が空の送信が 0 |
| (件数) | 同上 | `表示しなかった応答=<件数>`(`withheld_reply` の記録の数。判定に数えない) |
| A7-1 | 利用者が自分のターミナルで: `read -rs K; printf '%s\n' "$K" \| python3 test/tools/check_a7.py secrets --records <記録のディレクトリ>; unset K`(秘密が複数なら 1 値ずつ繰り返すか、`read -rs` を重ねて `printf '%s\n%s\n' "$K1" "$K2"`) | `A7-1 一致の合計=0`。そのままの文字列(JSON の文字列としての表記を含む)と base64(3 通りの桁の揃えで埋め込まれた形を含む)の 2 つで、記録の全ファイルを検索する。値は標準入力だけで受け、引数・ファイル・環境変数に残さず、出力は検索語の連番と件数だけ。8 文字未満の行と `-----` で始まる行(PEM の見出し)は検索語にせず数だけ出す |

A7-1 は、秘密の値を持つ利用者だけが実行する(LLM API キー、B2 の鍵、フレーズ、SSH 秘密鍵、Webhook の URL、MinIO の資格情報と使い捨ての鍵。後の 2 つは `test/env/.run/accept-<NAME>/` の生成物から受入試験 WP が検索する)。監督と agent は秘密の値を受け取らない。

照合で表示しなかった応答は件数だけを記録する。

## 記録の取り扱い

検索の前に記録を伏せ字にしない。検索が 0 件であることを確かめた後で、鍵 ID・バケット名・アカウント ID・ホスト名と IP を種類と連番の形に伏せてからリポジトリに入れる。伏せ字は `printf 'bucket\t%s\nkey-id\t%s\n' "$B" "$KID" | python3 test/tools/check_a7.py redact --in <記録のディレクトリ> --out <空のディレクトリ>`(利用者が自分のターミナルで。種類は `key-id`・`bucket`・`account`・`host`・`ip`。置き換えは長い値から、`<bucket-1>` の形。`repo_id`・保管先の版 ID・`snapshot_ref`・`run_id`・`change_id` は対象外なので値を渡さない)。合成データの個人名入りの名前は伏せない(A7-3 の検索に使う)。
