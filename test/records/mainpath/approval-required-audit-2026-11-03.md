# seal の後の直接実行の洗い出し(APPROVAL_REQUIRED。2026-11-03)

- 観点: spec 3.6 の「危険」の表と 3.1.2 の「危険」列で、`seal` の後は提案(`propose`)を要するとされるサブコマンドを、`internal/cli/dispatch.go` から直接呼んだときに `seal` の後で拒否するか。
- 事実: 製品コードで `CodeApprovalRequired` を返す箇所は、本 WP で足した `internal/restore/dest_cmd.go:27-35`(`restore-dest enroll`)の 1 か所だけ。`state.json` の `sealed` を読んで拒否する箇所はほかに無い(`sealed` を読む箇所は、判定の記録・段階の判定・警告の出し分けの用途)。
- 判定はコードの読みで行い、`restore-dest enroll` だけ試験で確かめた(`TestEnrollAfterSealNeedsRestoreChange`)。ほかは実行していない(未検証)。

| サブコマンド | 要る提案(spec) | dispatch の入口 | seal の後の直接実行 | 壊れ方 | 担当 |
|---|---|---|---|---|---|
| `restore-dest enroll` | `restore-change`(3.1.2 表 368 行) | dispatch.go:93 → `internal/restore/dest_cmd.go:23` | 拒否する(`dest_cmd.go:27-35`。`state.json` の `sealed` を直接読む。`--dry-run` も拒否) | 修正前は承認なしで再登録でき、`--host-key-fp` で固定したホスト鍵を置き換えられた | 本 WP で修正(試験 `TestEnrollAfterSealNeedsRestoreChange`。`apply` の `RestoreDestEnroller`(`dest.go`)は止めない) |
| `source enroll` | `source-change`(表 365 行) | dispatch.go:105 → `internal/acquire/source_enroll.go:343` | 拒否しない。`readDef`(356 行)の後、そのまま `d.enroll`(362 行)で `WriteState: true`。`sealed` は 165 行で自己診断の項目の扱いにだけ使う | 承認なしに元サーバーの受け口の再設置・`host_keys[]`・`receiver_hash`・`source_clusters` の書き換え。`--rotate` も同じ | 範囲外(acquire)。監督 |
| `init-storage` | `storage-init`(表 356 行。`--reissue` を含む) | dispatch.go:39 → `internal/keys/initstorage.go:56` | 拒否しない。`sealed` の参照が無い(`storageTarget` は `bucket.go:175` で定義の読み先を選ぶだけ) | 承認なしに鍵の再発行と旧鍵の失効、バケット設定の変更(管理用鍵が差し込まれた場合) | 範囲外(keys)。監督 |
| `storage issue-read-key` | `read-key-issue`(表 357 行。未初期化の新しいホストは承認なし) | dispatch.go の routeParts に無い → NOT_IMPLEMENTED(`internal/keys/skeleton.go:43`) | 動かない(NOT_IMPLEMENTED) | 実装時に `sealed` と `initialized` の判定を要する | 範囲外(keys)。実装 WP |
| `secrets set` | 参照中の参照名は `SECRET_REF_IN_USE`(表 359 行) | routeParts に無い → NOT_IMPLEMENTED(`internal/keys/skeleton.go:55`) | 動かない(NOT_IMPLEMENTED) | 実装時に必要 | 範囲外(keys)。実装 WP |
| `notify set` | 宛先追加は `notify-add`、削除・差し替えと資格情報の差し替えは `notify-change`(表 372 行) | dispatch の routeOwn(`internal/cli/notifycmd.go:15`) → `notifySet`(`notifycmd.go:34-60`) | 拒否しない。`sealed` の参照が無く、秘密ファイルを `d.Secrets.Put`(55 行)で置き換える | 承認・確認語なしに通知の資格情報(Webhook URL など)を差し替えられ、通知が攻撃者の宛先へ流れる | internal/cli の restore-dest・gate の外。監督が別に直す(`wire*.go` ではないが mp-unbypass の範囲外とも限らないため触っていない) |
| `restore-dest release` | `restore-dest-release`(表 371 行) | routeParts に無い → NOT_IMPLEMENTED | 動かない | イテレーション 4 の範囲 | — |
| `restore-dest teardown` | 提案なし(読み取り以外の破棄。承認の対象外) | 同上 | 動かない | — | — |
| `restore`(`--to ssh://`・`source:`、`--hold-hours`) | `restore-elsewhere`・`restore-session-extend` | dispatch.go:81 → `internal/restore/inplace.go:101` | 入口は空のディレクトリへの `--to` だけ(`--paths`・中継・保持時間は未実装)。`--to ssh://`・`source:` は gate の許可リスト(`restore_to_prefixes`)の外で `GATE_DENIED`、核の直接呼びは restore-elsewhere を実装していない | gate を通らない直接呼び(`inplace.go:108` は `--to` を絶対パス化するだけ)で、バックアップサーバー上の任意の空ディレクトリへの復元は承認なしにできる。仕様の「バックアップサーバー自身への復元は gate が拒否」に頼る | 範囲内だが、核に宛先の種類の区別が無く、仕様の復元先の提案(`restore-elsewhere`)の実装はイテレーション 2 以降。変更なし |
| `restore-test --force`(読み出し量が計画を超える) | `storage-read-extra`(3.1.2.6 の分岐 4) | dispatch.go:87 → `internal/restore/rtest.go` `RestoreTest` | 拒否しない。予約と累計の判定が無い(`storage-read-extra` を返す箇所なし。累計の規則はイテレーション 3 の範囲) | `--force` を繰り返しても承認を要しない | イテレーション 3(作業指示書 7 章)。変更なし |
| `hold set` | 提案なし(停止は安全側) | routeOwn(`internal/cli/holdcmd.go:14`) | 該当なし | — | — |
| `def validate` | seal の後は読み取りのみ | routeOwn(`internal/cli/defcmd.go`) | 保存しない(`defcmd.go:58-61`) | — | 確認のみ |
| `init` | 確定済みは `ALREADY_INITIALIZED` | dispatch.go:32 → keys | `keys.Init` | 未確認 | 範囲外 |

## 範囲外で監督に直してほしいもの(優先順)

1. `source enroll`(`internal/acquire/source_enroll.go:343`): `state.json` の `sealed` が真なら `APPROVAL_REQUIRED`(kind `source-change`)を `readDef` より前で返す。`apply` の `Enroller`(`SourceEnroller.Enroll`、同 44 行)は止めない。
2. `init-storage`(`internal/keys/initstorage.go:56`): `sealed` が真なら `APPROVAL_REQUIRED`(kind `storage-init`)。
3. `notify set`(`internal/cli/notifycmd.go:36`): `sealed` が真なら、追加か差し替えかに応じて `APPROVAL_REQUIRED`(kind `notify-add` / `notify-change`)。`seal` の前と `init --adopt` の後は従来どおり。
4. `storage issue-read-key`・`secrets set` は実装時に同じ検査を入れる(未実装のため今は動かない)。
