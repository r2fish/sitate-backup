# U0-env の受け入れの記録

- 再実行: 2026-11-03 JST、対象の作業ツリーで監督が実行。照らした文書は仕様書 1.2.7、作業指示書 1.0.16。

| コマンド | 結果 |
|---|---|
| `bash test/env/bin/accept-env.sh up acc1` | 起動した。約 1 分 |
| `bash test/env/bin/accept-env.sh check acc1` | `集計: ok=52 FAIL=0`(偽の保管先は未起動で skip) |
| backup の `/etc/environment` から `SITATE_EXTRA_CA_FILE` を消して `check --fast` | `集計: ok=48 FAIL=2`(壊すと赤) |
| `bash test/env/bin/accept-env.sh down acc1` | 停止後にコンテナは残らない |
| `test/tools/check_scope.py --allow test/env/` | PASS |

## 実装担当の実測(監督は再実行していない。緑に数えない)

- systemd 252 の `TimeoutStartSec` は `ExecStart` 1 本ごとに数える。cron の `@reboot` はデーモンの再起動では再実行されない。規則 12 の合計はユニット全体の上限にならない。設計の監督へ渡す。
- `SURVEY_DU_TIMEOUT_S`: 525 MB・8026 ファイルで du は 8 ms(合成データ)。実 VPS の大きさでは未測定。
- MinIO は arm64 のホストの qemu エミュレーションで起動した。ネイティブの x86_64 は未検証。
- 変種 lo、nocluster、add の検査と prepare-storage。

## 確かめていない範囲

- 完了条件 9 の後半(SSH で起動した試験ビルドの核)、`doctor` の timers_registered、backup の rootless podman。第 2 段で確かめる。
- 別の起動のレビューは受けていない(試験の環境で、製品の判定を持たないため、主経路の第 2 段の通しで代える)。
