# A2 の測定 1 の前の `make test-e2e`(2026-11-03、main 7cc551a)

- `make test-e2e NAME=e2em1`: 終了 0。`ok github.com/r2fish/sitate-backup/test/e2e`、`--- PASS: TestStage3MainPath (290.81s)`(手順書の手順 1〜13 の完了確認を実物の核で判定する)。
- 測定 1 の 1 回目の環境は `a2r1`(変種 add)。

## 1 回目(無効)と直し(D-0170)

- 1 回目(環境 a2r1、変種 add、実物の LLM と B2): ドライバの終了コード 1。seal の前の `doctor`(段階 `defined`)で手順 13 が合格の印になり、ドライバが手順 13 の印で終えたため、手順 8〜12 の前に打ち切られた。エージェントは Q2 を正しく聞いていた。手順 1〜7 は合格、問い 3 回(台本外 0)、続行の介入 3 回、人の作業 H1〜H3、秘密の漏れ 0。製品の失敗に数えず、無効として同じ回をやり直す(記録は accept の worktree の `test/acceptance/records/A2/m1/1-driver-defect/`)。
- 直し `5a6c69c`: 手順 13 の 2 つ目は `/result/stage` = `sealed` かつ `/result/passed` = `true`(D-0170 と一致)。ドライバは手順 1〜13 すべての合格で終える。`app/test/u3a/mp-steps.test.ts` の試験は条件を外すと赤。
- 直しの後の main で `make test-app-e2e`(偽の LLM、アプリと実物の核の通し): A2・A6 とも終了コード 0。段階 `sealed` の `doctor` は seal の後にしか返らないので、手順 13 の印が seal の前に付かないことはこの通しが含む。
- 試験ビルドは作り直し、`install.md.orig.sha256` と `docs/install.md` の sha256 は一致(`6eb317f95733f344...`)。

## 1 回目のやり直し(2026-11-03 13:05、環境 a2r1 を再利用、バケットは新しい名前)

- ドライバの終了コード 1「進まない(入力の回数が上限を超えた)」。手順 1・2 は合格、問い 1 回(Q1)、続行の介入 41 回、人の作業 H1、秘密の漏れ 0。
- 原因(製品の側。エージェントの問いの終わり方): 手順 2 の後、エージェントは復元先(Q3)と守る対象(Q4)をまとめて聞き、文末を「教えてください。」「…返してください。」「…再開します。」で終えた。規則ファイル(`assets/rules/data-section-rules.md` の「問いの文は疑問符で終える」、`quote-check-forms.md` の問いの終わり方の一覧)に当たらないので、ドライバは問いと数えず「続けてください」を返し、エージェントは同じ依頼を繰り返した。疑問符の問いを出した回も、後ろに説明の文を続けたので最後の文が問いにならなかった。
- ほかの所見: 手順の順を外れて Q4 と Q1 を先に聞いた。`init-storage` を `--adapter` なし(`ARG_INVALID`)と定義の前(`DEF_INVALID`)で呼んだ。開始時の入力の前に `version`・`status`・`doctor` を呼んで `SSH_UNREACHABLE`。
- 記録は accept の worktree の `test/acceptance/records/A2/m1/1/`。利用者の貼り付けにはこの回の前にもう 1 回の実行(手順 8〜12 が未合格)があるが、同じ出力先に上書きされ記録が残っていない。環境 a2r1 を作り直さずに続けて流した回で、原因は確かめられない。
- 環境 a2r1 は 2 回使ったので、次の回は作り直す。

## 往復の直し 1(D-0171)と測定 1 のやり直しの用意(2026-11-03 の夜)

- 測定 1 は 1 回で打ち切り(D-0171)。打ち切った回の記録は accept の worktree の `test/acceptance/records/A2/m1/aborted-1/`。手順 13 のドライバの不具合の回は `1-driver-defect/`。
- 直し `bda552a`: 規則ファイル `quote-check-forms.md` に問いの出し方の節(疑問符で終える、1 回に 1 つ、最後の文、手順の順、開始時の入力の前に道具を呼ばない)。システムプロンプトがこれを含む試験。ドライバは既存の記録を退避し、`question_not_last`・`multi_question`・`repeated_requests` を `a2_fields` に出す。`npm --prefix app test` は pass 295、fail 0。`make lint-install` 合格。偽の LLM の A2・A6 は終了コード 0(担当の実行)。
- 製品の説明書には足していない(契約の試験が見出しを固定している)。
- 用意: accept の worktree を `bda552a` に進め、試験ビルドを作り直し(写しの sha256 一致 `be4a0ece...`)、環境 `a2r1`(変種 add)を作り直して `prepare-a2` まで済ませた。朝に利用者が 1 回目を流す。

