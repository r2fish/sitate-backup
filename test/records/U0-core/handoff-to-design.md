# U0-core の受け入れの一覧(設計の監督へ)

対象の commit: `784e3cac1308c0ea69fa57cc4941a2e99e4f9acd`。次の版上げに使う一覧。

## 1. 7.5 の表に足す継ぎ目

| 継ぎ目 | 持ち主 | 利用者 | 中身 |
|---|---|---|---|
| `SSH` | `cli`(結合で実物を注入) | `acquire`、`restore` | `Run(ctx, SSHRequest{SSHConn, Argv か Script, Sudo, Stdin, Stdout})`。`Sudo` が真のときだけ `sudo -n sh -s` |
| `BundleSource` | `cli`(結合で実物を注入) | `restore`(核とチェックの配置)、`keys`(`bin/` の書き込み) | `Files(ctx, osArch) (version, []BundleFile{Path, Data, Sig, Want})`。`Path` は `linux-x86_64/sitate` の形 |
| `OSProbe` | `cli`(結合で実物を注入) | `keys`(`doctor`) | `FSType(ctx, path)` と `Check(ctx, name)` |
| `KeyHome`(契約から外した) | `keys` が自分で持つ | `keys` だけ | 契約にあるのは状態ディレクトリの位置 `Keys.Dir`・`KeyService.Dir` だけ。`keys/master.json`、`keys.latest`、`pending/` は `keys` が読み書きする |

## 2. 型の追加と変更

- `Retention.Class(ClassInput)`、`Retention.HoldSet(ctx, HoldInput)`、`PruneResult.Held` を bool に、`Retention.Offset`、`VersionInfo.RetainUntil`
- `Event` に `RepoID`、`SuppressedCount`、`Next`
- `Codec.Open` に `chunkID`、`Encode`・`Decode` に id と `Header`、`KeyService.NewSnapshotKey`・`OpenSnapshotKey` に `manifestID`
- `Approval.Gate(ctx, line string)`
- `Call.Positional`、`Call.PhraseIn`、`EnrollInput` の `Host`、`AdminRef`、`Admin`、`Rotate`、`StateFiles.LedgerDelete`
- `KeyService.IssueKeys(ctx, IssueRequest) (issued, orphaned, err)`、`RevokeKey(ctx, RevokeRequest) (revoked, notFound, err)`、`OpenedKeyring(ctx, phrase, keyring) (KeyringInfo, err)`。`Master`、`KeyID`、`IssueKey`、`KeyringVersion`、`PendingKeys` は外した
- `SSHKeySecret`、`StorageKeysSecret`、`StorageKeyEntry`、`SourcePullKeyRef = "sitate-pull"`、`RestoreKeyRef = "sitate-restore"`
- `DestProbeOut.OS`・`Arch`、`BundleOSArch`、`store.ObjectStore.Prefix`(`<p>` は `store` だけが付ける)
- `cli.Lease`・`Secrets` の `Dir`、`Keys.Recorder`、`Surveyor.Conn`
- `results.json` に `restore_verify`、`state.json` の `ledger_dest_session.dest` を `{at, key_ref}` に
- `FetchFragmentMax`(1 MiB)、`StreamFragmentMax`(1 MiB)

## 3. 仕様書の修正をお願いしたいもの

- 2176 行「JSON を取る選択肢はオブジェクト」: 1218 行の字句規則 5 と合わない(配列と文字列の引数がある)。試験は 1218 行に従った。
- 復元先の arch の出所: 隔離の検査の出力に `uname -m` を載せる形(回答済み)。3.5 への明記。
- `host_keys[].host`: 4.5 は「復元先の `at`」と書くが、`at` が `ssh://` の形だとスキーマと合わない。ホスト名だけを入れた。
- `orphaned_key_ids`: `ADAPTER_ERROR` の `detail` に運ぶ欄が無い。足すなら U1f-0 の着手前。
- `keys/pending/repo.json`: 4.5 の `pending/` の列挙に無い。U0 が足した。
- `make test-contract` の範囲: `internal/contract/` の全部と、`internal/<部品>/` の `TestContract*`。名前は `contract_naming_test.go` が確かめる。
- 調査の失敗の判定: Linux でないときは終了状態 77、root でなく出力が空で終了 1 か 127 なら `sudo_unavailable`。

