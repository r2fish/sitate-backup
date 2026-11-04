# 日 7(11/8)の朝の手順(1 枚)

作業指示書 6 章の日 7 の午前の直列の末尾(10:00 から)。監督 = Claude Code のメインセッション、利用者 = 自分のターミナルで秘密を扱う人。凍結(11/8 00:00)の後なので、commit の件名は `Docs:`・`Fix:`・`Submission:` だけ。

## 利用者へ(11/5 の夜の時点)

- 実物の LLM を使う試験は、測定 4 と A6 の再実施で終わった。ターミナルの `LLM`・`EP`・`BK`・`KID`・`KEY` の変数は `unset LLM EP BK KID KEY` で消してよい(ターミナルを閉じてもよい)。
- ただし、公開用に縮めて伏せ字にした記録に、秘密の値の照合(`check_a7.py secrets`)をもう一度流してもらう(D-0200 の追記 2 の (3))。そのときは監督がコマンドを示すので、`read -rs` で値を入れ直す。
- 受入試験の環境は止めてある。日 7 の `Submission:` の commit の後に、監督が動画用の環境を起こし直す。

## 前日までに済ませておくこと(利用者)

- 配布用の鍵の組(minisign の秘密鍵と公開鍵)がリポジトリの外にあり、秘密鍵のパスワードが分かること。秘密鍵のパスと公開鍵のパスは**絶対パス**で控える(`make build-app` は `app/` の下で動くので、相対パスでは「公開鍵のファイルが無い」で止まる。11/4 の予行で確かめた。`test/records/day7/rehearsal-2026-11-04.md`)。
- 動画の場面 4〜6 に使う新しい B2 のバケットを 1 つ用意する(使っていない名前。D-0200 の追記 2 の (1))。

## 午前の順

| # | 誰が | どこで | すること | 見るもの |
|---|---|---|---|---|
| 1 | 監督 | main の最終の作業ツリー | `make test`・`make test-app`・`make contract-status` | 集計行(README の【日 7 に入れる】の値の出どころ)。赤なら止まる |
| 2 | 監督 | 同 | evidence-auditor(受け入れパケット)を起動 | 充足 / 未充足 / 証拠不足。未充足があれば止まって利用者と設計の監督へ |
| 3 | 監督 | 同 | `make build`(製品) | 終了 0。`dist/product/bundle/<版>/` に `linux-arm64`・`linux-x86_64`・`prices`・ソースのアーカイブ・SBOM |
| 4 | 利用者 | 自分のターミナル(Claude Code を起動していないもの)で、リポジトリの根 | `SECRET_KEY=<秘密鍵の絶対パス> PUBLIC_KEY_FILE=<公開鍵の絶対パス> make sign` | パスワードを 1 回だけ聞かれる。最後の行に署名の件数。失敗の行が出たら止まって監督へ貼る |
| 5 | 監督 | main | `PUBLIC_KEY_FILE=<公開鍵の絶対パス> make build-app` | `build-app: product -> …`。試験用ドライバの混入の検査で失敗しないこと |
| 6 | 監督 | main | README の【日 7 に入れる】を 1 の集計行とその時点の git から埋め、`Submission:` の commit(D-0141) | 受入試験の環境と提出 branch をここで一致させて止める |
| 7 | 監督 | 6 の commit | 動画の場面 4・5 用の A2 の環境と、場面 6 用の A6 の環境を `accept-env.sh up` で起こす(D-0200 の追記 2 の (1)) | `prepare-a2` まで終了 0 |

- `make sign-installer` は行わない(D-0205。イテレーション 1 はインストーラーを配らない。アプリはリポジトリから `make build-app` で組んで起動する)。作業指示書の 3.1・6 章の `sign-installer` の文言は直していないので、日 7 の手順と evidence-auditor の照合では D-0205 を正として読む。
- 動画の製品の場面の起動(D-0205。試験用ドライバと同じ起動の仕方。リポジトリの根から): `app/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron app --user-data-dir=<撮影用の空のディレクトリ> --lang=ja`。製品ビルドは利用者のログインのキーチェーンを使う(試験ビルドの `SITATE_TEST_KEYCHAIN` は無い)。撮影の後に `jp.sitate-backup.app` の項目を消すかは利用者が決める。受入試験の環境へ届くかは下の「未決」。