## 測定 1 の 1 回目(2026-11-03 22:30、a2r1、add。不合格・手順 11)

- ドライバの終了コード 1「進まない」。手順 1〜10(seal を含む)は合格(手順 10 は表示の文字の壊れでドライバが取りこぼした。設計の監督の読み)。問いは D-0171 の規則どおり(`question_not_last` 0、`multi_question` 0)。
- 手順 11 の `backup` で DB の対象 t1〜t3 が `INTERNAL`、ファイルの対象 t4 は ok。原因は製品(核): `internal/acquire/run_target.go` の `targetSpec` が postgresql の対象の要求を `spec.path=""` で組み、要求の検証(`schema/adapter/acquire.json` の target.spec は `path` 必須、1 文字以上)で落ちる。アダプタは起動されない。主経路と偽の LLM の通しは DB のクラスタを `source.declined` にしていて、DB の取得を実物の核で通した試験が無かった。
- D-0171 の裁定(2)により、測定 1 の 1 回目「不合格(手順 11)」として数え、測定 1 は打ち切らない。2 回目(lo)・3 回目(nocluster)を同じビルド(`bda552a`)で流してから直し 2 に入る。製品の直しは測定 1 が閉じるまで main に入れない。
- 直しの案は申し送りの 41 節(裁定待ち)。試験 `test/mainpath/stage3_db_test.go`(wp/fix-db、未 commit)は今の main で赤。

## 測定 1 の 2・3 回目と測定 1 を閉じる(2026-11-04 の夜)

ビルドは 1 回目の後の accept(`694f4dd` に A7 の道具の `160bd01`。製品は 1 回目と同じで、直し 2 は入っていない)。記録は accept の worktree の `test/acceptance/records/A2/m1/2/`・`/3/`。

| 回 | 環境・変種 | 終了コード | 合格した手順 | 止まった所 | A7-1 |
|---|---|---|---|---|---|
| 1 | a2r1・add | 1(進まない) | 1〜10 | 手順 11: DB の対象 t1〜t3 が `INTERNAL`、ファイルの t4 は ok | — |
| 2 | a2r2・lo | 1(台本に無い人の作業で止まった) | 1〜10・13 | 手順 11: `backup` が 5 回とも `ALL_TARGETS_FAILED`、`result` は `{}`、`next` は `[]` | 一致 0、PASS |
| 3 | a2r3・nocluster | 1(台本に無い人の作業で止まった) | 1〜10・13 | 手順 11: `backup` が 7 回とも `ALL_TARGETS_FAILED` | 一致 0、PASS |

- 3 回とも手順 11 と 12 は不合格で、原因は同じ製品の不具合(DB の対象の取得の要求。41 節・D-0172 の直し 2)。直し 2(`b3e1143`・`92ab692`)が 694f4dd に無いことは git で確かめた。2・3 回目の対象は DB だけなので、全対象が失敗した。
- 問いの規則(D-0171): 2・3 回目とも `question_not_last` 0、`multi_question` 0。
- 2 回目: モデルが `sitate_backup` を `dry_run: true` で 1 回呼び、`NOT_IMPLEMENTED` を受けた。D-0183 の直し(道具から外す)の前のビルドなので想定どおり。
- 3 回目: モデルは Q4(守りたいもの)を聞かず、DB 3 つをすべて対象にする案を出した。ドライバは Q4 と照合せずに「推奨の案で」と答え、5433 のクラスタも対象に入った。nocluster の変種の狙い(使っていないクラスタを断る)を通っていない。往復の直しの案は申し送り 49 節。
- 元サーバーのディレクトリ: 3 回とも調査は `fs_type: overlay`・`supported: false`。overlay は数えない種類(仕様 3.3)なので、製品の判定は正しい。受入試験の環境の source がデータをコンテナの根(overlay)に置いているためで、変種の違いではない。1 回目はモデルが `supported: false` のディレクトリをファイルの対象 t4 に入れ、取得は ok だった。2・3 回目は入れなかった。49 節。
- 測定 1 はこれで閉じる(3 回とも不合格・手順 11)。直し 2 と D-0183 を含む統合のビルドを main に入れ(`a563a61`、`3128a2c`)、測定 2 の環境 a2r4(add)・a2r5(lo)・a2r6(nocluster)を用意した。
