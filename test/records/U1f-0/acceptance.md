# U1f-0 の受け入れの記録

- 再実行: 2026-11-03 JST、対象の作業ツリーで監督が実行。照らした文書は仕様書 1.2.7、作業指示書 1.0.16。

| コマンド | 結果 |
|---|---|
| `go test -count=1 ./internal/adapter`(製品ビルド、`-tags sitate_testbuild`、`-race`) | すべて `ok` |
| `go vet`(両方のタグ)、`gofmt -l` | 出力なし |
| `test/tools/cov_stats.py --package adapter` | PASS |
| `test/tools/check_scope.py --allow internal/adapter/`、`make lint-size` | PASS、終了 0 |

## 変異(監督が実行)

- 赤: 署名の結果を無視、sha256 の不一致を通す、シンボリックリンクを許す、親の終了後にグループを止めない、枠の絞り込みを外す、`SITATE_RECV` を全 kind に渡す。
- 緑: `sha256` が空でも通す(空なら比較が必ず外れるので等価)。

## レビュー

別の起動のレビューの 14 件のうち、高 4 件ほかを差し戻して直した。差し戻しの直しの後の版は別の起動のレビューを受けていない。

## 残したもの

- 署名の合わないアダプタの原因コード(今は `INTERNAL`)、置き場の所有者とモード、孫への `PR_SET_PDEATHSIG` は設計の監督の回答待ち(`test/records/U0-core/handoff-to-design.md` の 12 節)。
- 親が終わった後に孫が読み口を握るときの待ち(2 秒)は測っていない。
- 実物の `Verifier`(U1g)と実物の podman(U1c)との結合は第 1 段で確かめる。
