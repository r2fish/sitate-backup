# 日 7(11/8)の朝の手順(1 枚)

作業指示書 6 章の日 7 の午前の直列の末尾(10:00 から)。監督 = Claude Code のメインセッション、利用者 = 自分のターミナルで秘密を扱う人。凍結(11/8 00:00)の後なので、commit の件名は `Docs:`・`Fix:`・`Submission:` だけ。

## 利用者へ(11/6 の時点)

- 実物の LLM を使う試験と動画の撮影は、11/6 までに終わった(`test/records/day7/video-2026-11-06.md`)。縮めた記録 21 個への秘密の値の照合も済んだ(一致 0。`test/records/A7/shrink-2026-11-05.md` の (f))。
- 履歴の全体への秘密の値の照合も済んだ(2026-11-06、main の HEAD `1b3e991`)。利用者が自分のターミナルで、リポジトリの根で `git rev-list --all` の全 commit(worktree の branch を含めて 224)を `git grep -F` で検索した。入れたのは、これまでに使った LLM API キーと B2 の管理用鍵(キー ID と鍵)のすべてで、どの値もそのままの文字列と base64 の両方で 0 件だった。B2 のアプリ用の鍵はアプリが作って利用者が持たないので入れていない。これは環境ごとの A7(`video-2026-11-06.md`、測定 4 の記録)で調べた。`1b3e991` より後に足す commit は、push の前に同じ検索をもう一度流す。
- 受入試験の環境は止めてある。`Fix:` で製品のコードが変わったときだけ、変わった場面を撮り直す(下の「動画の撮影(案 D)」)。そのときは新しい B2 のバケットを用意し、`read -rs` で値を入れ直す。

## 前日までに済ませておくこと(利用者)

- 配布用の鍵の組(minisign の秘密鍵と公開鍵)がリポジトリの外にあり、秘密鍵のパスワードが分かること。秘密鍵のパスと公開鍵のパスは**絶対パス**で控える(`make build-app` は `app/` の下で動くので、相対パスでは「公開鍵のファイルが無い」で止まる。11/4 の予行で確かめた。`test/records/day7/rehearsal-2026-11-04.md`)。
- 撮った動画を通して見て、ターミナルの録画に誤って打った値が映っていないこと、フレーズのパネルの 24 語をぼかすか使わないことを確かめる。

## 午前の順

| # | 誰が | どこで | すること | 見るもの |
|---|---|---|---|---|
| 1 | 監督 | main の最終の作業ツリー | `make test`・`make test-app`・`make contract-status` | 集計行(README の【日 7 に入れる】の値の出どころ)。赤なら止まる |
| 2 | 監督 | 同 | evidence-auditor(受け入れパケット)を起動 | 充足 / 未充足 / 証拠不足。未充足があれば止まって利用者と設計の監督へ |
| 3 | 監督 | 同 | `make build`(製品) | 終了 0。`dist/product/bundle/<版>/` に `linux-arm64`・`linux-x86_64`・`prices`・ソースのアーカイブ・SBOM |
| 4 | 利用者 | 自分のターミナル(Claude Code を起動していないもの)で、リポジトリの根 | `SECRET_KEY=<秘密鍵の絶対パス> PUBLIC_KEY_FILE=<公開鍵の絶対パス> make sign` | パスワードを 1 回だけ聞かれる。最後の行に署名の件数。失敗の行が出たら止まって監督へ貼る。**先に、公開鍵のファイルの 2 行目が `internal/contract/trust_product.go` の `DistributionPublicKey` と同じであることを確かめる**(`make sign` も `make build-app` も、この一致を確かめない。`make sign` が確かめるのは、秘密鍵と `PUBLIC_KEY_FILE` が組であることだけ(`tools/sign.sh:92`)。違う組で署名すると、両方とも通るが、核は配布物の署名を受け付けない。11/7 に新しい組を作ってしまいかけた) |
| 5 | 監督 | main | `PUBLIC_KEY_FILE=<公開鍵の絶対パス> make build-app` | `build-app: product -> …`。試験用ドライバの混入の検査で失敗しないこと。監督が `app/dist/embedded-keys.json` の `keys` が `DistributionPublicKey` だけであることを確かめる |
| 6 | 監督 | main | README の【日 7 に入れる】を 1 の集計行とその時点の git から埋め、`Submission:` の commit(D-0141) | 受入試験の環境と提出 branch をここで一致させて止める |
| 7 | 監督・利用者 | 6 の commit | 動画の場面 4〜6 を撮る(案 D。D-0208。下の「動画の撮影(案 D)」) | 撮影の後、down の前に A7 を流し、`Docs:` で記録に足す |

- `make sign-installer` は行わない(D-0205。イテレーション 1 はインストーラーを配らない。アプリはリポジトリから `make build-app` で組んで起動する)。作業指示書の 3.1・6 章の `sign-installer` の文言は直していないので、日 7 の手順と evidence-auditor の照合では D-0205 を正として読む。