- 公開鍵のパスと秘密鍵のパスは、監督の会話・記録・commit に書かない(利用者のターミナルの行だけに置く)。監督が示すのは手順の文だけ。
- 4 の後に貼ってもらうのは、終了コードと最後の数行だけ(パスワードやパスが写っていれば伏せる)。

## 未決(設計の監督に問う)

- (決着)インストーラー: D-0205 で案 A(配らない)。
- 製品ビルドのアプリが受入試験の環境に届かない見込み(コードを読んだ所見。実行での確かめは日 6): 受入試験の環境のコンテナは SSH の口を作業端末に公開していない(`app/test/e2e/bridge.ts` の冒頭)。試験ビルドでは、ドライバが `docker exec` で橋を作り、試験ビルドだけの `SITATE_TEST_HOST_MAP`(`app/src/testing/host-map.ts`、`attach.ts`)で管理用 SSH の接続先を読み替えている。製品ビルドにはこの読み替えが無いので、開始時の入力の `backup`・`source`・`dest` に作業端末から届かない。設計の監督に問う。

## 動画の環境の実行の後(down する前)の A7(D-0200 の追記 2 の後の設計の指示)

核のログ・CLI の標準エラー・通知の記録は、測定 4 のビルドでも測定 3 のビルドでも検索していない(`test/records/A7/m4-2026-11-05.md`)。動画用の環境の実行の後、環境を down する前に次を行い、`Docs:` の commit で記録に足す。

1. 監督: 環境 `<NAME>` から、バックアップサーバーの状態ディレクトリ(`secrets/` を除く。鍵の置き場そのものは検索の対象にしない)と、通知の受け手の記録(`test/env/.run/accept-<NAME>/hook-data/`)を、リポジトリの外の空のディレクトリ `<A7DIR>` へ写す(`docker cp sitate-acc-<NAME>-backup:/var/lib/sitate <A7DIR>/state` の後に `<A7DIR>/state/secrets` を消す)。中身は読まない。
2. 利用者(自分のターミナル): `(printf '%s\n' "$LLM" "$KID" "$KEY"; bash test/tools/a7_env_secrets.sh <NAME>) | python3 test/tools/check_a7.py secrets --records <A7DIR> test/acceptance/records/<動画の記録>/` を流し、`files=` の行と `A7-1` の行を貼る。
3. 一致があれば、どのファイルの種類か(件数とパスの種類だけ)を監督が確かめ、鍵の置き場(暗号化した鍵束など、値を持つのが役目のもの)か漏れかを分けて記録する。
4. 監督が `<A7DIR>` を消す。

## 動画の撮り方(設計の指示)

- ドライバは変えない(速さを落とす引数は無い。待ちは固定)。動画は普通の速さで録り、編集で止め絵とゆっくり再生にする。語りは止め絵に重ねる。

## 予行(2026-11-05、監督。HEAD 1a11cb0)

- `make test`: 終了 0、`ok` のパッケージ 73。`make test-app`: 終了 0、`ℹ pass 372`・`ℹ fail 0`・`ℹ skipped 0`。`make contract-status`: 終了 0、全 WP で未実装 0・その他 0(skip は U1e 1・U1f-1 2・U1g 3)。

## README の【日 7 に入れる】の値の出どころ

| 印 | 取る場所 |
|---|---|
| A2 の 3 回の合否・問いの回数・人の作業の数 | `test/records/A2/m4.md` の「集計」 |
| A6 の合否の集計行 | `test/records/A6/real-m4-2026-11-05.md` の `A6 summary` の行 |
| A7 の一致件数と送った欄の検査 | `test/records/A7/m4-2026-11-05.md` |
| make test・make test-app の集計行 | 1 の出力(`ok` のパッケージ数、`ℹ tests`・`ℹ pass`・`ℹ fail`) |
| test-mainpath の第 3 段と DB の段、test-adapters-root | 最後に流した記録(`test/records/integrate/run-2026-11-04.md`)。日 7 に流し直すかは時間で決める |
| commit の数 | 7 の commit の直前の `git rev-list --count HEAD` に 1 を足す |
| WP の数 | 作業指示書 4 章の表の行数と `test/records/` の WP の置き場の数 |
| 往復の回数 | 測定の記録(`test/records/A2/m1-pre.md`〜`m4.md`)の、測定の間の直しの回数 |
