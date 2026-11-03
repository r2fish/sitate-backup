# mp-app の受け入れ(2026-11-03。アプリと実物の核の結線)

- 範囲: アプリのまとめレビューの致命 C1〜C9 と高 B1〜B10、`make build-app-test`、Electron の煙試験、結線の試験 `make test-app-real`。詳細は `notes.md`。
- 再実行(main に rebase した後、新しい環境): `make build-test`、`make sign-test`、`make build-app-test` は終了 0。`make test-app-real`: `✔ [mp-app 結線] bootstrap → 手順 2〜10(承認カード・署名・確認語)→ seal → backup → restore-test が、実物の核に対して通る`(pass 1、fail 0。LLM の代わりに台本が道具を呼ぶ)。`npm --prefix app test` を 3 回: 各 pass 261、fail 0。`test:smoke`(実物の Electron): pass 1、fail 0。`make lint-install`・`make lint-size` は合格。`internal/contract`・`data/prices` は ok。
- 監督の変異: `--secret-fd` の付与を壊すと app の試験の 1 件が赤。ほかは担当のログ(`mut-mp-app-C8.log` と修正前の赤)で、監督は再現していない。
- 手順書の判定の合格は手順 2、3、4、6、7、8、9、11、12。手順 1、5、10、13 は核の側の未解決が原因で未合格(続きの作業で直す)。
- 担当の報告の「途中 1 回 `Malformed OpenSSH public key`」は監督の 3 回では再現しなかった。原因は確かめていない。
- 残る制限: エージェントのプロセスの実行の制限は `--permission` と読み取りだけで、ネットワークは対象外(Electron 45 の Node 24 が `--allow-net` を知らない。担当の実測)。LLM の実物、エージェントのプロセスの実起動、製品ビルドは未検証。中・低の一部は未対応(`notes.md`)。
- Electron の実行体は `npm ci` では入らず、`node app/node_modules/electron/install.js` で取る。
- 別起動のレビューはこの版に未実施。
