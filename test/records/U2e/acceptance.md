# U2e の受け入れの記録

- 再実行: 2026-11-03 JST、対象の作業ツリーで監督が実行。照らした文書は仕様書 1.2.7、作業指示書 1.0.16。

| コマンド | 結果 |
|---|---|
| `make test-adapters ADAPTER=notify/webhook`、`go test -count=1 ./adapters/notify/...` | `ok` |
| `check_scope.py --allow adapters/notify/webhook/`、`make lint-size` | PASS、終了 0 |

## 変異(監督が実行)

4 通りすべて赤: `http` を許す、リダイレクトをたどる、TLS の検証を外す、プロキシの環境変数を読む。

## 受入試験の環境の hook での確認(実装担当の実行。監督は再実行していない)

追加の CA あり `delivered: true`、CA なし `reason: tls`、`/_control/fail` で 503 の後に受理、`http://` は `url_invalid`。

## 残したもの

- 別の起動のレビューは受けていない(小さな部品で、凍結の時点の公開面のレビューに含める)。
- 送信の全体の時限(30 秒)が核の打ち切り(`AdapterTimeoutNotify` 30 秒)と競合しうる。設計の監督の回答待ち。
- 追加の CA のファイルが壊れているときの `reason` が仕様に無い。今は `tls`。
