# 試験用ドライバの受け入れ(2026-11-03)

- 範囲: A2・A6・A7 を動かす実行入口 `app/test/e2e/cli.ts`、試験ビルドだけの LLM の口の差し替え、`make test-app-e2e`、`test/acceptance/launch.md` の空欄。
- 再実行(新しい環境): `make build-test sign-test build-app-test` は終了 0。`make test-app-e2e`: `driver: A2 終了コード 0`、`driver: A6 終了コード 0`。実物の Electron の主プロセス、utilityProcess のエージェント、Tool Runner、核、受入試験の環境を偽の LLM でつなぐ。`npm --prefix app test`: tests 277、pass 277、fail 0。
- 記録に秘密の検査の欄 `secret_check` がある(偽の値で record_hits 0)。担当の変異 5 件のログは監督が再現していない。製品の成果物に試験用の口が無いことは `product-clean.test` が確かめる。
- 未検証(A2 で確かめる): 実物の Anthropic API への認証(SDK 1.3.0 は `Authorization: Bearer` で送る。インストール済みの SDK のコードで監督が確かめた)、実物の LLM の問いの語、実物の B2、変種 add・lo・nocluster、A6 の合否と環境側の注入(F1・N1)。
- 疑義: `propose` は承認まで 1 回の道具の呼び出しで行い、封筒に変更の id が載らない。実物の LLM が存在しない id を `apply` に渡すと止まる可能性がある。A2 で確かめる。
- 別起動のレビューは未実施。
