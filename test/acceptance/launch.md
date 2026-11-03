# 受入試験 A2・A6 の起動の手順(利用者へ渡す 1 枚)

A2 の測定 1 の前に、監督が利用者へ渡す。値は書かない(環境変数の名前だけ)。起動は利用者が、Claude Code を起動していない別のターミナルで、受入試験 WP の worktree の根から行う。環境変数はコマンドの行の前置きだけに置き、シェルの設定ファイルや Claude Code を起動したシェルで `export` しない。名前に `ANTHROPIC_API_KEY` を使わない(Claude Code がその鍵で動き、API の請求になるため)。

## A2 の前の予行(利用者。LLM を使わない)

実物の LLM API を使う A2 の前に、保管先の B2 に対する部分だけを `make test-b2-preflight` で通す。偽の LLM の口でアプリを通すので Anthropic へは何も送らない。必要なのは B2 の管理用鍵とバケット名だけで、`SITATE_ROLE_LLM_KEY` は要らない。

- バケットは A2 の回に使うものとは別の名前にする(init-storage が作り、保持が 90 日あって消せないため。何も削除しない)。
- 準備(監督が済ませる): `make build-test sign-test build-app-test`、`node app/node_modules/electron/install.js`、`bash test/env/bin/accept-env.sh up <NAME>`(予行は `base`)。環境変数の前置きは A2 と同じ値の形(下の表)。
- 実行(Claude Code を起動していない別のターミナルで、受入試験 WP の worktree の根から。値は前置きだけに置き、`export` しない):

      SITATE_ROLE_B2_ENDPOINT=… SITATE_ROLE_B2_BUCKET=… SITATE_ROLE_B2_ADMIN_KEY_ID=… SITATE_ROLE_B2_ADMIN_KEY=… \
        make test-b2-preflight NAME=<NAME>

- 通すもの: `init-storage`、`bucket check`(保持の短縮などの試しの結果 `permissions[]`)、`backup`、`extend`、`restore-test`。記録は `test/records/b2-preflight/`(`record.jsonl` と、各道具の合否と `permissions[]`(op・key_role・expected・actual だけ)の要約 `preflight-summary.txt`)。値は記録にも標準出力にも出さない。終了コードが 0 でなければ、要約の ok=false の行と `permissions[]` の `<-- 不一致` を監督に伝える。
- 手順が壊れていないかは偽の口(試験用の MinIO)で確かめられる: `LOCK_DEFAULT_DAYS=90 make test-env-up NAME=<NAME>` の後に `make test-b2-preflight NAME=<NAME> FAKE=1`。
- 終わったら `accept-env.sh down <NAME>`(A2 の回の前に、環境を回ごとに作り直す)。

## 先に監督が済ませておくこと

- 試験ビルドの作り直し: `make build-test sign-test build-app-test`(手順書を変えたときは必ず再実行する。`dist/test/install.md.orig.sha256` が確定した `docs/install.md` の sha256 と一致することを監督が確かめる)。
- 受入試験の環境の起動(A2 は回ごと、A6 は 1 回): `bash test/env/bin/accept-env.sh up <NAME> --variant <add|lo|nocluster|base>`、続けて `bash test/env/bin/accept-env.sh prepare-a2 <NAME>`(試験ビルドの核の信頼の起点 `trust.pub`・`/etc/environment` の `SITATE_TEST_TRUST_PUBKEY_FILE`・sudoers の `env_file` を backup・dest に置く。`up` は置かない。これが無いと bootstrap の署名の検証や `attest` が通らない)。A6 の MinIO は `LOCK_DEFAULT_DAYS=90 make test-env-up NAME=<NAME>`。終わったら `accept-env.sh down <NAME>` と `make test-env-down NAME=<NAME>`。

## 環境変数の名前(人の役の値。利用者が自分で置く)

| 名前 | 何の値か | A2 | A6 |
|---|---|---|---|
| `SITATE_ROLE_LLM_KEY` | LLM API キー(Anthropic Console。ワークスペースに紐づけた無期限のもの) | 要る | 要る |
| `SITATE_ROLE_B2_ENDPOINT` | 試験用の B2 の S3 互換エンドポイント | 要る | — |
| `SITATE_ROLE_B2_BUCKET` | 試験用のバケット名(PoC とは別) | 要る | — |
| `SITATE_ROLE_B2_ADMIN_KEY_ID` | B2 の管理用鍵のキー ID(Q1 の値。秘密の入力ボックスへ 1 行目) | 要る | — |
| `SITATE_ROLE_B2_ADMIN_KEY` | B2 の管理用鍵(同 2 行目) | 要る | — |
| `SITATE_ROLE_NAME` | 受入試験の環境の名前(`accept-env.sh up` の `<NAME>`) | 要る | 要る |

`SITATE_ROLE_B2_ENDPOINT` は `https://s3.<リージョン>.backblazeb2.com` の形(`https://` から始め、末尾に `/` や経路を付けない。保管先準備アダプタとドライバがこの形だけを受け、違えば起動前に止まる)。`SITATE_ROLE_B2_BUCKET` は回ごとに別の名前にする(バケットは 90 日消せず、`init-storage` は作り直さないため。予行のバケットとも別にする)。

通知先・元サーバー・復元先・バックアップサーバーの接続情報は、受入試験の環境が `test/env/.run/accept-<NAME>/env` に書く(秘密ではない値と、起動のたびに生成した使い捨ての資格情報)。ドライバがそこから読む。

## 起動のコマンドの行(値を含めない)