- 公開鍵のパスと秘密鍵のパスは、監督の会話・記録・commit に書かない(利用者のターミナルの行だけに置く)。監督が示すのは手順の文だけ。
- 4 の後に貼ってもらうのは、終了コードと最後の数行だけ(パスワードやパスが写っていれば伏せる)。

## 未決(設計の監督に問う)

- (決着)インストーラー: D-0205 で案 A(配らない)。
- (決着)動画: D-0208 で案 D(試験ビルドでドライバが A2・A6 を流す実行を画面録画する)。製品ビルドは受入試験の環境に向けて起動しない。次の所見はその理由として残す。
- 製品ビルドのアプリが受入試験の環境に届かない見込み(コードを読んだ所見): 受入試験の環境のコンテナは SSH の口を作業端末に公開していない(`app/test/e2e/bridge.ts` の冒頭)。試験ビルドでは、ドライバが `docker exec` で橋を作り、試験ビルドだけの `SITATE_TEST_HOST_MAP`(`app/src/testing/host-map.ts`、`attach.ts`)で管理用 SSH の接続先を読み替えている。製品ビルドにはこの読み替えが無いので、開始時の入力の `backup`・`source`・`dest` に作業端末から届かない。設計の監督に問う。

## 動画の環境の実行の後(down する前)の A7(D-0200 の追記 2 の後の設計の指示)

核のログ・CLI の標準エラー・通知の記録は、測定 4 のビルドでも測定 3 のビルドでも検索していない(`test/records/A7/m4-2026-11-05.md`)。動画用の環境の実行の後、環境を down する前に次を行い、`Docs:` の commit で記録に足す。

1. 監督: 環境 `<NAME>` から、バックアップサーバーの状態ディレクトリ(`secrets/` を除く。鍵の置き場そのものは検索の対象にしない)と、通知の受け手の記録(`test/env/.run/accept-<NAME>/hook-data/`)を、リポジトリの外の空のディレクトリ `<A7DIR>` へ写す(`docker cp sitate-acc-<NAME>-backup:/var/lib/sitate <A7DIR>/state` の後に `<A7DIR>/state/secrets` を消す)。中身は読まない。
2. 利用者(自分のターミナル): `(printf '%s\n' "$LLM" "$KID" "$KEY"; bash test/tools/a7_env_secrets.sh <NAME>) | python3 test/tools/check_a7.py secrets --records <A7DIR> test/acceptance/records/<動画の記録>/` を流し、`files=` の行と `A7-1` の行を貼る。
3. 一致があれば、どのファイルの種類か(件数とパスの種類だけ)を監督が確かめ、鍵の置き場(暗号化した鍵束など、値を持つのが役目のもの)か漏れかを分けて記録する。
4. 監督が `<A7DIR>` を消す。

## 動画の撮影(案 D。D-0208)

撮るのは 6 の `Submission:` の commit の後。製品のコードは `f9fa3d8` から変えていないので、試験ビルドは測定 4 と同じものになる。

- 2026-11-06 に前へ動かして撮った(利用者の希望、設計の監督が了承。記録は `test/records/day7/video-2026-11-06.md`)。日 7 の 7 の段は、提出までに `Fix:` で製品のコードが変わったときだけ、変わった場面を下の手順で撮り直す。A7 もそのときだけ行う。

1. 監督: accept の worktree の追跡されていない記録(`test/acceptance/records/A2/`・`A6/`。縮める前の元の記録)をリポジトリの外の置き場へ移す(6 の commit には縮めた記録が同じパスにあるので、移さないと checkout が止まる。元の記録は受け入れ監査が済むまで残す)。accept の worktree を 6 の commit に合わせ(`git checkout --detach <6 の commit>`)、`make build-test sign-test build-app-test` を流す。`dist/test/install.md.orig.sha256` が `docs/install.md` の sha256 と一致することを確かめる。`make build-app-test` は `app/dist` を試験ビルドで上書きするので、main の作業ツリーの製品ビルド(5 の成果物)とは別の worktree で行う。
2. 場面 4・5(A2。変種 `add`): 監督が `accept-env.sh up <NAME> --variant add` と `prepare-a2 <NAME>` を流す。利用者が新しいバケットで、`test/acceptance/launch.md` の A2 の行を流す。`--out` は `test/acceptance/records/video/A2/`。
3. 場面 6(A6): 監督が A6 用の環境を起こす(`accept-env.sh up`、`LOCK_DEFAULT_DAYS=90 make test-env-up`、`prep.sh`・`prepare-a2`、偽の LLM で A2 を `seal` まで、`inject-env.sh`。測定 4 の A6 と同じ。`test/records/A6/real-m4-2026-11-05.md` の冒頭)。利用者が A6 の行を流す。`--out` は `test/acceptance/records/video/A6/`。
4. 場面 6 の CLI の部分は、監督がターミナルで流して録る(下の「場面 6 の分け方」)。
5. 環境ごとに、down の前に下の「動画の環境の実行の後(down する前)の A7」を行う。
6. 動画の記録(`test/acceptance/records/video/`)は、縮めと伏せ字(`test/tools/shrink_record.py`、`check_a7.py redact`)と秘密の照合を済ませるまで commit しない。

