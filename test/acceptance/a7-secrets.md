# A7 の秘密の検索の手順(利用者のターミナル。値は監督にも担当にも見せない)

対象: A2 の測定 1・2 の各回(`test/acceptance/records/A2/m1/<回>/`、`m2/<回>/`)と A6 の記録(`test/acceptance/records/A6/`)。検索するのは、試験で使った秘密の値がそのままの文字列と base64 の 2 つの形で記録に出ていないこと(件数 0)。

## 先に知っておくこと

- 場所は受入試験の worktree の根(`.claude/worktrees/accept`)。ターミナルは Claude Code を起動していないものを使う。
- 環境が生成した値(管理用 SSH の秘密鍵、Webhook の URL、`init-storage` が B2 で発行して backup の状態ディレクトリ `secrets/` に置いた書き込み鍵・読み取り鍵、pull 用と復元先用の SSH 秘密鍵、通知の資格情報)は、環境を down すると消える。だから回ごとに、環境を down する前に検索する。
- 環境の値は `bash test/tools/a7_env_secrets.sh <NAME>` が 1 行 1 値で標準出力へ出す(画面に出さずパイプで渡す。標準エラーに値のない行数の内訳 3 行が出る)。`key_id` は記録に出てよい識別子(伏せ字の対象。下の「伏せ字」)なので値に含めない。
- 手で持つ値は、LLM API キー(`LLM`)と B2 の管理用鍵(キー ID `KID`、キー `KEY`)。引数・ファイル・履歴に残さないよう、`read -rs` で読んだ変数を `printf` で渡す。
- 次の 2 つは検索できない。(a) `init` のフレーズ: 設計で、バックアップサーバーのディスク・保管先・記録に残らない(画面のフレーズのパネルと紙だけ)。環境からは取れないので、自動の検索の対象外。控えを取ったなら、その 1 行を `read -rs PH` で読み、下の `printf` に足す。(b) 送信用メールアカウントの資格情報: 受入試験では使っていない(使っていないと監督に伝える)。

## 1 回ごとの手順(各回の終了の後、環境を down する前)

起動の前に 1 度だけ、変数を読む(値は表示されない)。

    read -rs LLM; read -rs KID; read -rs KEY

各回の記録ができたら、その回の `<NAME>` の環境がまだ上がっている間に、次の 1 行を流す。`<測定>` は `m1` か `m2`、`<回>` は回の番号。

    (printf '%s\n' "$LLM" "$KID" "$KEY"; bash test/tools/a7_env_secrets.sh <NAME>) | python3 test/tools/check_a7.py secrets --records test/acceptance/records/A2/<測定>/<回>/

A6 の記録の後も同じ(A6 の環境の `<NAME>` で、環境を down する前)。

    (printf '%s\n' "$LLM" "$KID" "$KEY"; bash test/tools/a7_env_secrets.sh <NAME>) | python3 test/tools/check_a7.py secrets --records test/acceptance/records/A6/

出力の最後の行が `A7-1 一致の合計=0 PASS` なら、その回は合格。`FAIL` なら 0 でない件数がある(`secret#N` の行の番号は入力の行の順で、先頭が `LLM`・`KID`・`KEY`、続きが環境の値。値は出ない)。標準エラーの 3 行(`admin_key_lines`、`hook_url`、`state_values`)の数が 0 になっていたら、環境の値が取れていない(環境が既に down している、`<NAME>` の誤り)ので、検索したことにならない。やり直す。

## 最後に(全部の回の後)

環境の値は回ごとに消えるので、最後は手で持つ値だけで、記録の全体に 1 回流す。

    printf '%s\n' "$LLM" "$KID" "$KEY" | python3 test/tools/check_a7.py secrets --records test/acceptance/records

流し終えたら変数を消す。

    unset LLM KID KEY PH

## 監督に貼るもの

各回の `A7-1 一致の合計=<N> PASS|FAIL` の行と、標準エラーの 3 行(値のない数だけ)を、回の名前(`m1/1` など)を付けて貼る。鍵の値、`secret#N` 以外の出力、記録の中身は貼らない。

## 伏せ字(合計 0 の後)

記録を公開する前の伏せ字(鍵 ID・バケット名など)は `test/acceptance/launch.md` の「A7 の検査」の `redact` の手順で行う。
