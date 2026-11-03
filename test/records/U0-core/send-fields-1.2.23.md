# 送る欄の照合(仕様書 1.2.23、2026-11-04)

対象: `app/src/data/send-fields.json`(開始 commit 04cab1a)と仕様書 3.7 の送る欄の表。

## 実行

- `python3 test/tools/check_send_fields.py --spec <親>/docs/spec.md --send-fields app/src/data/send-fields.json`
  - 結果(逐語): `spec_version=1.2.23` / `rows spec=51 data=51` / `pointers spec=358 data=356` / `path_ref spec=7 data=7` / `diffs=2` / `FAIL`(終了コード 1)
- `python3 test/tools/check_send_fields_schema.py --send-fields app/src/data/send-fields.json --schema-dir internal/contract/schema`
  - 結果: `StopIteration`(道具が落ちる。`deref` が `cli/codes.json` など、`load_defs` が読む 3 ファイル以外へ向く `$ref` を解けない)。照合の結果は得られていない。
  - 参考: 読むファイルを増やした写し(scratchpad、道具そのものは未変更)で実行 → `checked=285` / `missing=1` / `FAIL`。この写しの結果は参考であり、正式な合否ではない。

## 食い違い(直していない)

| # | 種別 | 内容 |
|---|---|---|
| 1 | データの欠け | 仕様書 3.7 の `def validate` は `/result/def_hash`、`/result/pending_refs`。データは `/result/def_hash` だけ(`pending_refs` が無い。D-0160 の追加) |
| 2 | データの欠け | 仕様書 3.7 の `restore-dest enroll` は `/result/plan/errors/<i>/code`、`/result/host_key_fp`。データは `/result/plan/errors/*/code` だけ(`host_key_fp` が無い。D-0159) |
| 3 | スキーマと仕様・データの不一致(写した道具が出した参考結果) | `backup /result/targets/*/next/*/op`: 仕様書 3.7 の `backup` の行は `next/<i>/op` を書き、データもそう書くが、`cli/envelope.json` の `$defs/next` は `argv`・`requires_human`・`requires_approval`・`why` で、`op` を持たない。仕様書の `next/<i>/op` の誤記か、スキーマの欠けかの判断が要る |
| 4 | 道具の不具合 | `check_send_fields_schema.py` が上のとおり例外で落ちる(正式な照合ができない) |

## 注

- 1・2 は A7 の前に直す必要がある(A7 の `fields` は仕様書の表を正とする)。
- 直す側(データは app の担当、道具は test/tools の担当、3 は仕様書側かスキーマ側か)は監督が決める。

## 監督の直し(2026-11-04)

- `app/src/data/send-fields.json` に、仕様書 1.2.23 の 3.7 の表どおり `def validate` の `/result/pending_refs`(D-0160)と `restore-dest enroll` の `/result/host_key_fp`(D-0159)を足した。`check_send_fields.py --spec <仕様書> --send-fields app/src/data/send-fields.json` は `diffs=0`、`PASS`。`npm --prefix app test` は pass 307・fail 0。
- `check_send_fields_schema.py` の `load_defs` が 3 ファイルしか読まず `$ref` を解けなかった(道具の誤り)。スキーマの置き場のファイルをすべて読むように直した。`--schema-dir internal/contract/schema` で `checked=287`、`missing=1`(`backup /result/targets/*/next/*/op`)。これは申し送り 43 節の裁定待ち(`next/<i>/op` は仕様 3.7 の文言どおりに残す。D-0172 の後の設計の指示)。