## 4. 決めてほしいもの

- `[封筒, 秘密]` の原因コード: 今は `ENVELOPE_INVALID`。回答待ち。
- pull 接続のポート: 仕様書のどこにも無い。今の試験は 22。22 固定で 22 以外を v1 の対象外とするか、`source.host` にポートを足すか。
- 指摘 22: U3a の開始前に、3.1 の範囲の検査に `app/src/i18n/keys.json` と `check.ts` を足すか。

## 5. 実装担当が決めた形

- `sitate.relay/v1`: 1 行目 `{"format":"sitate.relay/v1"}`、見出し(`kind`、`key`、`version_id`、`size`)と本体の繰り返し、最後に `{"kind":"end","objects":n,"bytes":m}`
- `sitate.recvpipe/v1`: `request` 行、`{"in":n}`+本体、`{"in":0}`、`{"out":n}`+本体、最後に `{"exit":s}`。`exit` が無ければ 255
- `read-batch`: tar の直後のゼロ埋めを読み飛ばし、区切りの行 `SITATE-CHANGED/1` と NUL で終えるパスの列
- `sitate.fetch/v1`: 封筒 1 行、4 バイト BE の長さと本体の繰り返し、長さ 0 で終端
- `parseStdoutRecords(stdout, end: StreamEnd = "complete")`、`checkKindArgs(kind, args)`(`app/src/main/`)
- 手順書の記法: 完了確認が複数のときの「n つ目:」、`面:` の要素ごとの付け方、区切りの「、」と「→」。印は `DIST_KEY_FINGERPRINT`、`TIMER_NAMES`、`WARNING_ALLOWLIST`
- 説明書は 10 節。節 10 が「元サーバーの調査と断った候補の扱い」
- カタログのキーの名前空間(`core.code`、`core.why`、`app`、`card`、`chat`、`status`、`phrase`)、置換変数は ICU の 3 形、擬似ロケールは 1.4 倍、上限は 40 文字と 400 文字
- `restore-verify` の入力は `Call.Args` の `expected`、`actual`、結果は `files_failed` と `mismatches[]`
- `dry_run` の列: `subcommands.json`。true は 16 件。道具の「必須」はプロパティを持つことで、`required` には入れない
- `kind` の組み合わせのベクトルから外した 5 種: `storage-init`、`restore-elsewhere`、`restore-session-extend`、`restore-dest-release`、`storage-read-extra`(U3a が足す)

## 6. 版の固定

- Go 1.26.10、Electron 45.2.1、PostgreSQL 17.11(digest 固定)
- MinIO: `cgr.dev/chainguard/minio@sha256:0a01100085e30fb08d2f39e39c9f5c9d83a95707f220b007d813b690cd41622d`(`RELEASE.2026-10-23T20-06-51Z`)、`cgr.dev/chainguard/minio-client@sha256:3b67177c0e12f44f646a5cf21b20fc5a79dd2d7a6b4db632b436f0b9e2e46717`。公式の配布は入手できなかった
- `SURVEY_DU_TIMEOUT_S` は仮に 600(推測。U0-env の後に合成データで確かめる)

## 7. 仕様書 1.2.6・作業指示書 1.0.15 への追従(監督の直し)

D-0157 の申し送り 6 件を直した。そのときに決めた形と、見つけたもの。

