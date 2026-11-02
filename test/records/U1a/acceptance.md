# U1a の受け入れの記録

- 再実行: 2026-11-02 JST、対象の作業ツリーで監督が実行。開始 commit は `d844b93`。照らした文書は仕様書 1.2.6、作業指示書 1.0.15。

| コマンド | 結果 |
|---|---|
| `make shell-linux CMD='go test -count=1 ./internal/format/'` | `ok`。`-cover` は 89.2% |
| `go vet ./internal/format/`、`gofmt -l internal/format`、`go build ./...` | 出力なし |
| `test/tools/cov_stats.py --package format` | `tests pass=15 fail=0 skip=0`、PASS |
| `test/tools/check_scope.py --base d844b93 --allow internal/format/` | PASS |
| `make lint-size LINT_BASE=d844b93` | 終了 0 |

## 変異

監督が 15 通りを実行。赤 14、緑 0、変異がビルドに失敗 1。

- 赤: AAD を見ない、HKDF の info を捨てる、Argon2id の引数の入れ替え、`VerifyProof` が常に真、チャンクの最小の大きさ、`Seal` の上限、展開の上限、見出しの印を AAD から外す、種類を AAD から外す、`Hash` の接頭辞、ID の小文字の検査、SITK の見出しの ID、版の検査、SITM の圧縮、`Encode` の種類の不一致。
- ビルド失敗: 包んだ EOF を成功に戻す変異(`errors` の import が無い)。`TestChunkWrappedEOFIsError` が修正前に落ちた記録で代えた。

## レビューと裁定

別の起動のレビューの指摘のうち、実装の欠陥(包んだ EOF を成功にする、ID の字面の揺れ、SITK の見出しの ID、短いオブジェクトの版の判定、無圧縮の上限)は直した。

- マニフェストの圧縮は `Encode`・`Decode` が持つと監督が裁定した(`d844b93`)。展開の上限 `ChunkMax` は仮の流用。
- 直した後の版は別の起動のレビューを受けていない。

## 確かめていない範囲(緑に数えない)

- Argon2id の見出しのパラメータの上限(回答待ち。`test/records/U0-core/handoff-to-design.md` の 8 節)。
- 256 MiB の Argon2id と 16 MiB のチャンクの実機の性能とメモリ。
- U1c、U1e、U1g の実物との結合(主経路の第 1 段で確かめる)。
- 試験ビルドでの試験(`internal/format` に試験ビルドの分岐は無い)。