A2(回ごと。1 回目は `<回>` = 1、変種 `add`。2 回目は 2、`lo`。3 回目は 3、`nocluster`):

    SITATE_ROLE_LLM_KEY=… SITATE_ROLE_B2_ENDPOINT=… SITATE_ROLE_B2_BUCKET=… SITATE_ROLE_B2_ADMIN_KEY_ID=… SITATE_ROLE_B2_ADMIN_KEY=… SITATE_ROLE_NAME=<NAME> \
      node app/test/e2e/cli.ts --script test/acceptance/scripts/A2/script.json --variant <回の変種> --out test/acceptance/records/A2/<測定>/<回>/

A6(1 回。日本語の回):

    SITATE_ROLE_LLM_KEY=… SITATE_ROLE_NAME=<NAME> \
      node app/test/e2e/cli.ts --script test/acceptance/scripts/A6/injections.json --out test/acceptance/records/A6/

`…` は利用者がその場で値を打つ(履歴に残したくなければ、先頭に空白を置くか、`read -rs` で読んだ変数を前置きに使う)。`node app/test/e2e/cli.ts` は試験用ドライバ(`app/test/e2e/`)のコマンドラインの入口で、`--mode real`(既定。実物の LLM API を中継し、そこで送った内容を記録する)で動く。終了コードは 0(完了)・1(不合格)・2(台本に無いカード)・3(無効。H5)・64(引数の誤り)。終わったら、利用者は終了コードだけを監督に伝える。

## ドライバがすること(起動の前提)

- 実行の場所: 受入試験 WP の worktree の根。そこに `app/node_modules`(`npm --prefix app ci`)、Electron の実行体(`node app/node_modules/electron/install.js`)、`app/dist`(`make build-app-test`)、`dist/test/`(`make build-test sign-test`)、`test/env/.run/`(`accept-env.sh up` が作る)が要る。worktree を作り直したときは全部作り直す(`.run` と `dist` は git 管理外)。
- 開始: 接続先の記録も会話も無い状態から始まるので、チャット欄に「始める」のボタンが出る(spec 3.7 の最初の一歩)。ドライバは固定の文を打たずにそのボタンを押し(カタログの固定の指示が人の発話として送られる)、エージェントの最初の応答の後に、バックアップサーバーと元サーバーの所在と管理アカウントを渡す。この 2 つは開始時の入力で、問いにも人の作業にも数えない。ボタンが無ければ起動前の状態が違うので、終了コード 1 で止まる。
- 止め方: Ctrl-C(または終了の合図)で、LLM API キーを入れた使い捨ての keychain とユーザーデータの置き場を消してから終わる(終了コード 130)。強制終了(SIGKILL)では消えないので、その場合は `test/env/.run` と一時ディレクトリの `t.keychain-db` を探して消す。

- 実物の Electron の試験ビルド(`app/dist`。`make build-app-test` の成果物)を、回ごとに使い捨ての keychain とユーザーデータの置き場で起動する。A2 の各回は作業端末の初期化(キーチェーンの項目、アプリのデータの置き場、固定したホスト鍵の削除)から始まる。`--state-dir <dir>` を付けると置き場を残して使い回す(A6 を A2 の `seal` 済みの状態から始めるとき。初期化は `--reset`)。
- 受入試験の環境のコンテナ(`backup`・`source`・`dest`)は作業端末から直接届かないので、ドライバが作業端末の `127.0.0.1` の口を作って橋渡しする(`docker exec` 経由。試験用のアプリは管理用 SSH の接続先だけを読み替える)。このため `docker` が使えること。
- アプリの LLM API の口は、ドライバが立てるローカルの口(`127.0.0.1`)へ向ける(試験ビルドだけで効く)。実物の LLM API へはドライバが中継し、キーはアプリが秘密の入力ボックスで受けた値をそのまま渡す。
- 秘密の入力ボックス・承認カード・フレーズのパネルは台本の固定の回答で操作する。値は記録とログに出さない(記録を書く直前と書いた後の 2 回、秘密の値で検査し、出ていれば不合格にする)。
- 出力は `--out` の `record.jsonl`(1 件 1 行の JSON)。種類: `header`・`start_input`・`start_click`(「始める」の押下)・`displayed_reply`・`withheld_reply`・`question`・`continue_intervention`・`rate_limit_wait`(LLM API の頻度制限の待ちと、その後の再試行の入力。続行の介入には数えない)・`turn_limit_input`・`tool_call`(封筒を含む)・`tool_display`・`step_passed`・`surface_op`・`human_work`・`llm_request`(LLM API へ送った内容)・`keychain_items`・`a2_fields`・`secret_check`・`summary`。
- 偽の LLM の口で通す予行(LLM API キーも B2 も使わない): `bash test/env/bin/accept-env.sh up <NAME>`、`LOCK_DEFAULT_DAYS=90 make test-env-up NAME=<NAME>` の後に `make test-app-e2e NAME=<NAME>`(`test/wiring/e2e.sh`。A2 の手順 1〜13 と A6 の注入文の道を通す)。

## 記録の秘密の検索(利用者が自分のターミナルで行う)

LLM API キーと B2 の管理用鍵で、記録を検索する(そのままの文字列と base64 の 2 つ)。値は `read -rs` で読み、件数だけを監督に伝える。

    read -rs K; grep -rIc -F -e "$K" -e "$(printf %s "$K" | base64)" test/acceptance/records/ | grep -v ':0$' | wc -l; unset K

件数が 0 であることを確かめてから、監督は記録の識別子を伏せ字にする。MinIO の資格情報と使い捨ての鍵は `test/env/` が生成するので、受入試験 WP が検索する。
