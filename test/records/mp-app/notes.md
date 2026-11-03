# mp-app の記録(アプリと実物の核の結線の直し)

正本: 仕様書 1.2.15、作業指示書 1.0.24。レビュー(final-app)の指摘を 1 件ずつ確かめてから直した。誤りの指摘は無かった(下の「確かめた結果」)。

## 確かめた結果と直した場所

| 番号 | 確かめた結果 | 直した場所 | 試験(app/test/u3a/) |
|---|---|---|---|
| C1 | 確実。`models.json` が空 | `assets/models/models.json`、`tools/sign.sh`(空の一覧は署名しない) | `mp-models.test.ts` |
| C3 | 確実。1 行入力が改行を落とす。読めない鍵は ssh2 が同期で投げ、`SSH_AUTH_DENIED` にならない | `renderer/index.html`・`style.css`・`renderer.ts`(textarea、文字を伏せる)、`ssh/connection.ts`、`testing/driver-server.ts` | `mp-secret-input.test.ts` |
| C4 | 確実。複数行は 16 進で返り、約 4 KB で命令が切れる。実測: 1 行 3,000 文字は通り 4,000 文字は通らない | `keychain.ts`(全体を base64、2,000 文字ごとの断片、`#c<番号>`)、`secret-box.ts`(保存の失敗でも待ちを解く) | `mp-keychain.test.ts`(偽と、使い捨ての keychain での実物の往復) |
| C7 | 確実。D-0165 で確定 | `tools/sitate_init.json`(`approval_pubkey` を外す)、`tool-gate.ts`・`session.ts`・`approval-sign.ts`(`--approval-pubkey` と `--secret-fd 3`)、契約試験 `NOT_OFFERED` に `approval-pubkey`(init だけ) | `mp-init.test.ts` |
| C8 | 確実(6 原因)。ハッシュに state・not_before、labels、確認語の宛先、signed-at の形、seal の定義の出所 | `approval-sign.ts`、`approval-card.ts`、`index.ts` | `mp-card.test.ts` |
| C9 | 確実 | `step-judge.ts`(値は先頭の 1 語、裸の語は文字列) | `mp-steps.test.ts`(実物の `docs/install.md`) |
| B1 | 確実 | C8 と同じ | `mp-card.test.ts` |
| B2 | 確実 | `session.ts`(直列)、`card-gate.ts`(決定の changeId 照合)、`agent/entry.ts`(入力の直列)、`approveEnabled` を主プロセスでも確認 | `mp-session.test.ts`、`mp-card-gate.test.ts` |
| B3 | 確実 | `secret-labels.ts`、カタログ 4 キー(道具と引数でラベルを分ける)。途中の文の表示は未対応(下) | `mp-session.test.ts` |
| B4 | 確実 | `quote-render.ts`、`session.ts` | `mp-quote-render.test.ts` |
| B5 | 確実 | `docs/install.md` の「定義の形」 | 手順書の文面(`make lint-install`) |
| B6・B7 | 確実 | `secret-flow.ts`・`tool-gate.ts`(`*_ref` を差し込み、事業者の管理用鍵はキーチェーンを見ない)、手順書の「参照名(固定)」 | `mp-session.test.ts` |
| B8 | 確実(煙試験で描画の完了後に入力ボックスが開くことを確認) | `index.ts`(`did-finish-load` を待つ) | 煙試験 |
| B10 | 確実 | `docs/install.md`(保存しないモード) | — |
| C2 | 確実。加えて下の実バグ | `Makefile` 変更なし(`build-app*` は `app/scripts/build-app.mjs` が標準の置き場を組む)、`tools/sign.sh`、`app/package.json`(`main`)、`build-app.mjs` | 煙試験 `app/test/smoke/` |
| C6 | D-0165 に従い順を直した | `docs/install.md` | 結線の試験 |

## 結線の試験で見つかったもの(レビューが想定していなかった実バグ)

1. Electron の Node(BoringSSL)は `createHash("blake2b512")` を持たず、`Digest method not supported` で署名の検証が落ち、アプリは起動しなかった。`blake2b.ts` を自前で持つ(`mp-blake2b.test.ts` が OpenSSL と一致を確かめる)。煙試験で発覚。
2. `ssh/connection.ts` の `exec` が呼び出しごとに `close` の待ちを足し、長い導入で listener が溜まる警告が出た。

## Electron の実行体

`npm ci` は Electron の実行体を入れない(`node_modules/electron/dist` が無い。インストールの処理が走らない)。`node app/node_modules/electron/install.js` で取得できた(ネットワークが要る)。

## 仕様の穴・未対応

