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
