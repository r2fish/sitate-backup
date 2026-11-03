# 受入試験 A2・A6 の起動の手順(利用者へ渡す 1 枚)

A2 の測定 1 の前に、監督が利用者へ渡す。値は書かない(環境変数の名前だけ)。起動は利用者が、Claude Code を起動していない別のターミナルで、受入試験 WP の worktree の根から行う。環境変数はコマンドの行の前置きだけに置き、シェルの設定ファイルや Claude Code を起動したシェルで `export` しない。名前に `ANTHROPIC_API_KEY` を使わない(Claude Code がその鍵で動き、API の請求になるため)。

## 先に監督が済ませておくこと

- 試験ビルドの作り直し: `make build-test sign-test build-app-test`(手順書を変えたときは必ず再実行する。`dist/test/install.md.orig.sha256` が確定した `docs/install.md` の sha256 と一致することを監督が確かめる)。
- 受入試験の環境の起動(A2 は回ごと、A6 は 1 回): `bash test/env/bin/accept-env.sh up <NAME> --variant <add|lo|nocluster|base>`。A6 の MinIO は `LOCK_DEFAULT_DAYS=90 make test-env-up NAME=<NAME>`。終わったら `accept-env.sh down <NAME>` と `make test-env-down NAME=<NAME>`。

## 環境変数の名前(人の役の値。利用者が自分で置く)

| 名前 | 何の値か | A2 | A6 |
|---|---|---|---|
| `SITATE_ROLE_LLM_KEY` | LLM API キー(Anthropic Console。ワークスペースに紐づけた無期限のもの) | 要る | 要る |
| `SITATE_ROLE_B2_ENDPOINT` | 試験用の B2 の S3 互換エンドポイント | 要る | — |
| `SITATE_ROLE_B2_BUCKET` | 試験用のバケット名(PoC とは別) | 要る | — |
| `SITATE_ROLE_B2_ADMIN_KEY_ID` | B2 の管理用鍵のキー ID(Q1 の値。秘密の入力ボックスへ 1 行目) | 要る | — |
| `SITATE_ROLE_B2_ADMIN_KEY` | B2 の管理用鍵(同 2 行目) | 要る | — |
| `SITATE_ROLE_NAME` | 受入試験の環境の名前(`accept-env.sh up` の `<NAME>`) | 要る | 要る |

通知先・元サーバー・復元先・バックアップサーバーの接続情報は、受入試験の環境が `test/env/.run/accept-<NAME>/env` に書く(秘密ではない値と、起動のたびに生成した使い捨ての資格情報)。ドライバがそこから読む。

## 起動のコマンドの行(値を含めない)

A2(回ごと。1 回目は `<回>` = 1、変種 `add`。2 回目は 2、`lo`。3 回目は 3、`nocluster`):

    SITATE_ROLE_LLM_KEY=… SITATE_ROLE_B2_ENDPOINT=… SITATE_ROLE_B2_BUCKET=… SITATE_ROLE_B2_ADMIN_KEY_ID=… SITATE_ROLE_B2_ADMIN_KEY=… SITATE_ROLE_NAME=<NAME> \
      <ドライバの実行入口> --script test/acceptance/scripts/A2/script.json --variant <回の変種> --out test/acceptance/records/A2/<測定>/<回>/

A6(1 回。日本語の回):

    SITATE_ROLE_LLM_KEY=… SITATE_ROLE_NAME=<NAME> \
      <ドライバの実行入口> --script test/acceptance/scripts/A6/injections.json --out test/acceptance/records/A6/

`…` は利用者がその場で値を打つ(履歴に残したくなければ、先頭に空白を置くか、`read -rs` で読んだ変数を前置きに使う)。`<ドライバの実行入口>` は試験用ドライバ(`app/test/e2e/`)を台本どおりに動かす実行入口で、A2 の前に U3a の持ち主が置く(2026-11-03 の時点では `app/test/e2e/driver.ts` のクライアントだけで、台本を読んで走る入口が無い)。終了コードは 0(完了)・1(不合格)・2(台本に無いカード)・3(無効。H5)。終わったら、利用者は終了コードだけを監督に伝える。

## 記録の秘密の検索(利用者が自分のターミナルで行う)

LLM API キーと B2 の管理用鍵で、記録を検索する(そのままの文字列と base64 の 2 つ)。値は `read -rs` で読み、件数だけを監督に伝える。

    read -rs K; grep -rIc -F -e "$K" -e "$(printf %s "$K" | base64)" test/acceptance/records/ | grep -v ':0$' | wc -l; unset K

件数が 0 であることを確かめてから、監督は記録の識別子を伏せ字にする。MinIO の資格情報と使い捨ての鍵は `test/env/` が生成するので、受入試験 WP が検索する。
