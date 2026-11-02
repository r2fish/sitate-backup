# U5 の受け入れの記録

- 再実行: 2026-11-03 JST、監督が最終の作業ツリーで実行。照らした文書は仕様書 1.2.7(7.2)。

| コマンド | 結果 |
|---|---|
| `go test -count=1 -race ./data/prices/... ./internal/contract/` | 両方 `ok` |
| `go run ./data/prices/gen` | `generated_at` を除き `prices.json` と一致 |
| `check_scope.py --allow data/prices/` | `PASS`。`src/` の差分なし |
| `make lint-size` | 合格 |

- 別の起動のレビュー: 全 17 行・88 費目を入力と照合して誤りなし。
- 変異(監督が実行):
  - RED 28 件

## 確かめていないこと

- 公式ページとの価格の照合(入力の YAML を正とした)、実物の endpoint への接続、`make sign-test` と核の検証器の結合、`estimate` との結合。

## 残したもの

- `tier_upto` の読み、`all` の語、R2 の無料枠、基本料金、(d) の例外は設計の監督の判断待ち(`test/records/U0-core/handoff-to-design.md` の 22、26 節)。
