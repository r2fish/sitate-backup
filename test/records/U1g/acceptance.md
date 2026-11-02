# U1g の受け入れの記録

- 再実行: 2026-11-03 JST、対象の作業ツリーで監督が実行。照らした文書は仕様書 1.2.7、作業指示書 1.0.16。

| コマンド | 結果 |
|---|---|
| `go test -count=1 ./internal/keys`(製品ビルド、`-tags sitate_testbuild`) | `ok` |
| `go vet`(両方のタグ)、`gofmt -l` | 出力なし |
| `test/tools/cov_stats.py --package keys` | `tests pass=47 fail=0 skip=3`、PASS。skip は後のイテレーションの 3 件 |
| `test/tools/check_scope.py --allow internal/keys/`、`make lint-size` | PASS、終了 0 |

## 変異(監督が実行)

10 通りすべて赤: 全体署名の検証、trusted comment の照合、本体の署名の検証、HKDF の用途、鍵ファイルのモード、権限の過剰、権限の不足、`--secret-fd` 無しのフレーズの生成、確定で開けなくても進む、部分発行の失効。

## レビュー

別の起動のレビューの致命 2 件(実物の `Codec` で `init` が失敗、実際の配布物で `phrase verify` が確定できない)ほかを直した。試験の側の誤りは監督が直した。差し戻しの直しの後の版は別の起動のレビューを受けていない。

## 記録に残すこと

- 監督の操作ミスで作業中の `skeleton.go` を消し、複製から復元した。実装担当が内容を確かめた。
- `provisionCall` の要求 JSON と `Secrets.Get` の raw は消していない。実物の `AdapterRunner` と `Secrets` の形を結線で確かめて決める。

## 確かめていない範囲

- 実物の `format`、`store`、`state`、MinIO、B2 との結合(第 1 段以降)。
- `keyring_version` の「確定のたびに 1 増える」(増やす側がイテレーション 4 以降)。
- Argon2id の実機の時間。
