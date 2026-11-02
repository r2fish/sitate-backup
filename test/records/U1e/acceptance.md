# U1e の受け入れの記録

- 再実行: 2026-11-03 JST、対象の作業ツリーで監督が実行。照らした文書は仕様書 1.2.7、作業指示書 1.0.16。

| コマンド | 結果 |
|---|---|
| `go test -count=1 ./internal/restore`(製品ビルド、`-tags sitate_testbuild`、`-race`) | `ok` |
| `test/tools/cov_stats.py --package restore` | `tests pass=35 fail=0 skip=1`、PASS。skip は `JournalVerify`(イテレーション 2) |
| `check_scope.py --allow internal/restore/`、`make lint-size` | PASS、終了 0 |

## 変異

監督が実行: 隔離の検査の失敗を通す、復元先の報告の照合を外す。実装担当の記録ではほかに多数が赤で、root の所有の確認(root が要る)だけが赤にならない。

## レビュー

別の起動のレビューの高 8 件(dump の mode、偽の版での停止、`start` の打ち切りの後の台帳、鍵 ID の見逃し、自己申告での合格、標準入力の終わり、既定のプレフィクス、隔離の検査の道具の欠け)ほかを直した。固定した指紋が無いときに鍵を渡す経路も止めた。差し戻しの直しの後の版は別の起動のレビューを受けていない。

## 確かめていない範囲

- 実物の `SSH`、`AdapterRunner`、`format`、`store` との結合(第 1 段以降)。台本(probe、place)は実機で未検証。
- root で動く復元、16 MiB を超えるファイルの実メモリ。
- 内容ハッシュの照合は復元先の核に任せる。設計の監督の回答待ち(`test/records/U0-core/handoff-to-design.md` の 15 節)。
