# 主経路の第 2 段の迂回を外した記録(2026-11-03)

- 対象: 記録 `stage2-2026-11-03.md` の「代役」「見つけたもの」(E1〜E6、C1、C2)と、仕様書 1.2.9・作業指示書 1.0.18(D-0160)の S1・S2。前の記録は変更しない。
- 実行: 新しい環境(`accept-env.sh up` と `LOCK_DEFAULT_DAYS=90 make test-env-up`)ごとに `make test-mainpath STAGE=2`。台本は `test/mainpath/stage2_*`。
- 結果: 実 CLI の導入が仕様 3.8 の順(`def validate` → `init-storage` → `init` → `phrase verify` → `bucket check` → … → `backup` → `restore-dest enroll`/`check` → `propose seal` → `restore-test`)で通る。手順 10 の `propose seal` は提案が作られるところまで(実物の呼び出し)。**`seal` の承認(署名・確認語・遅延)と `apply` は台本の外で、seal は未到達**。集計行は完了報告に逐語で載せた。
- 版: 仕様書 1.2.10・作業指示書 1.0.19 に追従(D-0160、D-0162)。

## 外した迂回

| 前の迂回 | 直した欠陥 | 今の台本 |
|---|---|---|
| `keys/pending/master.json` を `keys/master.json` に写す | E3 | 写さない。`init-storage`(`init` の前)、`phrase verify` の確定、`bucket check` の記録が実物の順で通る |
| 秘密ファイル `storage.json` を台本が置く、`init-storage` を通さない | E1・E2・E3 | `init-storage --adapter minio` が保管先準備アダプタで鍵を発行し、核が秘密ファイルに保存する |
| `def/candidate.json` を `def/approved.json` に写す | E4 | 写さない。`seal` の前のまま `backup`・`restore-dest enroll`・`restore-dest check`・`restore-test` が候補の定義で動く |
| `secrets/restore-init.json` の仮の中身 | S1(D-0160、D-0162) | 置かない。`def validate` が `pending_refs[]` に `/restore/dest/key_ref`・`/storage/0/key_ref` を返す。`/storage/0/key_ref` は `init-storage` の鍵の保存で解決し、`ssh-host` の `/restore/dest/key_ref` は秘密ファイルでなく手順 9 の登録の記録(`host_keys[]` の `role: restore_dest` で `restore.dest.at` のホスト名の要素と、null でない `restore_dest_enrollment`)で解決する。台本は `restore-dest enroll` の後に実物の `propose seal` を呼び、提案が作られることを確かめる |
| `bucket check --key-ref storage` の明示 | S2(D-0160) | 省く。候補の `storage[0].key_ref` を使う |
| 置き場の `b2` の `adapter.json` の `secrets` を台本が書く | E1 | 同梱の `adapters/provision/b2/adapter.json` の `secrets[]` をそのまま使う |
| `bootstrap` が両 arch・ソース・SBOM を置かない(台本は版ディレクトリ全体を置く) | C1 | アプリの `bootstrap.ts` の段 (4) が版ディレクトリの全体を置くように直した。台本の置き方(`placeBundle`)は同じ内容 |
| アダプタの実行ファイルに `.sig` が無く `make build-app` が止まる | C2 | アプリ側(`bundle-load.ts`・`build-app.mjs`・`bootstrap.ts`)が、署名付き `adapter.json` の `sha256` で実行ファイルを照らす(仕様 3.2.1) |

E5(systemd の単位に `LimitCORE=0`・`LimitMEMLOCK=infinity`)は、台本の `verifyTimers` が実物の systemd `--user` で単位を起動し、`systemctl --user show -p LimitCORE -p LimitMEMLOCK` の値が `LimitCORE=0`・`LimitMEMLOCK=infinity` であることまで確かめた(systemd が持つ設定値。起動した核のプロセスの `/proc/<pid>/limits` までは見ていない。非特権の `--user` で hard limit を実際に上げられるかは未確認)。`ExecStart=-` は今のまま(裁定済み)。

**E6(保管先準備アダプタの無い事業者の `untested`)は取り下げた**(D-0162: 判定はイテレーション 3。spec 8.2)。`provisionAbsent` と `untested` への分岐、その試験を消し、アダプタが読めないときは修正前の fail-closed(`INTERNAL`)に戻した。