- カタログの組のキー: 原因コードと `reason` の組のキーの置き場が無かったので `core.reason.<原因コード>.<reason>` を足した。今は `ARG_INVALID.unsupported_port` と `RESTORE_ENROLL_MISMATCH.unsupported_platform`(引数 `os`・`arch`)の 2 件。
- 判定の順の読み方: `[別の schema, 封筒(phrase_emitted が true)]` は 3.1.4 の順の (3) の「`phrase_emitted` が `true` で秘密のレコードが無い」に当たるので `ENVELOPE_SECRET_MISMATCH` にした(ベクトルの `wrong-schema` を変えた)。
- `detail.os`・`detail.arch`: 復元先が報告した値なので `^[A-Za-z0-9_.-]{1,32}$` に絞った。3.7 の送る欄の行に `os`・`arch` が無い。今はモデルへ送らない。送るなら行を足すお願い。
- 契約の不足: 復元先の `restore` が標準入力の鍵と引数の保管先を受ける口が無かった。`Call.SnapshotKeyIn`(1 行目の `DestKeyLine`。終わりは `cli` が ctx の取り消しで渡す)、`StoreAddr`、`StoreOpener`、`Restore.OpenStore` を足した。7.5 の表に足すお願い。
- 仕様の穴: 空でない `--to` の原因コードが無い。契約試験はコードを問わず、失敗と保管先を開かないことだけを見る。
- 試験の直し: `acquire` の偽の元サーバーが 22 以外のポートで接続していて、ポート 22 固定と合わなかったので 22 に揃えた。`unsupported_port` の試験を足した。`--host` の検査は `source survey` で試す(`source enroll` は同じ規則を U1c の受け入れで見る)。
- `restore` の契約試験は入口の前提(引数の保管先、鍵の秘匿、空でない `--to`)まで。世代の書き出しは 5.6 の完了条件 7 の U1e の試験と主経路の通しで確かめる。
- 範囲の検査はリポジトリに道具が無かったので `test/tools/check_scope.py` を足した。`make test-mainpath` を足した。第 1 段のハーネスは部品の受け入れの後に `test/mainpath/` に置く。

## 8. U1a の受け入れで監督が決めたもの

- マニフェストの圧縮の持ち主: `Codec.Encode`・`Decode` が `SITM` だけ zstd で圧縮・展開する(spec 4.3、4.4、7.5)。形の印が無いので常に圧縮する。呼び出し側は JSON を渡す。展開の上限は `ChunkMax` を仮に流用した。マニフェストの大きさの上限は仕様に無い。値を決めるお願い。
- マニフェスト、エントリ、鍵表の Go の型は `format` に置かない。形の正はスキーマ(`sitate.manifest/v1`、`sitate.entries/v1`、`sitate.chunkkeys/v1`)と `testdata/data` で、書く側と読む側は各 WP が書き、主経路の第 1 段で突き合わせる。7.4・7.5 の `format` の行の「マニフェスト、エントリ、鍵表」は書き方を直すお願い。
- 回答待ち: Argon2id の見出しのパラメータに上限が無い。偽の版の見出しに大きな値を置くと、新しいホストの列挙でメモリ不足か長時間の停止になりうる(4.2「記録を持たない新しいホスト」)。コードを読んだ判断で、実走はしていない。上限の値は 4.3 と M-09 の測定で決めるもので、仕様に無い。U1a の Crypto で拒否するか U1g で拒否するかも決めてほしい。列挙の経路はイテレーション 2 以降なので、主経路は止めない。

## 9. U1f-1 の途中で監督が決めたもの

- `test-tick` の戻す指定: 注釈の `reason: range` は `detail.json` の値域に無かった。`mismatch`(値が今の状態と合わない)に揃えた。
- 回答待ち: 通知の再試行で、アダプタの出力の契約違反(`INTERNAL`)も再試行するか。実装は再試行せず、時限切れ・`delivered: false`・`ADAPTER_ERROR` だけ再試行する。契約違反は同じ物を起動し直しても直らない、という読みで今はこの形。

## 10. U1 の結合に向けて監督が決めたもの(7.5 の表と 3.2.1 に足すお願い)

- アダプタの終了 1: kind 固有の `error.code` も `Run` は誤りで返す(`ADAPTER_ERROR` の `*Error`、`detail.adapter_code` にその値)。`Error.Adapter` に error の全体(`resources[]`、`seen_fp`、`orphaned_key_ids`)を載せ、呼び出し側が 3.1.3 の表で写す。実行器と呼び出し側の前提が違い、結合すると失敗が成功に見えるところだった。
- `detail.adapter_code` のスキーマは `adapter:<name>:<CODE>` を通さなかった(3.1.3 と不一致)。小文字のコードとともに通るように広げた。
- `RunContext.Wrap`: 取得アダプタの容器の起動の口。検証は `adapter`、容器の組み立ては `acquire`(7.5)。
- `Codec.HashStream`: 大きなファイルの照合をメモリに載せない。
- 試験の誤りを直した: 月次のマニフェストの retain-until(+ 12 か月 + 7 日。4.2)、`UNEXPECTED_VERSION`(記録した版 ID のあるキーだけ。3.1.3、4.2)、受け口の `survey` の応答。

