道具の定義(JSON Schema のデータファイル)の置き場。`AgentRuntime` が `betaTool` で読み込む。
1 ファイル 1 道具、ファイル名は道具名 `sitate_<サブコマンド>.json`(語の区切りは `_`)。中身は WP U3a が書く。

ファイルの形(`app/test/contract/tools.test.ts` が照らす。U0-core が固定):

```json
{ "name": "sitate_<サブコマンド>", "description": "…", "input_schema": { "type": "object", "properties": { … } } }
```

- `name` はファイル名と同じ、`description` は空でない文字列、`input_schema` は `type: "object"` の JSON Schema。
- 引数の型は CLI の許可リストの値の種別から導く: 値の無い印は `boolean`、JSON は `object` か `array`、整数は `integer`、列挙は `enum`、そのほかは `string`。
- `sitate_propose` は平らな 1 つの形に固定する(API へ渡す入力スキーマの最上位に `oneOf`・`anyOf`・`allOf` を置かない): `type: object`、`properties` に `kind`(`enum` は許可リストの種別)と全種別の引数の和集合、`required` は `["kind"]` だけ、`additionalProperties: false`。
  `kind` ごとの引数の組み合わせは、主プロセスが実行の前に検査し、合わなければ実行せず道具の結果でエラーを返す(U3a が実装する。種別ごとの `oneOf` は、この内部の検査用に定義ファイルの `input_schema` の外へ置く)。
  実際の API が受け付けるかは、U3a の最初の起動で確かめる。
- `sitate_attest` の `step` は署名を要する段だけを受ける(`enum`・`pattern`・`oneOf` のどの書き方でもよい。試験が値を実際に通して確かめる)。
- `dry_run`(boolean)は `internal/contract/schema/cli/subcommands.json` の `dry_run` の列が true の道具だけが持ち、false の道具は定義に持たない(核が `--dry-run` を `ARG_INVALID` で拒否するため)。`bootstrap`・`bootstrap-restore` は列が無く、見ない。`sitate_propose` は true なので持つ(`required` は `kind` だけのまま)。ただし仕様 8.2 でイテレーション 3 へ送った 9 サブコマンド(`backup`・`hold set`・`notify set`・`extend`・`prune`・`init`・`source enroll`・`restore`・`restore-test`)は、列が true でも道具に `dry_run` を出さない(D-0183。列は変えず、`app/test/contract/tools.test.ts` の `DRY_RUN_DEFERRED` が例外の一覧)。
- `kind` と引数の組み合わせの検査は `src/main/tool-kind-args.ts` の `checkKindArgs`、標準出力の 2 レコードは `src/main/stdout-records.ts` の `parseStdoutRecords`(どちらも骨格。U3a が実装)。

骨格では空で、`app/test/contract/tools.test.ts` が「未実装」で失敗する。
