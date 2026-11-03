# 第 3 段: restore-test のバックアップサーバーの核のメモリ(測った値だけ)

問い: `restore-test` のバックアップサーバーの核の VmHWM 約 440 MB の内訳。Argon2id(`Argon2idMemoryKiB` = 262144 KiB)がどれだけ占めるか。

## 測り方

- 第 3 段の環境(`make test-mainpath STAGE=3`、合成データ 約 2.6 MB・56 ファイル)を 3 回作り直して実行した。核の試験ビルドに一時の計測を足した(`Crypto.Argon2id` の前後で `/proc/self/status` の VmHWM・VmRSS と `runtime.MemStats` を記録する。呼び出しの関数の並びを記録する。ヒープが 16 MB 増えるごとに `MemStats` を記録する。ヒープの実割り当てが 250 MB と 330 MB を超えた時点で `runtime/pprof` のヒーププロファイルを書く)。計測は作業ツリーに残していない(`git diff` で `internal/format/` に差が無いことを確認)。
- プロファイルは `go tool pprof -sample_index=alloc_space`(累積の割り当て量)と `inuse_space` で読んだ。プロファイルは直近の GC 完了時点の値で、書いた時点の瞬間値ではない。

## 測った値

- VmHWM(試験が 0.02 秒ごとに読む値の最大。comm=`sitate` のプロセス): `restore-test`(バックアップサーバーの核)は 3 回で 467784・468264・390616 kB。同じ回の復元先の核は 25148・24508・25176 kB。
- **`restore-test` のプロセスは Argon2id を呼んでいない**。3 回とも、計測の記録に Argon2id の呼び出しは 1 回だけで、呼び出し元は `PhraseVerify` → `commitVerify` → `OpenedKeyring` → `openKeyring`(別のプロセス。`restore-test` の VmHWM の標本とは別の pid)。そのプロセスでは Argon2id の前後で VmHWM 16916 → 280364 kB(1 回目)、17256 → 280192 kB(2 回目)、18040 → 281996 kB(3 回目)。mem=262144 KiB、iterations=3、parallelism=4。
- `restore-test` のプロセスのヒープの増え方(3 回目、`HeapAlloc`・VmHWM): 189 MB・264912 kB → 249 MB・326636 kB → 298 MB・391472 kB(終わりまで GC は 20 回前後)。
- `restore-test` のプロセスのヒーププロファイル(3 回目、累積の割り当て 459.25 MB の内訳): `restore.placeScript` 338.17 MB(うち `strings.(*Builder).WriteString` 190.62 MB、`base64.EncodeToString` 115.05 MB、`placeScript` 自身 32.50 MB)、`os.ReadFile` 57.34 MB(うち `DirBundles.Files` 46.44 MB、アダプタの検査 10.39 MB)、jsonschema の検証 30.03 MB。
- `placeScript`(`internal/restore/scripts.go`)は復元先へ置く核とチェック一式の全ファイルを base64 にして台本の文字列に埋める。呼び出しは `rtState.place`(`internal/restore/rtremote.go`)で、`Bundle.Files` が復元先の `<os>-<arch>` の bundle の全ファイルを読む。
- 復元先の arch の bundle のファイルの大きさ(バックアップサーバーの `/usr/libexec/sitate/bundle/0.1.0-dev/linux-x86_64/`。試験ビルド): 核 20955298 B、`provision/b2/b2` 7205026 B、`notify/webhook/webhook` 7151778 B(ほかのファイルを含む読み出し 46.44 MB)。

## 測っていないもの

- `restore-test` の VmHWM 390616〜468264 kB のうち、`placeScript` の寄与を差し引いた値(`placeScript` を除いた実行での VmHWM)。
- 同じ試験のプロセスで Argon2id が走るときの VmHWM(上の別のプロセスが `PhraseVerify` の実行そのもので、その VmHWM の最大は 2 回目の計測で 503188 kB だった)。その内訳(Argon2id の 262144 KiB を除いた約 200 MB)は測っていない。
- 実地のサーバーの大きなデータでの値(A2 で測る)。