## 11. 復元テストの試験で監督が決めたもの

- `config/<def_hash>` のキー: `def_hash` の値はスキーマで `sha256:<64 桁>` なので、キーは接頭辞つきの `config/sha256:<64 桁>` と読んだ。接頭辞なしが意図なら 4.2 に書くお願い。
- マニフェストの写し `definition` は必須(スキーマ)。無いものは照合を飛ばさず `DEF_COPY_MISMATCH` の扱いに揃える。

## 12. U1f-0 のレビューから(回答がほしいもの)

監督の今の扱いを書く。違えば教えてほしい。

- 署名と sha256 の合わないアダプタの原因コード: 3.2.1 に無い。`SIGNATURE_INVALID` は案内が「承認し直す」で意味が合わないので、今は `INTERNAL`(`component`: `adapter`、`reason`: `contract`)にする。
- `/usr/libexec/sitate/` の所有者とモードの定めが仕様に見つからない。検査と起動の間の差し替えの判断の前提なので、「root・0755」と `doctor` の検査を足すか決めてほしい。
- `PR_SET_PDEATHSIG` は直接の子にしか効かず、核を SIGKILL すると孫が残る(規則 13)。アダプタ側の義務か次の実行の掃除の対象かを決めてほしい。
- 要求のスキーマの検証は 3.2.1 の義務に無いが、実行器で行う(仕様より厳しい。呼び出し側の誤りを起動の前に止める)。
- `Validate` が並行に呼べなかったので直した(U0-core の欠陥)。

## 13. U1b のレビューから(7.5 の表に足すお願いと回答がほしいもの)

- `Index` に `SetRetain`・`Records`、`ObjectStore` に `RetainUntil`・`CheckSkew` を足した。チャンクの retain-until の記録が無く、4.3 の「索引の値が目標 A に足りなければ延ばしてから参照する」と `verify` の照合が実装できなかった。`VersionInfo.RetainUntil` は S3 の一覧が返さないので実物はゼロと直した。
- 試験の直し: `StoreProbe.Try` の `extend_retention` を外した(7.5 は試さないとする)。`prune` の試験に `sealed: true` を入れた(無いときは進まない)。
- 回答がほしいもの: 直前の応答の `Date` は、要求が途切れると古くなる。古い `Date` で計算すると `TIME_SKEW` で止まるか、retain-until が短くなる。監督の扱いは、`TIME_SKEW_MAX` の半分より古ければ `BucketConfig` で取り直す。4.2 に 1 文足してほしい。

## 14. U1g のレビューから

- `bin/` のアダプタの実行ファイル: `tools/sign.sh` は署名せず、`adapter.json` の `sha256` で照らす(3.2.1 の「実行ファイル 1 つ + 署名付きの adapter.json」)。
- `bucket check` も `BUCKET_MISMATCH` を返す(Compliance でない、バージョニング無し)。`init-storage` の合格だけに頼らない。
- `init` の Argon2id は `Crypto` を通らない(`Keys` に口が無い)。値は定数の初期値で上限の内側なので認める。開く側は `Crypto` を通す。
- `ChunkID` などは誤りの口が無いので panic する。`cli` が `*contract.Error` の panic を受けて `INTERNAL` の封筒にする。

## 15. U1e のレビューから

監督の今の扱い。違えば教えてほしい。

