# mp-prices の受け入れ(2026-11-03。D-0159)

- 範囲: 価格表の変換(`data/prices/`)、`estimate`、`doctor` の `timer_permission`・`adapter_dir_modes` の実測。作業指示書 1.0.18 の 5.15 と 5.17 の追加の完了条件(D-0159)。
- 再実行(Linux 容器): 無印と `sitate_testbuild` で `data/prices`・`internal/cli`・`internal/contract`・`internal/keys` が ok。`-race` で `data/prices`・`internal/cli`・`internal/contract` が ok。`make lint-size LINT_BASE=HEAD` は合格。
- 監督の変異(8 件すべて赤): 費目が無いとき 0、最低料金の行を月額に出す、最後の段の超過を価格ありにする、書き込み側の `all` を外す、B2 の `class_a` の費目を消す、`linger` を見ない、`crontab` の失敗も合格、所有者を見ない。
- 監督が行った契約の変更: `results.json` と `prices.json` のスキーマの `provider`・`plan`・`endpoint_patterns` の字種、`DoctorChecks` と結果の列挙に `adapter_dir_modes` を足す(仕様の表の順で `file_modes` の後)、`internal/keys` の契約試験の一覧 2 ファイル。`check_scope.py` は protected で FAIL を返すが、監督が直したものなので許可とする。
- 観察の答え: 製品の `prices.json` に `compute`・`combos`・`restore_dests` が無いのは、作業指示書 5.15 の「イテレーション 1 は `estimate --endpoint` に要る保管先の行だけ」に合う。作らない。
- 別起動のレビューは未実施(まとめレビューで見る)。
- 未検証: 実機の `loginctl`・`crontab` の出力と `sitate` ユーザーでの実行、実機の `adapter_dir_modes`(主経路の通しで `doctor` を呼ぶときに見る)、署名済みの配布物を経由する `estimate`。
- 設計の監督への疑義(両論): 置き場が無いときの `adapter_dir_modes` を外れにした。`/run/systemd/system` が無い環境は cron と判定する。Storj の「API 料金なし」を規則 6 の無料に数えた。
