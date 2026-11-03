# mp-fix の受け入れ(2026-11-03。mp-a2 の版のレビューの指摘)

- F1: `bucket check` の試しで 4xx を拒否と読むのは `retention_shorten` だけ、符号も `AccessDenied`・`InvalidRequest`・`InvalidArgument`・`ObjectLocked`・`ObjectLockedException` に絞った。`legal_hold`・`unversioned_delete` は従来の判定に戻した。監督の変異(版指定なしの削除の 4xx を拒否と読む)は赤。
- F2(秘密のラベル)、F3(予行の記録の退避と要約)、F4(生の記録を ignore)、F6(「始める」の二度押し。`index.ts` の `canStart` は監督がつないだ)、F7(再試行の記録の種類)。
- 再実行: `internal/store`・`keys`・`contract` が ok、`internal/store` は `sitate_testbuild` でも ok。`npm --prefix app test` は fail 0、typecheck は通る。予行の偽の口は担当の記録で 1 回目と環境を作り直した 3 回目が終了 0(2 回目は同じ環境の続けての実行で `SSH_UNREACHABLE`。予行は回ごとに環境を作り直す)。
- 未確認: B2 が保持の短縮を拒むときの実際の符号。予行で観測する。F5(予行専用のバケットの環境変数)は手順書の注意で代える。