### 録り方(撮影の予行。`test/records/day7/rehearsal-2026-11-05-video.md`)

- ドライバは変えない(速さを落とす引数も環境変数も無い)。動画は普通の速さで録り、編集で切り出し・間の延ばし・止め絵にする。語りは止め絵に重ねる。
- アプリの窓は、ドライバが起動するたびに新しく開く(1100×760、画面の同じ位置)。録画は画面のその範囲を録る。窓を選んで録る形は、回ごとに窓が変わるので選び直しが要る。
- 実行の間、ほかのアプリを前に出さない(予行では、ほかの窓が前に出るとアプリの窓が隠れた)。
- 画面の録画の許可は利用者の録画の道具に与える(監督のセッションには与えない)。
- フレーズのパネル(手順 6)は 24 語が画面に出る。ドライバは語を隠さない。編集でぼかすか、その区間を使わない。
- 承認の画面は短い時間しか出ない(秒の見込みは予行の記録の 2 章)。読めない場面は、提出文書のセッションが案 C(測定 4 の記録と U3c の画面写し)で差し替える。

### 場面 6 の分け方(A6。台本 `test/acceptance/scripts/A6/script.md` の「実行の道具」の欄による)

| 映すもの | 項目 | 何が映るか |
|---|---|---|
| アプリの窓(ドライバの A6 の回。実物の LLM) | A6-15 の動的な部分(注入文 I1〜I4) | チャットに入った 4 つの注入文と、エージェントが道具を呼ばずに断る応答。測定 4 の回は 29 秒、LLM の要求 4 |
| ターミナル(監督。`TestA6CLI` を `-v` で) | A6-1〜A6-10・A6-12 | 項目ごとに 1 行 `A6-<n>: ok=… <原因コード>(終了 <n>)`(例 `A6-2: ok=false SIGNATURE_INVALID`)。A6-6 は延長ごとの `not_before` が 1 日ずつ後ろへ動く行。測定 4 のビルドで 389 秒。試験の道具(`go test`)の出力で、利用者が打つ操作ではない |
| ターミナル(監督) | A6-N1・A6-15a〜c・A6-17・A6-18(`static-checks.sh`)、A6-16(`check-16.sh`) | 検査ごとの `PASS` の行(逐語は `test/records/A6/real-m4-2026-11-05.md` の補足) |
| ターミナル(監督) | 判定のまとめ | `check_a6.py` の行と `A6 summary PASS=… FAIL=… UNRUN=…` |

- A6-15 のファイル名(F1)は、エージェントが調査を呼ばなければ画面に出ない(測定 4 でも UNRUN)。映すと言わない。
- A6-9 の画面の側(管理用鍵がキーチェーンに無い)と A6-10 の画面からの確認語の誤入力は、ドライバに無い。CLI の行で映す。

## 予行(2026-11-05、監督。HEAD 1a11cb0)

- `make test`: 終了 0、`ok` のパッケージ 73。`make test-app`: 終了 0、`ℹ pass 372`・`ℹ fail 0`・`ℹ skipped 0`。`make contract-status`: 終了 0、全 WP で未実装 0・その他 0(skip は U1e 1・U1f-1 2・U1g 3)。

## 日 7 の 1(2026-11-08、監督。HEAD e39f19d の後の作業ツリー。製品のコードは 2fbea95 と同じ)

- `make test`(`test/env/bin/in-linux.sh 'GOFLAGS=-count=1 make _test'`。`make test` の入口は GOFLAGS をコンテナへ渡さず、61 パッケージがキャッシュの結果だったので流し直した): 終了 0、`ok` のパッケージ 73、`FAIL` 0、`(cached)` 0。`make test-app`: 終了 0、`ℹ tests 372`・`ℹ pass 372`・`ℹ fail 0`・`ℹ skipped 0`。`make contract-status`: 終了 0、全 WP で未実装 0・その他 0。

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

模擬審査(第 2〜4 回)の注文(2026-11-07、提出文書のセッション経由):

- 印を残さない。測る元の無い数(失敗したときの見え方、フックが止めた回数、2 つの監督の間の裁定の件数)は「取れなかった」と書いて行を残す。
- 役ごとの起動の回数と裁定の件数は、設計リポジトリの道具で数える側から数を受ける(製品の監督は設計リポジトリを読まない)。
- フックの拒否の例は、日時と拒否の文の逐語を 2〜3 件。
- 別に起動したレビューが誤りを見つけた例を 2〜3 件に(今は `fix-db` の 1 件)。設計の側の候補は 11/7 の完了条件と試験の照合の掃き出し(D-0227)で、文は設計の側から受ける。
- 最初の commit の中身と、引用の照合が応答を止めた例は、2026-11-07 に README に入れた。
