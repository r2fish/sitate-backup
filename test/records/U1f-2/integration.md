# U1f-2 結合の記録(作業指示書 5.18)

作成: 2026-11-03(期間中)。逐語のログは作業の scratchpad に置いた(パスは受け入れの報告に書く)。

## 完了条件ごとの確かめ

| # | 内容 | 確かめたもの |
|---|---|---|
| 1 | `make test`・`make test-app` | `make test` 終了 0(`go vet` 両タグ、`go test` 両タグ、契約 go・prices、アダプタ)。`make test-app` は 231 件 pass / fail 0 / skipped 0(`npm --prefix app ci` の後) |
| 2 | `make build`・`make build-test` | 依存の検査 OK(製品ビルドに fake 系なし・試験用の信頼の起点なし、試験ビルドに配布用の信頼の起点なし)。ソースのアーカイブ(tar.gz)と SBOM(SPDX-2.3)ができ、SBOM は `go list -m all` の依存を全部含む(ルートのモジュール自身を除く 45)。アーカイブを展開して `tools/build.sh product` を回すと、両 arch の核の sha256 が元のビルドと一致(再現)。`prices/prices.json` を `make build` で bundle に置くよう Makefile を直した(`bundle/<version>/prices/` が空だった)。`make sign` と `make sign-installer` は利用者の仕事(未実施) |
| 3 | `make test-e2e` | `test/e2e` の読み込み・判定の試験に加え、`make test-e2e NAME=<WP>` が第 3 段の台本を走らせ、終わりで手順書(`docs/install.md`)の手順 1〜13 の完了確認コマンドを実物の核に gate 経由で実行し、`test/e2e` の `LoadInstall`・`Judge` で期待する値に照らす(`test/mainpath/stage3_e2e_test.go`)。`sitate status` の `/result/last_backup/ok` と `/result/last_restore_test/ok` は真 |
| 4 | `make sign-test` | 58 件に署名。bundle の 15 署名(両 arch の核と同梱アダプタ 5 種、価格表、ソースのアーカイブ、SBOM)を minisign で検証して不合格 0。アダプタの `adapter.json.sig` の trusted comment は `version=<版> arch=<os-arch> path=<os-arch>/adapters/<kind>/<name>/adapter.json`。核の信頼の起点での検証は第 2・3 段の `bootstrap` が通している |
| 5 | MinIO に対する再実行 | 第 3 段で `verify`・`extend`・`prune`(`stage3_retention_test.go`)。同じ id `t1` の根を変え、同一メタデータ・異内容のファイルの世代を取得 → dest へ復元して元の根から計算した行と一致(`stage3_rootchange_test.go`) |
| 6 | `test-tick` | 受入試験の環境のバックアップサーバーで `test-tick --days 1`(共有の値が 24 時間進み、その間のタイマーの `backup` が動く。`stage3_tick_test.go`)。`KIND=fake` で `test-tick --days 3` の後に、保持 1 日の日次の世代が `Retention.Prune` で消え、月次の世代は残る(`stage1_faketick_test.go`) |
| 8 | 元サーバーの調査の通し(変種 (c)) | `stage3_survey_test.go`(`enrollSource` から呼ぶ): 調査の後に `source_seen[]` に指紋、`root_forbidden` の根は `source enroll` が `DEF_INVALID` で止まり受け口の行も ACL も付かない、登録の後に `source_clusters` が書かれ `source_seen[]` が消え、`source check` が `actionable = []` |

## 結合で直したもの(要確認)

- `tools/fingerprint.sh`: 手順書に埋める指紋が、核の `version` の `signing_key_fp` と違っていた(minisign の 42 バイトを掛けていた)。仕様 3.2.1 の「ssh-ed25519 の blob」に直した。
- `internal/approval`: `seal` の承認で確認語が一致しても `state.json` の `notify.proof_confirmed` を誰も書いていなかった(手順 10 の完了確認が通らない)。`commitApproval` で書く(`TestSealProofSetsProofConfirmed`)。
- `Makefile`: `_build` に価格表の複写、`test-e2e` に `NAME` 付きの結合の実行。
- 定義(第 3 段の台本): 元サーバーの PostgreSQL のクラスタを `source.declined` に書いた(変種 (c))。
- `test/mainpath/backup_test.go`: 保持区分の判定に既存の世代を渡していなかったため、世代がすべて月次になっていた。
- `internal/contract`(最小の追加): `VerifyResult.Sampled`。

## 外した機能・飛ばした試験の一覧

6 章の規則で結合から外した機能は、監督から指示が無かったので無い。`go test -v` の skip は次の 2 種類。数は `-tags sitate_testbuild` で 40 行、タグ無しで 39 行。

1. 後のイテレーションの印(7 章): `journal verify`(`TestContractRestoreJournalVerify`)、`storage issue-read-key`・`storage revoke-read-key`(`TestContractKeysStorageIssueReadKey`・`TestContractKeysStorageRevokeReadKey`)、`secrets set`(`TestContractKeysSecretsSet`)、`RecordReadStart`・`SettleInterrupted`(`TestContractStateRecordReadStart`・`TestContractStateSettleInterrupted`)、`TestContractAdapter` の一部。
2. 環境が要る試験(環境変数が無ければ skip。環境を立てた実行では走る): MinIO(`TestMinIO*`・`TestExternalS3ObjectLock`)、偽の保管先(`TestTestTickAdvancesFakeStoreRetention`)、PostgreSQL(`TestRealPostgreSQL`、`TestSetup*`)、podman(`TestPodman*`)、受入試験の環境(`TestAccept*`)、手順書の完了確認(`TestInstallCompletionChecks`。第 3 段の台本が同じ確認を実行する)。

## 確かめていないもの

- `make sign`・`make sign-installer`・`make build-app`・`make build-app-test`(利用者または wp/mp-app の仕事)。
- アプリを試験用のバックアップサーバーの `bootstrap` に結線する確かめと、試験用ドライバでの 5 面の操作(人の役の値を置く起動は利用者)。
- `verify` の `storage.noncurrent_surge`(前回の値の置き場が仕様に無い)、`head_missing`、保管先の `journal/` の版の照合(`journal verify` の持ち場でイテレーション 2)。
- `extend`・`prune` の `--dry-run`(`Retention` の契約に plan が無い。`NOT_IMPLEMENTED`)。
- 手順 13 の `/result/schedule` ⊇ `{{TIMER_NAMES}}`: `status` の `schedule` は hhmm の文字列で、手順書の期待(項目名の配列)と食い違う。仕様 3.1.2.7 と CLI スキーマは文字列。第 3 段の台本はこの期待だけを「既知の食い違い」としてログに残す(他の期待は強制)。
- 手順 1・5・13 の `doctor` の `/result/passed`: 受入試験の環境は台本が管理アクセスを使い続けるので `admin_access_closed` が外れる。台本は「必須の項目で外れたのがそれだけ」のときに限り通す。
