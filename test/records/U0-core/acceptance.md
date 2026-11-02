# U0-core の受け入れの記録

- 対象の commit: `784e3cac1308c0ea69fa57cc4941a2e99e4f9acd`
- 再実行の日時: 2026-11-02 19:17〜19:20 JST(監督が対象の commit の直前の作業ツリーで実行。アーカイブの確認は commit の後)
- 照らした文書の版: 仕様書 1.2.5、作業指示書 1.0.14

## 読み方

骨格は `NOT_IMPLEMENTED` で止まるので、骨格での赤と「その他 0」は完了条件 3 の確認だけで、判定の品質の証拠ではない。骨格を相手にした実測では、失敗した行より手前の判定の行は 494 行中 8 行(1.6%)だった(`test/tools/cov_stats.py` の集計の元の版。2026-11-02)。

参照実装は試験と同じ担当が書いたもので、リポジトリの外に置いた。判定に届く証拠としてだけ扱い、正しさの証拠にしない。

判定の品質は主経路の実装と各 WP の受け入れの変異(テスト仕様書 6.2)で確かめる。

## コマンドと結果

開発用コンテナ(`go1.26.10 linux/arm64`)は `make shell-linux CMD='…'` の形で実行した。

| コマンド | 終了コード | 集計 |
|---|---|---|
| `go build ./...`、`go vet ./...`、`go vet -tags sitate_testbuild ./...`、`gofmt -l`、`go test -count=1 ./internal/contract/... ./tools/...` | 0 | `internal/contract`、`fake`、`fake/s3server`、`harness`、`tools/lintinstall`、`tools/lintsize` がすべて `ok` |
| `make test`(コンテナの中) | 2 | 骨格の未実装による。panic とビルドエラーは 0 件 |
| `make shell-linux CMD='make test …; exit $$r'` | 2 | コンテナの中の終了コードも 2 で一致(完了条件 6) |
| `make test-contract` | 2 | 骨格の未実装による |
| `make contract-status` | 0 | 全 WP で「未実装」1 件以上、「その他」0。U1a 15、U1b 18、U1c 34、U1d 13、U1e 32、U1f-0 5、U1f-1 30、U1g 47、U2a 3、U2b 1、U2d 1、U2e 1、U2g 1、U3a 12、U3b 1、U4 1、U5 1 |
| `make test-adapters` | 2 | 骨格の未実装による |
| `make lint-size` | 0 | 合格。commit の後に `LINT_BASE=<対象の commit>` でも 0 |
| `make lint-install` | 2 | `docs/install.md` が骨格のままのため(U4 の未実装) |
| `make build`(commit の前) | 0 | 依存の検査 OK。HEAD が無いのでアーカイブを飛ばす警告 |
| `make build`(commit の後) | 0 | 依存の検査 OK。ソースのアーカイブと SBOM(SPDX-2.3、459 ファイル)ができた |
| `make build`(作業ツリーに差があるとき) | 0 | 未 commit の差の警告が出た |
| `make build-test` | 0 | |
| `make test-app` | 2 | `tests 44 / pass 31 / fail 13`。実装担当の報告では赤はすべて未実装の印 |
| `npm --prefix app run typecheck`、`npm --prefix app run lint` | 0 | |
| `npm --prefix app run test:u3a`、`test:u3b` | 1 | 骨格の未実装による |
| `test/tools/check_send_fields.py` | 0 | 仕様書 1.2.5。`rows 51/51`、`pointers 353/353`、`path_ref 7/7`、`diffs=0`、PASS |
| `test/tools/check_send_fields_schema.py` | 0 | `missing=0`、PASS |
| `make test-env-up NAME=<n>` と `TestExternalS3ObjectLock`(MinIO) | 0 | `--- PASS`。停止後にコンテナは残らない |
| 同じ(`KIND=fake`) | 0 | `--- PASS`。停止後にコンテナは残らない |

## 判定に届くことと、判定を壊して赤になること

作業端末の Go(1.27.2)で、複製に参照実装を重ねて実行した。リポジトリの外の置き場が開発用コンテナに共有されないため。

- `internal/` の契約試験: `pass=189 fail=0 skip=6`、`cov_stats.py` は終了 0。骨格で判定の前に止まっていた `TestContractRetentionPruneUsesDatePlusOffset` は、判定の直前の 2 か所の印がどちらも出て合格した。
- 規則を 1 つずつ壊した結果:

| 部品 | 壊した規則 | 結果 |
|---|---|---|
| `approval` | `gate` の字句の検査 | 赤(`TestContractApprovalGateDenied`) |
| `approval` | 未知のサブコマンドの拒否 | 赤(同上) |
| `format` | AAD から ID を外す | 赤(3 件) |
| `format` | AAD から見出しの印を外す | 赤(2 件) |
| `store` | retain-until を付けない | 赤(1 件) |
| `store` | Compliance でなく Governance | 赤(3 件) |
| `store` | `Content-MD5` を付けない | 緑のまま(下の「穴」) |
| `keys` | `bin/` の署名の検証を飛ばす | 赤(2 件) |
| `keys` | `write_intent` を書かない | 赤(2 件) |

## 確かめていない範囲(緑に数えない。テスト仕様書の Q12)

- `format`、`state`、`approval`、`store` の試験の 1 件ずつの仕様との照合
- 価格表の項目単位の照合
- アダプタ(`provision`、`notify`、`check`、`acquire`)の項目単位の照合
- 実物の SSH と B2 での結合。`SSHRequest` と `IssueKeys`・`RevokeKey` は偽物と要求のスキーマの検査まで
- `make sign` の端末での対話(使い捨ての鍵で標準入力を配管した確認まで)と、`make sign-test` の実走の一部
- 実 systemd の `TimeoutStartSec` と cron の `@reboot`
- x86_64 での MinIO の起動
- 実際の API が `sitate_propose` の平らな入力スキーマを受け付けるか(U3a の最初の起動)
- `keyring_version` の「確定のたびに 1 増える」(版を増やす側がイテレーション 4 以降)

## 穴として残すもの

- `approval` の署名の検証: 参照実装の `Approve` は `NOT_IMPLEMENTED` のままで契約試験が合格する。承認の実行条件(承認鍵の署名)は契約試験で判定していない。U1d の受け入れの変異で必ず確かめる。主経路の通しでは確かめられない。
- `store` の `Content-MD5`: 付けなくても契約試験が通る。U1b の完了条件と受け入れの変異で確かめる。
- `Run` の本体: 定数を返す `Run` が `acquire` の `Run` の試験を通る。`Run` の正常系と `Judge` は U1c 自身の試験に回す(設計の監督の了承)。
- 手順書と道具の残り: `check_send_fields_schema.py` の `/error/zzz` の検査は未着手。Go の変異 1 件(`harness.ClassifyStream`)は当たらず、走っていない。
- 後回しにした指摘: `contract_seams_test.go` の kind と予約名の拒否、`bin/` のソースのアーカイブと SBOM の fixture、`Timers`・`Proposer`・`Acquire` の Recorder、`harness/adapter.go` の `reqs[0]` と終了 1、`retryable` の強制と環境変数 2 つ、`lintsize` の FuncLit と ESLint の許可リスト。

## 再レビュー

レビューは実装者と別に起動し、全体を 2 回、指摘の閉じ確認を 1 回行った。閉じ確認の後に見つかった 2 件(`RevokeKey`、台帳の `dest`)を直した。その 2 件の直しは閉じ確認の後なので、別の起動のレビューを受けていない。