- 復元先の自己申告(3.5 の表の 9): 照合の実行場所が仕様に無い。核はマニフェストの `targets` の `files`・`bytes` の和を期待値にし、復元先の報告と違えば `RESTORE_MISMATCH`。0 件の報告で合格にしない。内容ハッシュの照合は復元先の核に任せる(乗っ取られた復元先の偽りは T-68 の範囲と読む)。
- 3.5 の表の 1 の矛盾: 「egress はキーリング 1 個分」と「保持中のマニフェストの鍵 ID がすべて含まれる」。`snapshots[]` に `key_id` が無い。今は見出しだけを読み(`Codec.Header`)、読んだ量を `egress_bytes` に足す。`snapshots[].key_id` を足す案を検討してほしい。
- `restore_failed` を同じチャンクを参照する世代に広げるのは見送り(全世代の鍵表を読むので読み出しが増える)。3.5 の文と違う。削除停止は掛かるので失われない。イテレーション 3 へ送るか決めてほしい。
- 読み出し量の累計と予約の判定はイテレーション 3 なので `restore-test` から外す。
- `SSHRequest.Argv` の引用の規則、`StoragePrefixDefault`、`LedgerAppend` の置き換えを契約に書いた。
- `ssh_from` が無く観測の口も無いので、`ssh_from` が無ければ `DEF_INVALID`。

## 16. U1d のレビューから

署名なしで適用できる経路は見つからなかった。ただし次を監督の裁定で直している。違えば教えてほしい。

- `def-change` の混入の判定(1944 行、A6 (12)): 7.5 と作業指示書 5.17 は `cli` の持ち分としているが、`propose` の入口にある `approval` に置く。持ち主が曖昧なまま誰も作らない形だった。定義を受ける 3 種別で同じ判定を通す。7.5 の行の修正をお願いする。
- `notify-add` は変更前が変更後の部分集合、`restore-test-increase` は頻度が実際に上がること、も `approval` で判定する。
- `Approval` に `TestNotifier` を足す。`notify-add`・`notify-change` の確認語は Planner に任せず核が計算する。
- `approval_delay_hours` の上限が無く、桁あふれで遅延を約 25 分にできた(下限は 7 時間)。上限を 8760 時間(1 年)に仮置きする。値は推測で、決めてほしい。4.1 とスキーマに足すお願い。
- 中断した適用の再開でも `verifyStored` で署名を照らす(承認鍵の変更後は 1895 行の「(a) だけ」のまま)。
- 秘密は `--id` と一致する変更にだけ渡す。ほかの再開は `HUMAN_SECRET_REQUIRED`。仕様に 1 行足すお願い。
- `propose restore-elsewhere`・`storage-read-extra` の `--to` もローカルへの復元の拒否を照らす(3.1.5 は `restore` だけを書く)。

U1f-1 の途中から: `--snapshot-key-stdin` の標準入力の終わりの `RUN_ABORTED` の `detail.reason` に切断の値が無く、`signal` に寄せた。

## 17. U1b の差し戻しから

- `prune` は `Index` が無ければ止まる(結線の漏れで消した版が索引に残らないように)。契約試験の前提を直した。
- `write_intent` は `state.json` の項目でなく `journal/` の種別なので、`Retention.Intents` を足し、`cli` が記録から渡す。7.5 の表に足すお願い。
- 試験ビルドの `verify` で「`LastModified` にオフセットを足して比べる」は入れていない(偽の保管先は模擬時間で二重になる)。実の保管先にオフセットを与える運用は無い前提。

## 18. U2a のレビューから(仕様の判断がほしいもの)

監督の今の扱いを書く。

- 根がシンボリックリンクのとき: `/etc/ssh` などの下に `sitate-ro` の ACL が付く。段 `roots` で `realpath` した値も 4.1 の (i)〜(iii) で検査する(核が一覧を埋め込む)。3.3 に足すお願い。
- 既定 ACL の副作用 2 つ: アプリの umask 077 が効かなくなり新しいファイルが他者に読める場合がある。`mode` の群のビットが ACL の mask に変わり、0600 が 0640 として記録・復元される。どちらも実証済み。直し方に副作用があるので、3.3 と 5.3 の扱いを決めてほしい。今は変えない。`mode` は元の値を取れないので残るリスクとして記録する案を推す。
- DB の対象のクラスタの選び方が U1c(`port` か 1 つだけ)、U2a、3.3(DB 名で選ぶ)で揃わない。`db_only` の調査では DB 名が読めない。今は U1c の形(`port` 指定か信じるクラスタが 1 つ)で、複数なら `cluster_ambiguous`。
- 取得の途中でリンクの中間の要素を差し替えると `sitate-ro` の権限の範囲で根の外を読む。`setfacl` に nofollow が無い。5.3 の残るリスクに書くお願い。
- `from=` が空の `source-setup` は成功にしない。`sudo` の経路では `SSH_CLIENT` が消えるので、核が `From` を埋め込む(設定の持ち主は `cli`)。