- 核の `status.schedule` は日次の時刻の文字列(`"02:30"`)で、手順 13 の期待 `/result/schedule ⊇ {{TIMER_NAMES}}` は配列を想定する。`TIMER_NAMES` を空の一覧にして署名している(`tools/sign.sh`)。タイマーの単位の名前を `status` が返すか、期待を直すかが要る。
- 保管先の事業者の管理用鍵は `{"key_id","secret"}` の組(仕様 3.1.1)だが、入力ボックスは 1 つの欄。アプリが「1 行目キー ID・2 行目鍵」か JSON を受けて組む(`secret-labels.ts`)。仕様に書かれていない。
- B3 の「途中の文も表示する」: `AgentRuntime.run` は最後の応答の文だけを返すため未対応。
- B9(`agentExecArgv` の実測)、中・低の一部(`truncated` の属性、`sanitizeText` の `Cn`、`pending_count`、保留中の変更からカードを開く道、`host` の表示など)は未対応。

## 結線の試験の結果(`make test-app-real NAME=<名前>`。ログ: scratchpad の test-mp-app-real.log)

アプリの実物の部品(Session、bootstrap.ts、SshCoreRunner、承認カードと署名、フレーズのパネル、入力ボックス)が、台本の道具の呼び出しで、実物の核(試験ビルド)に対して手順 1(bootstrap)から手順 12(restore-test)まで通った。人の役(入力ボックスへの入力、承認カードの承認と確認語、フレーズの写し間違いと再入力)は台本が演じる。保管先準備アダプタは代役(test/mainpath/provminio)。
`PHRASE_INVALID`(写し間違いの再入力)と `SIGNATURE_INVALID`(登録していない承認鍵)は、カタログの文で止まる。手順書の判定で合格した手順: 2,3,4,6,7,8,9,11,12。

### 核の側で見つかった未解決(internal/ は担当範囲の外。監督へ)

1. `attest --step admin_access_closed` が `init` の前に `NOT_INITIALIZED` を返す(`internal/approval/attest.go`)。仕様 3.1.2 は「`init` の前は `admin_access_closed_at` だけを置く」。結果として bootstrap の段 (8) の記録が残らず、`doctor` の必須項目 `admin_access_closed` が不合格のまま、手順 1・5・13 の判定が通らない。bootstrap.ts は記録の失敗を無視する(`closeAccess`)。
2. `status` の `notify.proof_confirmed` が、`seal` の承認で確認語が一致しても `false` のまま(`internal/cli/status.go` は初期値の `false` を持ち、承認が書かない)。手順 10 の期待が満たせない。
3. `status.schedule` が文字列 `"02:30"` で、手順 13 の期待(配列に `TIMER_NAMES` を含む)と形が違う(上の「仕様の穴」)。
4. `SIGNATURE_INVALID` の `detail.reason`(`unknown_key` など)が `app/src/data/send-fields.json` に無く、モデルへ届かない(送る欄の表は U0 の固定)。仕様 3.1.3 は `reason` を持つ。
5. `init-storage --dry-run` は `--prefix` を必須とする(`ARG_INVALID` の `missing`)。仕様 4.1 の既定は `sitate/`。手順書の手順 2・5 に `--prefix sitate/` を足して合わせた。

### 結線の試験の前提(試験の環境の準備)

- 試験ビルドの核が信頼の起点を読む `/etc/sitate-test/trust.pub` と `/etc/environment` の行は、台本 `host.prerequisites` と同じく試験の環境の準備として `test/wiring/run.sh` が置く(アプリの bootstrap の仕事ではない)。
- 保管先準備アダプタの代役は、bootstrap が同梱の b2 アダプタで `/usr/libexec/sitate/adapters/provision/b2` を上書きし管理アクセスを閉じた後に、ホスト側が root で置く(`need-doubles` / `doubles-done` の旗ファイルで同期)。
- 試験の本体は Node 24(`node:24-bookworm-slim`。タグ指定で digest は固定していない)のコンテナで走る。作業端末の node_modules(macOS)を使うため、ssh2 の native 部品は無く JS の実装で動く。
- 環境の作り直しに数分かかる(bootstrap は 1 度きり)。やり直しは `accept-env.sh up` から。

## 未検証

- Electron の utilityProcess でエージェントが実際に起動すること(`--allow-net` を外したが、LLM API に繋がないため未実測。Node 24 の実行体で旗が通ることだけ確認)。
- 実物の LLM、`make build-app`(product。PUBLIC_KEY_FILE と配布用の署名は利用者のもの)。
- 手順 13、手順 1・5・10 の合格(上の核の未解決)。
