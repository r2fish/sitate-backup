# U1c の受け入れの記録

- 再実行: 2026-11-03 JST、対象の作業ツリーで監督が実行。照らした文書は仕様書 1.2.7、作業指示書 1.0.16。

| コマンド | 結果 |
|---|---|
| `SITATE_NESTED=1` で `go test -count=1 ./internal/acquire`(製品ビルド、`-tags sitate_testbuild`、`-race`) | `ok` |
| 同じ環境で実物の podman の試験 4 件(隔離、ラベルの掃除、他の実行を巻き込まない、中継) | PASS |
| `test/tools/cov_stats.py --package acquire` | `tests pass=34 fail=0 skip=0`、PASS |
| `check_scope.py --allow internal/acquire/`、`make lint-size` | PASS、終了 0 |

実 root の `source-setup` の試験(`TestSetupScriptWithRealSourceSetup`)は実装担当の実行だけで、監督は再実行していない。

## 変異(監督が実行)

4 通りすべて赤: `--network none`、記録の retain-until を常に足りるとする、延ばせなくても参照する、`end` の後のフレーム。実装担当の記録ではほかに 10 通りが赤。

## レビュー

別の起動のレビューの高 6 件(中継の `stop` が戻らない、改行の無い入力のメモリ、隔離が黙って外れる、延長の失敗の後に古い版を指す、`status: ok` を信じる、許可リストと定義の不一致)ほかを直した。隔離は実行器の側で止めた(`488e0be`)。差し戻しの直しの後の版は別の起動のレビューを受けていない。

## 残したもの

- `contract.Index` に「開けない」を知らせる口が無い。`IndexUnreadable` を返すので、`cli` が見て `Rebuild` を起こす結線が要る。
- 再構成した行の `Size`・`ETag`・`RetainUntil` は空のまま。
- 自己診断の転送・agent・pty の 3 項目は `contract.SSH` に試す口が無く、出していない。
- `IngestResult.Dumps` の形は 4.4 の `{format}` の配列でなく、`cli` の組み立てで合わせる。
- 延長が毎日全チャンクに起きる読み(4.3 の文のとおり)の費用は未測定。設計の監督の回答待ち。