## 19. U2a の差し戻しから

- `SOURCE_SETUP_FAILED` の `reason` に `from_missing` を足した(`SITATE_FROM` が空か不正)。3.1.3 と 3.3 に足すお願い。
- 名前に `\001` を含むファイル 1 つで受け口の `list` が止まり、対象全体が毎回 `failed` になる。3.3 の `list` の形に報告の形が無い。そのファイルを取らずに報告する形か、今の形かを決めてほしい。
- 全対象が `failed` のときの結果の `targets[]`: 3.2.2 は `error_code` を結果に置くが、失敗の封筒は `result: null`。今は `run_end` にだけ載る。

## 20. U2d、U2e、U2g、U1f-1 から

- `provision` の `error.code` の列挙に `KEY_MISSING_PERMISSION`、`TIME_SKEW` を足した(3.2.1 の表のとおり。無いと実行器が `INTERNAL` にする)。
- 回答がほしいもの: `revoke-keys` の要求に `prefix` が無いのに「別のプレフィクスの印を拒否」を求める(3.2.6。今は鍵の `namePrefix` から名前を作って照らす)。`provision` の `TIME_SKEW` の判定の持ち主。
- 回答がほしいもの: 通知の送信の全体の時限(今は 30 秒で、核の打ち切りと同じ値)。追加の CA のファイルが壊れているときの `reason`(今は `tls`)。
- 回答がほしいもの: `enroll` の初回のホスト鍵(固定値が無く、初期資格情報が未検証の相手に渡りうる。T-63)。`stop` の残存の判定(セッションごとか、切れた SSH の処理の扱い)。
- 回答がほしいもの: `estimate` の「記録から」の入力の形、`--daily-gb` を省いたときの引き方、価格表の `request` の `ops` の語彙、`hold-release` を停止していないときに提案できるか。事業者の管理用鍵を「キーチェーンから消す」記述と no-save の併存(今は no-save)。
- B2 の API の形は実物で未検証。利用者の鍵で確かめる。

## 21. U2d のレビューからの持ち越し(2026-11-03)

- F7: 復元先の `enroll` の要求に `host_key_fp` が無く、再登録でも初期資格情報を照合より先に渡す(T-63)。契約と仕様の変更が要る。
- F5 の後半: 帰属できない処理が `stop` の `remaining` に載っても、`inspect` が `absent` なら核が台帳を消す。扱いを仕様で決める。
- D1: `remaining` を session ごとに分ける解釈。製品の監督は実装のままとし、仕様の文の追従を求める。

## 22. U5 の報告からの持ち越し(2026-11-03)

- スキーマと仕様の不一致: `provider`(ident)が空白と日本語を許さず、完了条件 (d) の「入力と等しい」と両立しない。`endpoint_patterns` の字種に `_` が無く `<account_id>` が通らない。`source_url` の空白が url の型に合わない。
- 仕様の穴: `tier_upto` の「次の N TB」が幅か累積か、`<region>`・`<bucket>` 以外のプレースホルダの照合、月額の基本料金と最低料金を持つ欄、桁が文字の無料枠。
- ops の語彙: 入力の区分名(`class_a`、`write`、`all` など)と `estimate` の数え方の写像を、仕様 6.1.1 (3) で決めるか。

## 23. `kit confirm --kit-version` の値の種類(2026-11-03)

- `gatedata/allowlist.json` の `int` を `word` に直した。`kit_version` は 12 文字の 16 進(spec 4.7)で、`int` では実在の版を渡せない。

## 24. U3a の範囲の逸脱の取り下げ(2026-11-03)

