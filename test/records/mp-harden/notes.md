# mp-harden の裁定の依頼(仕様の穴・契約の食い違い)

実装担当が推測で埋めなかったもの。監督が設計リポジトリで裁定する。

1. 承認カードの欄名の食い違い: 契約の plan の schema(`internal/contract/schema/data/change.json`)は seal の欄を `host_key_pins`・`weak_items` と書き、
   アプリの規則ファイル(`assets/rules/approval-card-format.md`)とカタログは `host_keys`・`weaker_items` と書く。仕様書 3.1.2.9 の表は欄名を書かない(「ホスト鍵の固定値」「既定より弱い項目」)。
   mp-harden は schema の名前をカード側に揃えた(`host_keys`・`weaker_items`)。仕様書の表に欄名を書くかは裁定。
2. `weaker_items` の判定: 仕様 3.6 は `checks`・`auto_hold_days`・`approval_delay_hours` の既定値との比較を求めるが、`checks`・`auto_hold_days` の既定値が仕様書に表として無く、
   `approval_delay_hours`(既定 24 時間より短い)だけを実装した。
3. `source-change`・`restore-change` のホスト鍵の取得(仕様 3.6「dry-run で核が取得する」): 核に、認証の前の鍵交換だけでホスト鍵を取る継ぎ目(SSH の `HostKey` 取得)が
   `propose` から使える形で結線されていない。この版の plan.host_key_fp は提案の節に書かれた値だけ(省いたときは null で、`source-change` の適用は指紋が無いので拒否する)。
   取得を `propose` に入れるなら、取得用の継ぎ目の追加(contract の変更)が要る。
4. `estimate_after_jpy`・`over_ceiling`(def-change 系): 月額の見積もりを propose から呼ぶ道が無く、null(「月額が出なければ null」)を返す。費用の上限の超過を示す承認(遅延 0)の判定はこの版では働かない。
5. 0 件の世代の健常性: `manifestHealth` は全対象 ok なら 0 件の世代でも `pass` にする。復元テストは 0 件の報告を合格にしなくなった(U1)が、
   取得の側で「0 件の対象」を `incomplete` にするかは仕様に無い。全消去の後の世代が `latest` として選ばれ得る。
6. `--dry-run` を受けるが plan を作れない入口(`init`、`backup`、`hold set`、`restore-test`、`source enroll`、`notify set`)は NOT_IMPLEMENTED で何も動かさない。
   `init-storage` と `restore-dest enroll` は D-0165 で plan を返す。ほかの入口の plan の形は仕様に無い。
   また `--dry-run` の実行も `lockedRun` が `run_start`/`run_end` を journal に書く(実行の記録。状態ファイルや保管先の鍵は変えない)。dry-run は記録を残さないとするか裁定。
7. `restore` の 3.1.5 の規則 1 と核: gate は `ssh://`・`source:`・`restore-dest:` の接頭辞の `--to` を通すが、核はイテレーション 2 まで宛先付きの `--to` を持たない。
   核は絶対パスでない `--to` を `ARG_INVALID`(`--to`、`format`)にした。仕様書 3.1.5 は gate の側の規則だけを書く。核の側の拒否を仕様に書くかは裁定。
8. 状態ディレクトリへ向く絶対パスの `--to`(IT 担当者が直接実行する場合)は、仕様に禁止の規則が無く、核は拒否しない(gate は絶対パスを通さない)。
9. `commitVerify` の保留と状態の照合: 契約試験(`contract_keys_phrase_test.go`)は状態に記録の無い保留を確定できることを求めるため、「記録の無い保留を拒否」は入れず、
   「状態と食い違う保留(確定済みなのに別のキーリング、repo_id の違い)を拒否」にした。記録の無い保留も拒否するなら契約試験の変更が要る。
10. 適用の中の `source enroll`(source-change)が宛先を解けない: 定義の `source.host` は `app01.example.invalid` のようなホスト名(schema の `host`)で、
    登録の `--host ssh://<user>@<host>:<port>` の利用者とポートを持たない。`apply` の `Enroller` は節の `host` をそのまま `parseHostArg` に渡すので `ARG_INVALID` で failed になる
    (第 3 段の台本 `stage3_kinds_test.go` の `kindSourceChange` で再現)。管理用の利用者をどこから得るか(`--source-admin-ref` の秘密に利用者を持たせる、直前の登録の記録、
    `source-change` の JSON に利用者を足す)は仕様に無い。この台本の `source-change` の apply は失敗のまま残す(試験は赤)。
11. 早期に拒否する `restore --snapshot-key-stdin` と標準入力: 核が標準入力を読む前に拒否する(`--to` の形)と、gate 経由で標準入力に中身を送る呼び出しが、
    封筒を返す前に SSH の切断の信号を受けて `RUN_ABORTED`(`reason: signal`)になる競合が第 3 段で起きた(標準入力を送らない呼び出しでは ARG_INVALID が安定)。
    試験は標準入力を送らない形にした。未読の標準入力を抱えたまま早期に失敗する入口の終わり方(読み捨てるか)は仕様に無い。
12. `propose notify-add`・`notify-change` の資格情報(`--secrets-stdin`)を核が秘密ファイルへ置く処理は無かった(試験通知が宛先に届かず、確認語を人が得られない)。
    cli の `proposeEntry` で、作成時に置き、提案を作れなければ消すようにした。仕様 3.1.2.9 の残り — 提案が applied 以外で終わったとき(取り消し・期限切れ)に消す、適用の後に使わなくなった旧い参照名の値を消す — は未実装。
