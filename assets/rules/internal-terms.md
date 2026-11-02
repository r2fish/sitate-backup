# 内部概念の語の一覧と言い換え

管理者に見せる概念は 6 つ(元サーバー、バックアップサーバー、保管先、復元先、定義ファイル、復旧キット)に固定する。下の内部概念の語は、エージェントが管理者に見せる文に出さない。核の JSON と道具の呼び出しには現れてよい。値は引用の印で示す。

## 規則

- 左の語を使わず、右の言い換えで書く。「出さない」の語は、言い換えもせずに、その事柄に触れない。
- 引用の印の中(角括弧 2 つで囲んだ部分)は、この検査の対象から除く。
- 判定は文字列の一致で行う。語は下の `terms` の `ja` と `en` の全部を調べる。

## 一覧(アプリ向け)

```json
{
  "terms": [
    {"id": "snapshot", "ja": ["スナップショット"], "en": ["snapshot"], "rewrite_ja": "何月何日のバックアップ", "rewrite_en": "the backup from <date>"},
    {"id": "chunk", "ja": ["チャンク"], "en": ["chunk"], "rewrite_ja": null, "rewrite_en": null, "say_instead_ja": "保管先のデータ", "say_instead_en": "the data in storage"},
    {"id": "manifest", "ja": ["マニフェスト"], "en": ["manifest"], "rewrite_ja": null, "rewrite_en": null, "say_instead_ja": "保管先のデータ", "say_instead_en": "the data in storage"},
    {"id": "keyring", "ja": ["キーリング"], "en": ["keyring"], "rewrite_ja": null, "rewrite_en": null, "say_instead_ja": "保管先のデータ", "say_instead_en": "the data in storage"},
    {"id": "version_id", "ja": ["版 ID", "バージョン ID"], "en": ["version ID", "version id"], "rewrite_ja": null, "rewrite_en": null, "say_instead_ja": "保管先のデータ", "say_instead_en": "the data in storage"},
    {"id": "snapshot_key", "ja": ["スナップショット鍵", "チャンク鍵"], "en": ["snapshot key", "chunk key"], "rewrite_ja": null, "rewrite_en": null, "say_instead_ja": "保管先のデータ", "say_instead_en": "the data in storage"},
    {"id": "retention_extend", "ja": ["保持延長", "prune", "プルーン"], "en": ["retention extension", "prune", "pruning"], "rewrite_ja": null, "rewrite_en": null, "say_instead_ja": "保存期間の管理", "say_instead_en": "managing how long backups are kept"},
    {"id": "retention_class", "ja": ["日次区分", "月次区分", "保持区分"], "en": ["daily class", "monthly class", "retention class"], "rewrite_ja": "90 日前まで毎日、1 年前まで月ごとに戻せます", "rewrite_en": "You can restore any day within the last 90 days, and any month within the last year"},
    {"id": "isolation", "ja": ["隔離環境", "健常性チェック"], "en": ["isolation environment", "health check", "healthcheck"], "rewrite_ja": "結果だけを述べる(例: 何月何日のバックアップに異常は見つかりませんでした)", "rewrite_en": "state only the result (for example: no problems were found in the backup from <date>)"},
    {"id": "delayed_apply", "ja": ["遅延適用"], "en": ["delayed apply", "delayed application"], "rewrite_ja": "承認から 24 時間後に反映されます", "rewrite_en": "It takes effect 24 hours after approval"},
    {"id": "restore_session", "ja": ["復元セッション"], "en": ["restore session"], "rewrite_ja": "復元先に戻したデータは何月何日何時まで取り出せます", "rewrite_en": "The data restored to the restore server can be taken out until <date and time>"},
    {"id": "adapter", "ja": ["アダプタ"], "en": ["adapter"], "rewrite_ja": null, "rewrite_en": null, "say_instead_ja": "言い換えずに触れない", "say_instead_en": "do not mention"},
    {"id": "language", "ja": ["言語設定"], "en": ["language setting"], "rewrite_ja": null, "rewrite_en": null, "say_instead_ja": "言い換えずに触れない", "say_instead_en": "do not mention"}
  ]
}
```

## 注意

- 「遅延」の時間は固定の 24 時間ではなく、承認カードと状態の値で示す。上の言い換えの 24 時間は例で、実際の時間は引用の印で書く。
- 保持の日数と月数(90 日、12 か月)は固定の決まりとして述べてよい。ただし区分の名前は出さない。
- 語の判定は管理者に見せる文だけに掛ける。手順書と製品の説明書の引用は、引用した節の本文のままでよい(それらの本文もこの語を避けて書いてある)。