## 残した迂回と理由

| 残したもの | 理由 |
|---|---|
| 保管先準備アダプタの代役 `minio`(`test/mainpath/provminio`)。`init-storage` は `--adapter minio`。置き場の `b2` も同じ代役に差し替える | `bucket check` は `--adapter` を受けず既定の `b2` を呼ぶ(仕様 3.1.2)。同梱の `b2` は B2 の API にしか届かない。MinIO は試験用の代役のアダプタで通す方針(S3 は仕様の変更なし)。実物の B2 は未検証。**代役の `check` は環境のポリシーと同じ固定の `capabilities` の表を返す**ので、`permissions[]` の「権限の読み取りで判定する操作」の列は MinIO の鍵の実際の権限を読んだ値ではない。試す操作(版指定なしの削除、保持の短縮、リーガルホールドなど)は実物の MinIO で実際に試している。台本は `init-storage` と `bucket check` の `permissions[]` の 30 要素すべてで `expected` = `actual`、`untested` なしを判定する |
| `bootstrap` を台本が行う | アプリの `bootstrap.ts` は動かさない。置く内容は `bootstrap.ts` と同じ(C1 の直しで版ディレクトリ全体になった)。`bootstrap.ts` 自体は app の単体試験で確かめた |
| `seal` を省く | 承認の署名は台本の外。承認済みの定義を写さず、`seal` の前のまま進める。そのため `restore-test` の `config_copy_ok` は偽(`config/<def_hash>` を `seal` の `apply` が書く) |
| 復元先へ渡すスナップショット鍵を台本が導く、元サーバーの管理用資格情報に環境の鍵 | 前の記録のとおり |

## E3: `init` の前の記録の形(仕様に答えが無く、担当が選んだ。設計の監督が追認し、仕様書 1.2.10 の 4.5 に入った)

`init-storage` は `init` の前に動くので、その時点では記録鍵(マスター鍵から導く)がどこにも無い。

1. マスター鍵が `keys/pending/master.json` にだけある間(`init` の後、`phrase verify` の確定の前)は、`journal` の鍵だけをその鍵から導く(`keys/master.json` が無い(`ErrNotExist`)ときだけ。壊れているときは導かない)。ほかの用途の鍵は確定まで導かない。この間の項目は状態ディレクトリにだけ残し、保管先へは書かない。確定の後の `SyncRemote` が写す。台本は、最後に `journal/` の連番が 1 から切れずに並び、全項目が保管先に写った記録(`state.json` の `objects[]`)を持つことを確かめる。
2. マスター鍵がまだ 1 つも無い間(`init` の前の `init-storage`)は、`write_intent` だけを `journal/pre-master`(行ごとの JSON)に退避し、鍵ができた最初の追記の前に連番の項目として移す。中身は `probe/` の乱数のキー名だけで、保管先へ写らない。鍵のない封じ方は作らない。`run_end` など ほかの種類の記録は従来どおり鍵が無ければ書けない。
3. 確定の前に `init` を再実行すると保留のマスター鍵が変わるので、古い鍵で封じた項目は読めなくなる。`state.lock` の下で、捨てる前に古い鍵で開いて `write_intent`(保管先に実在する版を指す)だけを `journal/pre-master` へ戻し、封じた項目(`run_end` など、確定を捨てる試みの記録)を捨て、保留を作り直す。新しい鍵ができた最初の追記で戻した `write_intent` が連番の先頭に移り、連鎖は 1 から読める(試験 `TestKeysInitRerunKeepsWriteIntentsAndJournalReadable`)。残るリスク: 確定の途中の `phrase verify` が `repo.json` の応答を失った後に `init` を再実行すると、古い `repo_id` の `repo.json` が保管先に残る(ロックされた版が残るだけ。`write_intent` は戻すので `verify` の誤報にはならない)。

## 範囲外で足したもの

