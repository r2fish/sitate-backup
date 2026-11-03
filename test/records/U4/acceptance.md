# U4 確定の受け入れ(2026-11-03。D-0168 まで)

- 範囲: `docs/install.md`、受入試験の台本 `test/acceptance/scripts/`、起動の手順 `test/acceptance/launch.md`、手順 13 の判定の直し(`TIMER_NAMES` を外す)、`timers_registered` が全部の単位を確かめる直し(`internal/cli/osprobe.go`。担当の差分を監督が当てた)。
- 再実行(新しい環境): `make build-test sign-test build-app-test` は終了 0。`make test-app-real`: 手順 1〜13 すべて合格(手順 13 は定義の `schedule` と `timers_registered` を見る)。`internal/cli`・`tools/...`・`internal/contract` は無印で ok、`internal/cli` は `sitate_testbuild` でも ok。`npm --prefix app test`: pass 262、fail 0。`make lint-install` は合格。写しの元の sha256 と `docs/install.md` の sha256 は一致(`e939567ade10b532...`)。
- 欠陥検出: `TestTimersRegisteredMissingUnitFails` は監督が差分を当てる前に赤。判定の変異は担当のログだけで、監督は再現していない。
- 未検証: `make test-e2e` の手順 13(`test/e2e/judge.go` が `<定義の schedule>` を扱うよう wp/U1f-2 で直す)、A2・A6 の実行。assets の説明書と規則は変更なし。
- 残る穴: `test/acceptance/launch.md` の `<ドライバの実行入口>` が空欄。台本を読んで走る実行入口が無い。続きの作業で作る。別起動のレビューは未実施。
