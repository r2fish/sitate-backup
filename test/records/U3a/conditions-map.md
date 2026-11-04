# U3a 完了条件 → 試験の対応表(作業指示書 5.14。テスト仕様書 6.1 の 1)

作成: 2026-11-04(期間中)。作業ツリー wp/maps(開始 commit f503da6)。試験は変えていない。
実行: `npm --prefix app ci` の後、`app` で `node --test --test-reporter=spec "test/contract/*.test.ts" "test/u3a/*.test.ts" "test/e2e/*.test.ts" "src/kit/*.test.ts"`(`npm --prefix app test` と同じ集合)。集計行(逐語): `ℹ tests 324` / `ℹ pass 324` / `ℹ fail 0` / `ℹ skipped 0`。ログは作業の scratchpad の `test-maps.log`。
表の「試験」は `app/` からの相対パスと、試験の題の抜粋(関数名でなく `test("…")` の題)。結果の「緑」は上の 1 回の実行で pass。
判定: 緑 = 試験があり緑 / 一部 = 条件の一部だけ試験がある(残りを備考に書く) / 試験なし / 未実行 / 範囲外。

| # | 試験(ファイル:行 題) | 結果 | 備考 |
|---|---|---|---|
| 共通 | `npm run test:u3a` の集合は上の全体に含まれる | 緑 | `make test-app` 相当。`test:u3a` 単独の実行はしていない(全体で包含) |
| 1 境界 | `test/u3a/boundary.test.ts:20,30,38,56,65`(自前ソースが禁止モジュールを import しない、鍵を環境変数から読まない、依存の木、画面と直接通信しない、自己検査)、`boundary-runtime.test.ts:11,19`、`egress-static.test.ts:40,47,52,69,80,96`、`egress-runtime.test.ts:104,119,157`(LLM の口以外への送信) | 緑 | 「ネットワークは LLM API だけ」は guarded fetch(origin 検査)で試験がある。D-0177 の形(静的 + 実行時)で充足に数える(設計の監督の返事、2026-11-04)。監査の予行は「未実装」としたが、egress-* の試験は D-0177 の形(権限モデルでなく guarded fetch)で緑。この形を条件 1 の充足と数えるかは監督の裁定 |
| 2 実行基盤と継ぎ目 | `test/u3a/runtime.test.ts:14,24,36,42,53,60,71`(往復、履歴、system の並び、人の入力待ち、往復の上限、中断、max_tokens)、`gate.test.ts:13,24`(許可リスト外・型違反の拒否)、`surfaces.test.ts:81,95`(T-55 の照合)、`test/contract/tools.test.ts:39-`(道具の定義)、test/u3a/runtime-json-schema.test.ts | 緑 | 本文で確認(maps2)。`betaTool`(JSON Schema)を使い Zod を使わないことを `test/u3a/runtime-json-schema.test.ts` で確かめた(静的な検査)。失敗のコードは `llm.test.ts:12`・`status.test.ts:220` |
| 3 道具と CLI の一対一 | `test/contract/tools.test.ts:48,56,63,71,94,103,284`(公開区分、一対一、`bootstrap`・`bootstrap-restore` の例外、`attest` の署名を要さない 3 段) | 緑 | |
| 4 秘密の入力ボックス 3 モード | `test/u3a/secrets.test.ts:39,72,85,104,114`(保存・保存しない・アプリ自身が開く・管理用鍵)、`mp-secret-input.test.ts:13,20,24,37`、`mp-keychain.test.ts:43,56`、`mp-session.test.ts:47,59,71` | 緑 | 本文で確認(maps2)。製品は事業者の管理用鍵を常に保存しないモードで受け(`secrets.test.ts:85` で保存されない・同名の別項目を読まず消さないことを確認)、`session.dispose()` は何も消さない(キーチェーンに残るものが無いため。`session.ts` の注記)。D-0159 の「キーチェーンに保存したときだけ消える」は保存しない実装では空になる。保存した場合の消去の試験は作れない(その経路が無い)。アプリ自身が開くモード(LLM API キー)の試験は `agent-host.test.ts:30` と `mp-agent-args.test.ts:7` が間接 |
| 5 承認カード | `test/u3a/card.test.ts:35-200`(JCS・ハッシュ・署名、seal の区画 3 と固定文、AI の説明の別枠、確認語の欄、無効化、欄の見出し)、`card-propose.test.ts:22-61`、`mp-card.test.ts:28-51`、`mp-card-gate.test.ts:6,17`、`mp-session.test.ts:20,36`、`session.test.ts:55,67`、`survey.test.ts:24` | 緑 | 「確認語は保存・表示・モデル送信をしない」の独立の試験は `surfaces.test.ts:16` と `phrase-actions.test.ts:61` が近いが、`--notify-proof-stdin` の経路だけの試験は特定できなかった |
| 6 標準出力の 2 レコード | `test/u3a/stdout.test.ts:11,18,30`、`surfaces.test.ts:16,26`(SSH 切断含む)、`test/contract/vectors.test.ts:80,94,110,117,138,154`、`session.test.ts:143,154`(H2) | 緑 | |
| 7 フレーズのパネル | `test/u3a/surfaces.test.ts:38`、`phrase-actions.test.ts:61-249`(verify・confirm まで進めない、印刷の失敗、手書きの道 143、一時ファイル無し) | 一部 | 印刷の一時ファイルは「HTML をファイルに書かない」(surfaces.test.ts:38)と `windows.test.ts:30`。Electron の実機の印刷・パネルは未実行(予行 U3a-7。作業端末で 1 回の確かめと記録が要る) |
| 8 チャット欄の送信前の検出 | `test/u3a/surfaces.test.ts:56`、`session.test.ts:88` | 緑 | |
| 9 封筒の整形と要約の絞り込み | `test/u3a/shape.test.ts:13,26,38,45`、`test/contract/send-fields.test.ts:8` | 緑 | |
| 10 状態と報告の表示(復元テスト・無音) | 無音: `test/u3a/status.test.ts:63`。報告の復元テストの行: `status.test.ts` は `last_restore_test` を参照するが専用の題は無い、test/u3a/status-rows.test.ts | 緑 | 本文で確認(maps2)。復元テストの行(日付と ✓/✗、記録が無いとき —)を `test/u3a/status-rows.test.ts` で確かめた。無音は `status.test.ts:63`。錨・`teardown_pending` は範囲外 |
| 11 bootstrap | `test/u3a/bootstrap.test.ts:85-320`(全手順、署名不正、ハッシュ不一致、段 5 の linger・/etc/sitate、`--rekey`、ホスト鍵)、`ssh.test.ts:21-138`、`secrets.test.ts:138` | 緑 | `sitate doctor`(段階 bootstrap)の `check_images` 件数 0 を確かめる題は特定できなかった。実機の systemd は未実行(受入試験の環境) |
| 12 試験用ドライバ | `test/u3a/driver.test.ts:11,34`、`test/e2e/loop.test.ts:42-245`、`form.test.ts`、`endpoint.test.ts:16,41`、`product-clean.test.ts:14`(製品の成果物にドライバ無し)、`build.test.ts:12` | 緑 | 英語の回は範囲外(イテレーション 3)。A2・A6・A7 の実機の駆動は各 WP の記録(mainpath・A2・A6) |
| 13 信頼の起点と同梱物の署名 | `test/u3a/bundle.test.ts:31`(改変・取り違えで全体拒否、固定文)、`bundle-load.test.ts:22,32,68`、`build.test.ts:12,34`、`bootstrap.test.ts:129,144,305` | 一部 | 試験用の鍵での受付と拒否は緑。製品のアプリが `make sign` の成果物を受けることは配布用の鍵が要り未実行(利用者の `make sign`。予行 U3a-13) |
| 14 キーチェーンの経路 | `test/u3a/bundle.test.ts:53,70`(固定のサービス名、保存・取り出し・削除・列挙)、`mp-keychain.test.ts:43,56`、`refs.test.ts:73` | 一部 | 上は `security` の入れ物(模擬)での試験。macOS の実機のキーチェーンでの保存・取り出し・削除は未実行(作業端末で 1 回要る。予行 U3a-14)。許可ダイアログを止めない構成の報告は別途 |
| 15 カタログ | `test/contract/catalog.test.ts:30,35,45,54,60,69`、`test/u3a/catalog-ja.test.ts:12,22,27`、`catalog-use.test.ts:23,34,53`、`card.test.ts:142` | 緑 | |
| 16 日本語の表示 | `test/u3a/driver.test.ts:11`、`test/e2e/loop.test.ts`(日本語の台本) | 一部 | ドライバの日本語の操作は緑。A2・A6・A7 の日本語の回は各記録 |
| 17 LLM API キーの経路 | `test/u3a/agent-host.test.ts:30`、`mp-agent-args.test.ts:7`、`boundary.test.ts:30`、`boundary-runtime.test.ts:19` | 緑 | |
| 18 モデルの候補 | `test/u3a/mp-models.test.ts:8`、`llm.test.ts:20`、`status.test.ts:274`(使用中のモデル名と印) | 緑 | Models API の存在確認の実機は実物の LLM API が要り未実行(送らない約束) |
| 19 LLM API の障害と P7 | `test/u3a/llm.test.ts:12`、`status.test.ts:220,240`、`runtime.test.ts:71` | 緑 | 要求ごとの無応答の時限の試験は特定できなかった |
| 20 送る欄の表と `path_ref` | `test/u3a/shape.test.ts:13,26,45`、`gate.test.ts:62`、`refs.test.ts:25-61`、`test/contract/send-fields.test.ts:8` | 緑 | 「再起動の後に別の参照名が出て古い参照名が PATH_REF_UNKNOWN」の題は特定できなかった(gate.test.ts:62 は未知の参照名) |
| 21 | — | 範囲外(イテレーション 2) | |
| 22 製品と設定の説明 | `test/contract/assets.test.ts:11,50,64`、`test/u3a/mp-quote-render.test.ts:9,18,26`、`surfaces.test.ts:81,95,107`、`runtime.test.ts:36`、test/u3a/manual-fixed-sentence.test.ts | 一部 | 固定文「この製品の資料にはありません」がカタログ・説明書・規則ファイルで同じ文であることを `test/u3a/manual-fixed-sentence.test.ts` で確かめた(maps2)。製品側の注意: カタログのキー `chat.manual.unanswerable` はコードのどこからも参照されず、固定文はモデルが規則ファイルの指示で返す(核の側で強制する経路は無い)。読み取りだけの道具の制限(道具は `status`・`def show`・`list`・`report`・`estimate` だけ)を確かめる題は特定できなかった |
| 23 「アプリの情報」 | `test/u3a/status.test.ts:63,274`(`appInfo` の行にモデル名と印)、test/u3a/status-rows.test.ts | 一部 | 鍵の種別の表示(ソフトウェア鍵だけ)を `test/u3a/status-rows.test.ts` で確かめた(maps2)。表示言語の切り替え(UI の操作)の題は特定できなかった。`StatusView.setLocale` は日付の書式だけに効く |
| 24 アプリの記録 | `test/u3a/status.test.ts:257,274`、`test/e2e/app-fields.test.ts:9` | 緑 | |
| 25 実行の制限 | `test/u3a/agent-host.test.ts:49,73`(子プロセス・ファイルの読み書き・送信)、`egress-static.test.ts`、`egress-runtime.test.ts:104,119,157`、`mp-agent-args.test.ts:7` | 緑 | キーチェーンへの触れの失敗は「import が無い」静的検査(boundary.test.ts:20)で、実行時の試験は特定できなかった |
| 26 道具の拒否の封筒 | `test/u3a/gate.test.ts:13,24,36,44,62`、`runtime.test.ts:42,60`、`session.test.ts:67`(HUMAN_CANCELLED)、`catalog-use.test.ts`、`test/contract/catalog.test.ts:35`(名前空間) | 緑 | |
| 27 同梱物の取り込み | `test/u3a/bundle-load.test.ts:22,32`、`runtime.test.ts:36`(system の並びとキャッシュの区切り)、`test/contract/assets.test.ts:11,50,64` | 緑 | |
| 28 面の列挙 | `test/u3a/windows.test.ts:12,19,30,36` | 緑 | |
| 29 引用の照合の範囲 | `test/u3a/surfaces.test.ts:95`(問い・人の入力の値・説明書の引用は外し、印の外の主張で全体不表示)、`mp-quote-render.test.ts:18,26` | 緑 | 手順書の引用の文を含む応答の題は mp-quote-render.test.ts:26 |
| 30 手順の合格と往復の数え直し | `test/u3a/status.test.ts:144,160,177`、`mp-steps.test.ts:27,33,49,55,65`、`test/e2e/loop.test.ts:81`(AGENT_TURN_LIMIT) | 緑 | |
| 31 道具の呼び出しの表示 | `test/u3a/session.test.ts:77`(引数の値・参照名・パスが現れず、モデルへ送られない。拒否も同じ書式) | 一部 | 「会話の履歴に入れない」「LLM API へ送った内容に現れない」はこの題の中。`secret_refs`・`path_ref` が行に出ないことの専用の題は特定できなかった |
| 32 「AI に見立てを聞く」 | `test/u3a/status.test.ts:33`(変化の種類ごとに操作が出る、固定文)、`status.test.ts:117`(押したときの入力) | 緑 | 「変化の無い読み直しでは出ず、押さなければ呼び出しが増えない」は題 33・117 の本文に含まれるか未確認(題からは読み取れない) |
| 33 調査の結果の区画 | `test/u3a/survey.test.ts:24,49,56,67`、`card.test.ts:64` | 緑 | |
| 34 アプリの原因コードの `detail` と次の一手 | `test/u3a/status.test.ts:192`、`secrets.test.ts:125`、`shape.test.ts:45` | 緑 | |
| 35 健常性チェックの無い回 | `test/u3a/status.test.ts:85`(`checks_count` が 0 だけで固定文) | 緑 | 引用の印が照合を通ることは `mp-quote-render.test.ts:9` が近いが `checks_count` の題ではない |

## 集計(共通 1 行 + 条件 35 = 36 行)
- 緑 28(共通を含む。maps2 で #2・#10 を緑、#4 は設計の読みで緑) / 一部 7(#7・#13・#14・#16・#22・#23・#31) / 範囲外 1(#21) / 試験が全く無い条件 0。
- 「一部」の欠けた部分は備考。未実行(実機・鍵が要る)は #7・#13・#14・#18 の備考。
- maps2 で試験を足した: 条件 10(status-rows)・22 の固定文(manual-fixed-sentence)・23 の鍵の種別(status-rows)・2 の betaTool(runtime-json-schema)。残る欠け: 条件 22 の道具の制限、23 の言語の切り替え、31 の `secret_refs`・`path_ref`。
