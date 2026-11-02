# U1f-1 の受け入れの記録

- 再実行: 2026-11-03 JST、監督が最終の作業ツリーで実行。照らした文書は仕様書 1.2.7、作業指示書 1.0.16(5.17)。

| コマンド | 結果 |
|---|---|
| `go test -count=1 ./internal/{cli,state,acquire,contract,keys,approval,restore,store}/` | すべて `ok` |
| 同じ、`-tags sitate_testbuild` | すべて `ok` |
| 同じ、`-race` | すべて `ok` |
| `cov_stats.py`(`TestContract`、`cli` と `state`) | pass 31、fail 0、skip 2、`failed_asserts_reached=0/0`。skip は読み出しの精算(イテレーション 3) |
| `make build`、`make build-test`、`make lint-size`、`check_scope.py` | 成功、成功、合格、`PASS` |

- 変異(監督が実行): 失敗の封筒に秘密を添える、秘密を消さない、cron の `timeout` を外す、再試行を 1 回にするの 4 通りがすべて赤。修正前は緑だったものを含む。LOCKED の判定、前の印の処理、`last_backup.ok`、`run.stuck` は修正前の版で赤を確かめた。
- 別の起動のレビューで高 7 件を差し戻した。監督は修正の後の再レビューをしていない。
- 新しい試験は、規則を壊した版での赤の記録はあるが、修正前のコードでの赤の記録は無い。

## 確かめていないこと

- 実物の systemd(`--user` と `enable-linger`)と cron、実物の B2(`Region`)、実物の SSH での `SSH_CONNECTION` の観測。主経路の第 2 段で確かめる。
- 完了条件 11 は担当の実行だけで、監督は再実行していない。

## 範囲の外の変更

- `internal/acquire/source_enroll.go`、`types.go` は監督が許可した。`ingest_wiring_test.go` の直しと `observe_from_test.go` は担当が足した。本体の変更に伴うものなので監督が認めた。
- `go.mod` の `golang.org/x/text` の直接依存は監督が直した。

## 残したもの

- M10: `SITJ`・`SITG` の印のマスター鍵の鍵 ID を得る口が `KeyService` に無い。監督が判断する。
- `verify`・`extend`・`prune` は `NOT_IMPLEMENTED`(5.18 の U1f-2)。`estimate` は `Deps.Prices` が未結線。
- `keys.Keys` の `init` はタイマーの登録の前に状態を書く(U1g)。
- `--run-id` 付きの `completedBefore` は `journal` が読めないと `INTERNAL` を返す。
