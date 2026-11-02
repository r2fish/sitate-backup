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
