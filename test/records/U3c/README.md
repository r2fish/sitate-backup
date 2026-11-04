# U3c 画面写し(作業指示書 5.14c の 7)

試験ビルドを偽の LLM で A2(変種 add)の最後まで進め、各面が出た時点で明と暗の PNG を撮った。値は偽の LLM と受入試験の環境の合成値だけ。窓は既定の大きさ(2200x1456 画素)。

## 撮ったビルド

- 基の commit: `184241b`(branch `wp/u3c`)に、この WP の未 commit の差分(`app/src/testing/shots.ts`・`driver-server.ts`・`attach.ts` の画面写しの口と `app/test/shots/cli.ts`)を載せた作業ツリー。
- ビルド: `make build-test sign-test build-app-test`(画面写しの口を入れた後に作り直したもの)。

## 画面の一覧

| ファイル(明 / 暗) | 面 | 撮った時点 |
|---|---|---|
| `chat-light.png` / `chat-dark.png` | チャット | 導入の始めの会話(人の発話の吹き出し、エージェントの文、知らせ) |
| `chat-installing-light.png` / `chat-installing-dark.png` | 導入中のチャット | 道具の行の畳み(開いた並びと畳んだ並び)、導入の進み具合と「次」の札、応答の待ちの印が同時に見える。開いている面(ダイアログ)が無い間だけ撮る |
| `approval-card-light.png` / `approval-card-dark.png` | 承認カード | 最初の承認カード(定義の確定)。確認語の欄は空 |
| `secret-input-light.png` / `secret-input-dark.png` | 秘密の入力ボックス | 最初の入力ボックス(LLM の API キー)。入れる前で、空 |
| `phrase-panel-light.png` / `phrase-panel-dark.png` | フレーズのパネル | 語の欄を隠して撮った(空白になっている)。注意書きとボタンだけが写る |
| `status-and-report-light.png` / `status-and-report-dark.png` | 状態と報告(右の欄)と導入の完了 | 手順 13 まで終わった後。進み具合は全部に合格の印 |

## 撮り方

- 台本: `node app/test/shots/cli.ts --name <環境名> --variant add`(`app/test/e2e/` のモジュールを import して使うだけ。`app/test/e2e/` は変えていない)。環境の準備は `test/wiring/e2e.sh` と同じ(accept-env up、`LOCK_DEFAULT_DAYS=90 make test-env-up`、prep.sh、prepare-a2、doubles-watch)。
- 台本は A2 の駆動の窓口(Port)を包み、`pending` と `lines` を読む回に、面が初めて出た時点で撮る。撮るたびに試験ビルドの口へ `capture`(theme を明か暗に当ててから `webContents.capturePage` の PNG を書く)を送る。
- 口は試験ビルドだけに入る(`app/src/testing/shots.ts`)。書く先は `test/records/U3c/` の直下の `.png` だけ。製品のビルドに入らないことは `app/test/u3a/shots.test.ts` が成果物で確かめる。

## フレーズの語を写していないことの確かめ

- 撮る前に口の `mask` が `#phrase-words` を `visibility:hidden` にする(語の文字は DOM に残るので、ドライバの読みは変わらない)。
- `capture` は、フレーズのパネルが開いていて語の欄が隠れていなければ、PNG を作る前と書く前の 2 回の検査で失敗し、何も書かない(試験: `shots.test.ts`)。語が写った PNG を一度も作っていない。
- 撮った PNG を目で見て、語の欄が空白であることを確かめた。
