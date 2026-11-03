# mp-core3 の記録(結線の試験で未合格だった手順 1・5・10・13 の核の側)

正本: 仕様書 1.2.15、作業指示書 1.0.24。根拠: test/records/mp-app/notes.md の「核の側で見つかった未解決」。

| 項目 | 結果 | 直した場所 | 赤の試験(修正前に落ちた) |
|---|---|---|---|
| 1 `attest` の署名を要さない段が `init` の前に `NOT_INITIALIZED` | 直した。署名を要する段は `init` の前のまま `NOT_INITIALIZED` | `internal/cli/precheck.go`(`attest` の `--step` が署名を要さない 3 段なら前提の確認を飛ばす) | `internal/cli/attest_preinit_test.go` の `TestAttestUnsignedStepsRecordBeforeInit`(`NOT_INITIALIZED`、終了 3)。署名を要する段が弱まらない側は `TestAttestSignedStepsStillNeedInit` |
| 2 `status.notify.proof_confirmed` が `seal` の承認の後も `false` | 直した | `internal/approval/approve.go`・`notify.go`(確認語の照合が通った `seal` の承認の中で、state.lock の下で `notify.proof_confirmed` を `true` にする。他の項目は残す) | `internal/approval/proof_confirmed_test.go` の `TestSealApprovalSetsProofConfirmed` |
| 3 `status.schedule` の形 | 決められない。直していない | — | — |
| 4 `SIGNATURE_INVALID` の `detail.reason` が送る欄の表に無い | 仕様に載っていない。直していない | — | — |

## 3 の食い違い(仕様の穴。推測で埋めていない)

- 仕様 3.1.2.7 の `status` の結果は `schedule` とだけ書き、形を定めない。4.1 の `schedule` は `"02:30"`(日次の時刻)で、CLI の結果のスキーマ(`internal/contract/schema/cli/results.json`)と見本は `hhmm` の文字列。実装(`internal/cli/status.go`)はこれに従う。
- 3.8 の手順 13 は「`/result/schedule` に 3.1.2 のタイマー列の全項目がある」と読める期待を置き、手順書は `/result/schedule` ⊇ `{{TIMER_NAMES}}` と書く。文字列には項目を含められない。
- 両論: (a) `status.schedule` をタイマーの単位の名前の配列にする(手順 13 は満たすが、4.1・スキーマ・見本・`status` を読む表示と合わない。時刻は別の欄が要る)。(b) 手順 13 の期待を直す(`schedule` は時刻のまま、タイマーの登録は `doctor` の `timers_registered` で確かめる)。どちらかは設計リポジトリの監督の裁定。
- そのため `tools/sign.sh` の `TIMER_NAMES` は空のまま。結線の試験は手順 13 の合格を要求するが、空の一覧に対する判定なので `schedule` の中身を確かめていない(空虚な合格)。仕様の裁定の後に `TIMER_NAMES` を埋め、この確かめを実にする。

## 4 の送る欄

仕様 3.7 の送る欄の表(2242〜2260 行)に `SIGNATURE_INVALID` の行は無い。封筒の行は `/error/detail` を送らない欄に置く。3.1.3 は `detail.reason`(`unknown_key` / `bad_signature` / `hash_mismatch` / `clock`)と `skew_s` を持つ。両論: (a) 追加する(モデルが承認のやり直しの原因を説明できる。値は秘密を含まない)。(b) 追加しない(`next` が「アプリの承認カードで承認し直す」で足りる。署名の失敗の細部をモデルに渡さない)。表は U0 の固定なので変更していない。

## 結線の試験で見つかった、依頼の外の原因(2 件。どちらも手順 1 と 5 の合格に必要だった)

1. 試験の環境: バックアップサーバーの sudo は環境を落とすため、bootstrap の段 (8) の `sudo -u sitate sitate attest` は試験ビルドの核に `SITATE_SIM_TIME_FILE` が届かず `ARG_INVALID`(`--sim-time`)になった(製品のビルドは環境変数を読まない)。`test/wiring/run.sh` が sudoers に `Defaults env_file="/etc/environment"` を置く(試験の環境の準備)。
2. 実装の誤り: `tools/sign.sh` が手順書の印 `DIST_KEY_FINGERPRINT` に使う指紋(`tools/fingerprint.sh`)は minisign の鍵の 42 バイトの SHA-256 で、仕様 4.5 の指紋(OpenSSH の blob の SHA-256)と違い、核の `version` の `signing_key_fp` と一致しなかった。手順 1 の判定(`/result/signing_key_fp` = 印)が落ちる。`sign.sh` に仕様どおりの `ssh_fp` を持たせた。`tools/fingerprint.sh` は担当範囲の外で、もう使われない(監督が直すか消す)。