- 製品の監督が起動プロンプトで `restore fetch` の展開器を指示した。作業指示書 5.14 の作らないものと D2-01 に反する。監督の誤りで、担当に取り除かせる。

## 25. フレーズの再入力の後に語を見直せない(2026-11-03)

- spec 3.7 のとおり再入力の提出時に 24 語を消す。写し間違いの `PHRASE_INVALID` では `init` をやり直す形になる。導入の体験に効くので、この形でよいか確かめたい。

## 26. U5 のレビューからの持ち越し(2026-11-03)

- 22 節に足す。`all` の語の当てはめと、当てはめの無い区分を `PRICE_UNKNOWN` にするか 0 にするか(7.2 の写す規則 1 と `estimate` が食い違う)。
- R2 の無料枠(100 万件が数の一致で写せない)の置き方。月額の基本料金と最低課金の置き方(小さな入力で過小。`over_ceiling` が偽のまま上限を超える場面がある)。
- `tier_upto` は表の中に幅と累積が混在する。差は 40 TB 超で 1 % 未満で、標準例と上限例では出ない。
- (d) の「文字列として等しい」に対する置換 4 種の例外の明記。

## 27. 復旧キットの国の欄(2026-11-03)

- `storage.country` は `null` のまま。4.7 は `def show` の `storage_country` と同じとするが、`internal/keys` に価格表の入力が無い。紙の「保管先の国」の欄が空になる。
- 手順 (5) の `<run_id>` を (3) の `$RUN` と同じ値に読んだ。

## 28. U1f-1 のレビューからの裁定(2026-11-03。追認を求める)

仕様書に文が無く、受入試験の環境を止めるので、製品の監督が次のとおり決めて進める。

- タイマーの置き場: 雛形は `/etc/systemd/system`・`/etc/cron.d`・`User=sitate` で、`sitate` で動く `init` は書けない。spec の `timer_permission`(そのユーザーの単位を常駐、`sitate` の crontab)に合わせ、ユーザーの単位 `~/.config/systemd/user` と `crontab -` にした(`timers.json`、雛形、golden を変更)。
- `from=` の値(`acquire` の `From`): spec に出所が無い。`SITATE_FROM` と状態のファイルをやめ、`source enroll` の接続の `SSH_CONNECTION` の先頭(元サーバーから見た核の送信元)から取る。`restore.ssh_from` の「核が観測した送信元」と揃える。
- `RunCleanup` の呼び出し条件: 作業指示書 5.17 より広い spec 3.5、7.5 に従う(中断を記録しなかったときも呼ぶ)。
- `SITJ` の見出しの印: spec どおりマスター鍵の鍵 ID とする。

## 29. 復旧キットの紙の読みやすさ(2026-11-03)

- 仕様の字面を表 1 面に載せると、本文 5.8pt、等幅 5.3pt になる。裏はほとんど余っている。(2) の説明の一部を裏へ移す割り付けの変更を認めるか。
- (5) の `<run_id>` を (3) の `$RUN` の値で刷った。4.7 の「`$RUN` は (3) にだけ」は `--run-id` の意味に読んだ。

## 30. U3a のレビューからの持ち越し(2026-11-03)

- M9: キーチェーンのサービス名を `repo_id` で分ける完了条件 14。`repo_id` が決まる前に保存した項目の引き継ぎの規則が読み取れない。切り替えると `init` の前に入れた参照名が見つからなくなる。今は固定のサービス名のまま。
- 参照名の字種: アプリの入口は `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` で、契約の `ref_pattern` より広い。
- 手書きの道: `kit confirm` は印刷の成功の後だけにした。4.7 の「刷り込むか手書き」は、紙を刷って語を手で書く意味に読んだ。
- 登録済みの接続先と host か port が違う `bootstrap` は `SSH_HOSTKEY_MISMATCH` で拒否する。

## 31. 主経路の第 2 段で見つけた仕様の穴(2026-11-03。仕様書 1.2.8 を見た時点)

詳細は `test/records/mainpath/stage2-2026-11-03.md`。監督は 1.2.8 の改版履歴に該当の語が無いことだけを確かめ、本文の全文照合はしていない。