- `internal/contract/` の `def validate` の結果: `schema/cli/results.json` の `def_validate` に `pending_refs`(JSON Pointer の配列、必須)、`subcommands_table.go` の `ResultKeys`、`testdata/cli/results-samples.json`。D-0160 の例外。
- `internal/cli/args.go` の `--invoked-by` を `Call.Args["invoked-by"]` にも載せた(導入中のタイマーの飛ばしを入口が判定するため。F4)。`internal/cli/notifycmd.go` の `notify set` と `internal/acquire/source_enroll.go`・`internal/keys/initstorage.go` に、seal の後の直接実行の `APPROVAL_REQUIRED` を足した(D-0160 の後の差し戻し)。
- 単位の雛形 `internal/contract/timers/systemd.service.tmpl` は変えない。`internal/state/timers.go` が描画の後に 2 行を足す(雛形に入れば二重に足さない)。雛形での正しい差分は報告に書いた。

## 未検証

- 実物の B2(代役のアダプタは B2 の API を呼ばない)、`seal`、通知(Webhook)、`bootstrap.ts` の実機(単体試験のみ)、`make build-app` の実行(`make build-app-test` は引数なしで `build-app.mjs` を呼ぶため、そのままでは止まる。同梱の置き方の組み立ては Makefile の外)。

## レビューの差し戻しで足したもの

- F3: 確定(`phrase verify`)の前の取得は `PHRASE_UNCONFIRMED`(`acquire.Ingest`)。seal の前も取得を止めなくしたため、確定の前に入口が開くのを塞いだ。
- F4: 導入中(seal の前)で `source enroll`・`restore-dest enroll` の記録が欠けている間、タイマーの `backup`・`restore-test` は本体に入らず `{"skipped":true,"reason":"setup_incomplete"}` を返して終わる(通知しない)。結果の形は仕様に無い(スキーマ外の値。仕様の穴)。
- E4 のラッパ・`Ingest` は `sealed: true` では候補へ開かない。
- seal の後の `init-storage`(`--reissue`・`--dry-run` を含む)、`source enroll`(`--rotate` を含む)、`notify set` の直接実行は `APPROVAL_REQUIRED`(`detail.kind`: `storage-init` / `source-change` / `notify-add` または `notify-change`)。`restore-dest enroll` は wp/mp-restore が直した。
- F9: `master.json` が壊れているときは保留の鍵へ切り替えない。F8: 単位に足す場所が無いときは誤り。F10: `bootstrap` が置くのは配布物(`linux-*/`・`prices/`・ソースのアーカイブ・SBOM)だけ。
- F1(裁定済み、D-0162): 台本が `propose seal` を呼び、提案が作られる(`change_id` を確認)。

## 監督の受け入れ(2026-11-03)

- 判定: 統合する。第 2 段は `propose seal` の入口までの通しとして完了とする。`seal` の承認と `apply` は未到達で、第 3 段の始めに通す。
- 再実行(main に rebase した後、新しい環境): 第 2 段 `--- PASS: TestStage2MainPath (49.40s)`、第 1 段 `ok`(10.723s)。Linux 容器で `go vet ./...`、`go test -p 1 ./...`、`-tags sitate_testbuild`、主なパッケージの `-race` が通り、`ok` の行は 55。`make lint-size` は合格。
- 監督の変異(8 件すべて赤): 確定の前の取得を止めない、`seal` の後の `source enroll`・`init-storage`・`notify set` を通す、導入中のタイマーを飛ばさない、別のホストの記録でも解決にする、登録の記録を見ない、壊れた master でも pending へ切り替える。
- 別起動のレビューは差し戻しの前の版に 1 回(F1〜F11)。差し戻しの後の版の再レビューは未実施で、まとめレビューで見る。
- 担当の範囲の外の変更を追認した: `internal/cli/args.go`、`backup.go`、`timerskip.go`、`notifycmd.go`、`internal/acquire/source_enroll.go`、`internal/state/validate.go`。
- 残る迂回は上の記録のとおり: 代役の `provminio` の `capabilities` は固定の表、`bootstrap` は台本が行う、スナップショット鍵は台本が導く、元サーバーの資格情報に環境の鍵を使う。
- `go test ./...` の並行実行の `ssh-host` の 4 件は wp/mp-rest で直す。
- 急がない疑義: 導入中に飛ばした結果 `{"skipped":true,"reason":"setup_incomplete"}` は結果のスキーマに無い。`init` の再実行で戻す `write_intent` の `journal/pre-master` の平文は `probe/` 以外のキー名を含む。`Deps.State` のラッパはプロセス内の状態を持つ。