- S1 `def validate` は参照名の未解決を `DEF_INVALID`(`unresolved_ref`)にするが、`storage[0].key_ref` は `init-storage`(手順 5)で、`restore.dest.key_ref` は `restore-dest enroll`(手順 9)で初めて存在する。手順 4 の `def validate` は必ず止まる。A2 の導入が止まるので、裁定が要る。案: 候補の検査では未解決を許す / 手順 4 では参照名を検査しないモードを足す。
- S2 手順 5 の `bucket check` は引数なしと読めるが、`seal` の前は `--key-ref` の既定が無く `ARG_INVALID`(`missing`)。案: 手順書に `--key-ref` を書く / 候補の定義の `storage[0].key_ref` を既定にする。
- S3 `secrets set` がイテレーション 3 のため、イテレーション 1 では B2 以外の保管先に鍵を置く手段が無い。A2 は B2 なので測定は止まらない。受入試験の環境の MinIO は試験用の代役のアダプタで通す方針で、仕様の変更は求めない(確認だけ)。
- E3 の関連: 記録の journal 鍵は master から導くが、`init` の前の `init-storage` には master が無い。仕様の答えが見つからなければ、担当が最小の形を選び、次の節に足す。

## 32. 主経路のレビューからの裁定の依頼(2026-11-03)

### 32.1 裁定が要る(手順 10 が止まる)

`ssh-host` で `restore.dest.key_ref` が解決せず、`propose seal` が必ず `DEF_INVALID`(`unresolved_ref`)になる。監督がコードで確かめた。

- `propose seal` は `ValidateDef(..., false)`(`internal/approval/defs.go:49`)で、解決は `Secrets.Exists`(`internal/cli/secrets.go:31`)が `secrets/<ref>.json` の有無を見る。
- 仕様 2 章の秘密の表は「`ssh-host` の初期資格情報はバックアップサーバーのディスクに置かない」。`restore-dest enroll` は標準入力で受けて渡すだけで保存しない。
- 仕様の `def validate` の行と D-0160 は `restore.dest.key_ref` を「手順 9 で置かれる」とする。`ssh-host` では置かれないので食い違う。
- 案 A: `ssh-host` では `host_keys[]` の `role: restore_dest` と `restore_dest_enrollment` の記録があれば解決とみなす。案 B: `ssh-host` の `key_ref` を定義の参照名の検査から外す。案 C: 定義の必須から外す。

### 32.2 追認を求める(担当が選んだ形。D-0160 の依頼どおり)

`init` の前の journal 鍵: マスター鍵がまだ無い間(`init` の前の `init-storage`)は `write_intent` だけを `journal/pre-master` に平文で退避し、鍵ができた最初の追記の前に連番の項目として移す。中身は `probe/` の乱数のキー名だけで、保管先へ写らない。`keys/pending/master.json` がある間は journal の鍵だけをその鍵から導き、項目は状態ディレクトリにだけ置き、確定の後の `SyncRemote` が写す。仕様 4.5 の「`journal/` の項目は暗号化」から外れるのは pre-master の 1 ファイルだけ。

### 32.3 疑義(急がない)

- `ExecStart=-` は段を独立に走らせるために読めるが、systemd の結果が常に成功に見え、`OnFailure=` が使えない。今は変えない。
- 「アダプタの無い事業者」の判定の出所が仕様に無い。配布物は常に `b2` を同梱し、`bucket check` は `--adapter` を受けないので、B2 以外の事業者でも `b2` の `check` を呼ぶ。`untested` の経路に実物の入口が無い。
- seal の前は候補の復元先へ `restore-test` がスナップショット鍵を渡す(D-0025 の 5 の範囲。E4 の修正で初めて動く)。乗っ取られたエージェントが seal の前に候補の `restore.dest` を替えると、取得済みのデータを読める。消せるようにはならない。残るリスクの表にあるかを確かめてほしい。
- `mlock`・`PR_SET_DUMPABLE` の呼び出しが `internal/`・`cmd/` に無い(仕様 3.1.1「鍵のメモリ」)。実装の欠けとして次の WP で扱う。
