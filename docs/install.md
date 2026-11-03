# 導入手順書

この手順書は、人とエージェントが同じ文面で使う。各手順は「目的 / 実行するもの / 完了を確認するコマンドと期待する値 / 失敗時の原因コードごとの次の手順」の形で、表の 1 行に書く。期待する値は「JSON Pointer = 値」で、エージェントは人に聞かずに完了を判定できる。`<i>` は配列の全要素を表す。

印の値の定義(署名のときに埋まる): `{{DIST_KEY_FINGERPRINT}}` は配布用の鍵の指紋、`{{WARNING_ALLOWLIST}}` は手順 13 で許す警告の原因コードの一覧(初期値は空)。

付録 E3(元サーバーの侵害からの回復)など、この版の手順書に無い付録は、イテレーション 2 以降で足す。参照先が無いときは、先へ進まずに管理者へ知らせる。

## 目次

- 開始前に用意するもの
- 省けない手順 3 つ
- 確認項目
- 人への問い Q1〜Q4 と人の作業 H1〜H5
- 手順 1〜13
- 手順のやり直し
- 付録「事業者別(組 A)」

## 開始前に用意するもの

次は導入の開始時の入力で、人への問い(Q1〜Q4)にも人の作業(H1〜H4)にも数えない。

- 空のバックアップサーバー(OS 導入済み)と、管理者端末からそこへの管理アクセス(SSH)。手順 1 の `bootstrap` が最後に閉じる。
- 元サーバーの所在(ホスト名かアドレス。SSH が 22 番で受けること)と、元サーバーへの管理アクセス。管理アカウントは root か、パスワードを問わない sudo を持つアカウント。端末に設定済みの `ssh` の接続や ssh-agent はアプリが使わないため、所在と管理アカウントの名前は導入の指示に書いて与える。その管理アカウントのパスフレーズの無い OpenSSH 形式の秘密鍵は、アプリが開く秘密の入力ボックス(保存するモード)に入れる。
- 通知の受け口: JSON を受けて人が表示できる受信 Webhook の URL を事前に作っておく(Q1 で入れる)。
- 署名を検証したクライアントアプリ。
- LLM API の契約とキー: Anthropic Console で発行する API キー。1 つのワークスペースに紐づけ、有効期限は無期限にして、差し替えで管理する。サブスクリプションの転用は使わない。アプリが最初の起動で開く秘密の入力ボックス(アプリ自身が開くモード)に入れる。第一者の API では処理地域を日本に固定できない(確認項目に書く)。キーの差し替えは、盗まれたとき・無効になったとき・全損の後の新しい端末での再入力の 3 つの場合に行う。差し替えの手順は付録「LLM API キーの差し替え」(イテレーション 2 で書く)に従う。
- バックアップサーバーの事業者とプラン名(分かれば。手順 4 で月額の見積りに使う)。

保管先と LLM API の契約は、保管の国と処理地域を選んだうえで結ぶ。業務ソフト別の適合は製品が判定せず、対象にできない場所は `sitate source check` が示す。

## 省けない手順 3 つ

次の 3 つは省かない。

1. リカバリーフレーズの 2 か所保管(人の作業 H2、人への問い Q2)。
2. 復元先サーバーの確保(人の作業 H3、人への問い Q3)。
3. 復旧キットの保管場所の確認(フレーズのパネルでの `kit confirm`。Q2 に含める)。

## 確認項目

導入の前か途中に、人が次を確かめる。エージェントは該当する場面で管理者に確かめ、確かめた結果を記録する。

- 保管先の公正利用条件・最低保存期間・費用上限に届いたときの挙動を読んだ。
- 保管先が、元サーバーと別の事業者・別のアカウント・別の地域である。
- 保管先のアカウントに多要素認証を掛けた。
- バックアップサーバーをディレクトリ(社内の認証の仕組み)に参加させない。
- 委託先の管理ツールをバックアップサーバーに入れない。
- OS の自動更新を有効にした。
- バックアップサーバーのディスク暗号化を、選べる事業者では有効にした(`sitate doctor` では警告で、不合格にしない)。
- LLM API の契約条件の確認: 入力を学習に使わないこと、保持が 30 日であること、処理地域を確かめた。第一者の API では処理を日本に固定できないことを知ったうえで契約する。
- 業務アプリの接続情報(DB の接続設定)の置き場を、ファイルの対象に含めたか。含めないと、本番への戻しの手順で使えない。
- 本番復元(DB の読み込みと業務の再開)を頼む依頼先を決め、手順書に記したか。製品は依頼先を持たない。
- 開始前に用意した管理アクセスが閉じたか。手順 1 の `bootstrap` が最後に閉じる。結果の `/result/admin_access_closed` が `false` のときは、人が事業者のコンソールで閉じる。以後の root の作業は事業者のコンソールから行う。
- 事業者アカウント(保管先、バックアップサーバー、元サーバー、復元先)のログイン情報と多要素認証の端末を、誰が持ちどこに保管するかを決めた。
- 保管先・バックアップサーバー・LLM API の事業者側で、容量と請求額の閾値のアラートを管理者の連絡先に設定し、設定できるときは支払いの上限も設けた。保管先とバックアップサーバーの閾値は `sitate estimate` の月額を基に決める。LLM API の利用料は見積りに含まれないので、閾値と上限は人が決める。
- 保管先の事業者アカウントの請求の状態(支払い方法が有効で未払いが無いこと、請求と停止の知らせが管理者の連絡先に届くこと)を確かめた。
- バックアップサーバーのスワップを、選べる環境では無効にした(`sitate doctor` の `swap_disabled` は参考の項目で、合否に使わない)。

## 人への問い Q1〜Q4 と人の作業 H1〜H5

人への問いは、エージェントが人の答えを求める回数で数える。回数は目安(想定 4 回)で、上限ではない。指標の本旨は、分からないまま答えを求める問いを 0 にすること。エージェントは各問いに、何のために聞くかの説明と推奨の案を添え、分からなければ推奨の案でよいと伝える。問いは疑問符で終え、1 回の発話に問いは 1 つだけにし、問いを発話の最後の文にしてその後に文を続けない(説明と推奨の案は問いの前に置く)。手順の順を守り、Q3 は手順 3、Q4 は手順 4 の前半で聞く(順を飛ばして先に聞かない)。開始時の入力が揃う前に道具を呼ばない。

### Q1 保管先の契約情報と通知先(手順 2)

- 聞くこと: 保管先のエンドポイントとバケット名、事業者の管理用鍵、通知の受け口の URL。
- 何のために聞くか: バックアップの送り先と、失敗したときの知らせ先を決めるため。
- 推奨の案: 保管先は H1 で契約した事業者の値をそのまま使う。通知先は、普段見る場所にある受信 Webhook を 1 つ。
- 入れ方: 事業者の管理用鍵は、呼び出し(手順 2 の確認と手順 5 の `init-storage`)のたびに秘密の入力ボックスの保存しないモードで入れる(1 行目にキー ID、2 行目に鍵)。キーチェーンには置かず、その呼び出しの間だけ使う。通知の受け口の URL は保存するモードで入れる(手順 4 の `notify set`)。保管先準備アダプタの無い事業者では、書き込み鍵と読み取り鍵を入れる。
- 通知の受け口の要件: JSON を受けて人が表示できること。確認語は通知の本文に載る。
- 管理用鍵はその都度入れるので、入れ直しは新しい問いには数えない(Q1 の続き)。

### Q2 フレーズを保管したことの確認(手順 7)

- 聞くこと: リカバリーフレーズを 2 か所に保管したこと(秘密の入力ボックスでの再入力)と、復旧キットの保管場所。
- 何のために聞くか: バックアップサーバーを全部失ったときの唯一の手掛かりだから。保管を確かめるまで次へ進まない。
- 推奨の案: 印刷か手書きで 2 部作り、別々の場所(例: 金庫と自宅)に置く。

### Q3 復元先の所在(手順 3)

- 聞くこと: 復元先の到達先(ホスト)と、初期の資格情報。資格情報は秘密の入力ボックスに入れる。
- 何のために聞くか: 毎月の復元テストを、別のサーバーで本当に試すため。
- 推奨の案: 時間課金で借りられる小さなサーバーを 1 台、本番と別のネットワークに用意する。

### Q4 何を守りたいか(手順 4 の前半)

- 順序: 手順 3 の後、定義を組み立てる前に行う。エージェントは `sitate source survey` で元サーバーを調べ、結果から対象の案を作り、`sitate estimate` で月額の目安を出してから問う。
- 説明: 取れていない場所は被災のときに戻せない。調査で見つけた場所は、実名ではなく種別と大きさで示される(実名は状態の表示で人が見る)。v1 で取れないもの(対象外のディレクトリ、対象外のマウント、ラージオブジェクト、DB の生ファイルの場所)は、ファイルの対象にできず、DB は論理ダンプで取る。取れるマウントと DB は、人が断らなければ対象に入る。断ったものは定義の `source.declined` に人の判断として残り、週次の通知に毎回出る。調べていない置き場はチャットで伝えれば対象にできる。保持は日次 90 日・月次 12 か月で固定。大きさの調査が不完全なときは、大きさと月額が小さめに出る。月額は初回の取得の前の目安で、バックアップサーバーと復元先の費用は含まない。
- 推奨の案: 人の言葉(例「会計ソフトのデータと介護記録」)に当たる候補を選んだ案。分からないと答えたときは、調査で見つけた取れる候補をすべて含む案を、月額とともに示す。DB の生ファイルの場所は根にせず、その下の DB でない直下のディレクトリを根にする。
- 確かめ: 人が案を受ければ手順 4 の後半へ進む。違うと言われれば案を直して示し直す(同じ Q4 の続き)。断った候補は定義の `source.declined` に書く。取れるマウントは `mount: <マウントの根>`、DB はクラスタの単位で `postgresql_cluster: <ポート>` と書く。調査で外した場所とその理由は、黙らずに人へ示す。

### H1 保管先の事業者との契約と支払い(手順 2 の前)

事業者の管理用鍵の発行を含む。保管先準備アダプタの無い事業者では、付録「事業者別(組 A)」に従ってバケットを用意し、書き込み鍵と読み取り鍵を発行する。

### H2 リカバリーフレーズの保管(手順 7)

印刷または手書きで 2 部、2 か所に保管する。

### H3 復元先サーバーの確保(手順 3 の前)

復元先にするサーバーを借りる。`ssh-host` の復元先は付録「事業者別(組 A)」に前提を列挙する。

### H4 危険な変更の承認(手順 10)

導入時は `seal` の承認カード。手順 4 の試験通知で宛先ごとに受け取った確認語を入力する。

### H5 元サーバーに道具が無いときだけ(手順 8 の前)

`sitate source survey` の結果で足りない道具が示されたときだけ、人が元サーバーの OS のパッケージ管理でそれを入れる。エージェントは入れる命令を示して説明する。起きた回は人の作業が 5 個になることを人に伝える。人の作業は通常 H1〜H4 の 4 個に収まる。

## 手順 1〜13

| 手順 | 目的 | 実行するもの | 完了確認コマンド | 期待する値 | 失敗時 |
|---|---|---|---|---|---|
| 1 | バックアップサーバーに道具を置き、配布物の署名を確かめ、管理アクセスを閉じる | `bootstrap --admin-ref bak-admin`(接続先と管理アカウント `admin_user` は導入の指示のとおりに渡す。バイナリの配置、gate の登録、専用ユーザー・容器実行環境・タイマー登録の権限と鍵のメモリ固定の上限(`memlock`)の用意、最後に管理アクセスを閉じる) | `sitate version` と `sitate doctor` | 1 つ目: `/ok` = `true`、`/result/signing_key_fp` = `{{DIST_KEY_FINGERPRINT}}`。2 つ目: `/result/passed` = `true`(必須の項目 `admin_access_closed` を含む。参考の項目は合否に使わない) | `SSH_UNREACHABLE` → 接続先と 22 番の到達を確かめる。`SSH_AUTH_DENIED` → 管理アカウントと鍵を確かめる。`SSH_HOSTKEY_MISMATCH` → 接続先を確かめ、人が接続先を削除してやり直す。`BOOTSTRAP_PRIVILEGE_REQUIRED` → root か sudo を持つアカウントで行う。`SIGNATURE_INVALID` → 配布物を取り直す。止めて管理者へ知らせる |
| 2 | 保管先の契約情報を入れ、保管先の設定を実行前に確かめる(定義が無い状態で、引数だけで動く。何も作らない) | Q1、`sitate init-storage --dry-run --endpoint ... --bucket ... --prefix sitate/ --admin-ref admin` | `sitate init-storage --dry-run --endpoint ... --bucket ... --prefix sitate/ --admin-ref admin` | `/result/plan/errors` = `[]`(管理用鍵の拒否と前提の欠けはここに載る) | `STORAGE_UNREACHABLE` → エンドポイントと到達を確かめる。`STORAGE_DENIED` → 鍵の権限と契約の状態を確かめる。`BUCKET_MISMATCH` → 既存のバケットの設定を人と確かめる。`SECRET_MISSING` → Q1 の鍵を入れ直す |
| 3 | 復元先の資格情報を入れ、復元先の登録を実行前に確かめる | Q3、`sitate restore-dest enroll --dry-run --dest ... --adapter ... --admin-ref dest-admin` | `sitate restore-dest enroll --dry-run --dest ... --adapter ... --admin-ref dest-admin` | `/result/plan/errors` = `[]`(定義が無い状態で、引数だけで動く。何も作らない) | `RESTORE_HOST_DENIED` → 復元先の資格情報と権限を確かめる。`RESTORE_HOSTKEY_MISMATCH` → 復元先のホスト鍵を人と確かめる。`SECRET_MISSING` → Q3 の資格情報を入れ直す |
| 4 | 守る対象を決めて定義を作り、通知先を登録して試験通知で確かめる | Q4、`sitate source survey --host ssh://<user>@<host> --admin-ref source-admin`、`sitate estimate --endpoint ... --size-gb ... --daily-gb ... --dump-gb ...`、先に `sitate notify set --to <通知先> --secret-ref <通知先と同じ参照名>`(Webhook の URL は秘密の入力ボックスの保存するモード。定義の `notify.to` の参照名をここで解決するので、`def validate` より前に呼ぶ)、続けて定義を組み立てて `sitate def validate --file -` に渡す、`sitate notify test` | `sitate def validate --file -` と `sitate notify test` | 1 つ目: `/ok` = `true`。2 つ目: `/result/all_delivered` = `true`(試験の通知には宛先ごとに別の確認語が載る。人には、受け取った確認語を手順 10 の承認カードで宛先ごとに入力することを伝える。問いには数えない) | `SOURCE_SURVEY_FAILED` → 元サーバーの所在と管理アカウントを確かめる。`SOURCE_HOSTKEY_MISMATCH` → 元サーバーのホスト鍵を人と確かめる。`DEF_INVALID` → `pointer` と `reason` に従って定義を直して出し直す(`seal` の前は、保管先の鍵の参照名 `storage[0].key_ref` と復元先の `restore.dest.key_ref` が未解決でも止まらず、結果の `pending_refs[]` に載る。手順 5・9 で解決する。`unresolved_ref` で通知先の参照名が挙がるときは、先に `notify set` を呼んだか確かめる)。`NOTIFY_UNDELIVERED` → 通知先を確かめ、`notify set` と `notify test` だけやり直す |
| 5 | 保管先を作成し、消せない設定と鍵の権限を確かめる | `sitate init-storage --endpoint ... --bucket ... --prefix sitate/ --admin-ref admin`(保管先準備アダプタの無い事業者は付録「事業者別(組 A)」。事業者の管理用鍵は保存しないモードで入れ、キーチェーンに残らない) | `sitate bucket check` と `sitate doctor` | 1 つ目: `/result/lock_mode` = `COMPLIANCE`、`/result/versioning` = `true`、`/result/permissions/<i>/actual` = `/result/permissions/<i>/expected`(書き込み鍵の版指定なしの削除の要素を除く)。2 つ目: `/result/passed` = `true` | `BUCKET_MISMATCH` → 設定の食い違いを人と直す(管理用鍵を入れ直す)。`KEY_TOO_POWERFUL` → 鍵の権限を絞って発行し直す。`KEY_MISSING_PERMISSION` → 鍵に足りない権限を足す。`ALREADY_INITIALIZED` → 済みなので完了確認へ進む |
| 6 | バックアップの暗号化の仕組みと承認鍵を初期化する | `sitate init`(道具 `sitate_init`。承認鍵の公開鍵と秘密の出力先 `--secret-fd` はアプリが付けるので、エージェントは渡さない。失敗として扱った出力のときは、手順 7 の前なら同じ `init` を再実行する) | `sitate status` | `/result/initialized` = `true` | `ALREADY_INITIALIZED` → 手順 7 の確認に進む。`SECRET_FD_REQUIRED` → 秘密の渡し方を確かめる。`KEYRING_MISMATCH` → 保管先の既存の内容を人と確かめる |
| 7 | フレーズを人が保管したことと復旧キットの保管を確かめる | Q2、H2、面: `sitate phrase verify`、`sitate kit`、面: `sitate kit confirm` | `sitate status` | `/result/phrase_confirmed` = `true`、`/result/kit/confirmed` = `true` | `PHRASE_INVALID` → 入れ直してもらう。`PHRASE_UNCONFIRMED` → 保管を確かめるまで次へ進まない |
| 8 | 元サーバーに受け口を設置し、対象の取得を自己検査する | `sitate source enroll --admin-ref source-admin`(元サーバーに道具が無いときは H5) | `sitate source check` | `/result/selftest/<i>/ok` = `true`、`/result/actionable` = `[]`(足せる候補が無い。知らせるだけの種別は完了の条件から除く) | `SOURCE_SETUP_FAILED` → 道具が無い(`tool_missing`)なら H5 を行う。`SOURCE_HOSTKEY_MISMATCH` → 元サーバーのホスト鍵を人と確かめる。`RECEIVER_SELFTEST_FAILED` → 結果の `next` に従う。`RECEIVER_TARGETS_MISMATCH` → 対象と受け口の設定を合わせる |
| 9 | 復元先を登録し、隔離の設定を確かめる | `sitate restore-dest enroll --admin-ref dest-admin` | `sitate restore-dest check` | `/result/isolation/<i>/ok` = `true` | `RESTORE_ENROLL_MISMATCH` → 登録の内容を確かめる。`RESTORE_ISOLATION_FAILED` → 復元先の隔離を直す。`RESTORE_HOST_DENIED` → 復元先の権限を確かめる |
| 10 | 定義を承認して確定する | `sitate propose seal --file -`(定義を標準入力で渡す) → H4(承認カード。手順 4 の確認語を宛先ごとに入力)→ `sitate apply` | `sitate def show` と `sitate status` | 1 つ目: `/result/sealed` = `true`。2 つ目: `/result/notify/proof_confirmed` = `true` | `NOTIFY_PROOF_MISMATCH` → 通知先を確かめ、手順 4 の `notify set` と `notify test` だけやり直してこの手順を出し直す。`APPROVAL_REQUIRED` → 承認カードで承認する。`DELAY_NOT_ELAPSED` → 遅延の時刻まで待つ。`PRECONDITION_CHANGED` → 提案し直す。`DEF_INVALID`(`unresolved_ref`。参照名が未解決、または渡した定義が最後に `def validate` を通した内容と違う)→ 手順 5・9 が済んでいるか確かめ、`def validate --file -` に通した内容をそのまま渡して出し直す |
| 11 | 最初のバックアップを取る | `sitate backup` | `sitate status` | `/result/last_backup/ok` = `true`(読み取り中に変化したファイルは警告で、成功を妨げない) | `ALL_TARGETS_FAILED` → 結果の `next` に従い対象を確かめる。`TARGET_EMPTY`(警告。取得した対象のエントリが 0 件で、世代は不完全になり通知が出る)→ 元サーバーの対象のデータを確かめ、正しく空なら対象から外す提案(`source-change`)を出す。`STORAGE_UNREACHABLE` → 保管先への到達を確かめる。`NOT_SEALED` → 手順 10 を済ませる。`LOCKED` → 実行中の処理の終わりを待つ |
| 12 | 別のサーバーへの復元テストで、戻せることを確かめる | `sitate restore-test` | `sitate status` | `/result/last_restore_test/ok` = `true` | `RESTORE_MISMATCH` → 復元した内容の差を結果で確かめる。`NO_HEALTHY_BACKUP` → 手順 11 をやり直す。`RESTORE_ISOLATION_FAILED` → 手順 9 を確かめる。`TEARDOWN_UNVERIFIED` → 復元先の後始末を確かめる |
| 13 | 定期実行が登録され、警告が許容の外に無いことを確かめる | 確認だけ | `sitate status` と `sitate doctor` | 1 つ目: `/result/schedule` = `<定義の schedule>`(手順 4 で組み立てた定義の `schedule` の値。定義に書かなければ既定の `02:30`)、`/warnings/<i>/code` ⊆ `{{WARNING_ALLOWLIST}}`。2 つ目: `/result/stage` = `sealed`、`/result/passed` = `true`(段階 `sealed` の `timers_registered` が、定期実行の単位がすべて登録されていることを確かめる) | `NOT_SEALED` → 手順 10 を済ませる。`TIME_SKEW` → サーバーの時刻を合わせる。`doctor` の `timers_registered` が不合格 → その項目の `next` に従う |

保管先準備アダプタの無い保管先(付録「事業者別(組 A)」の事業者と、試験環境の S3 互換の保管先)の手順 2 と 5:

- バケットと書き込み鍵・読み取り鍵は H1 で付録の手順により用意する。
- 手順 2 は、書き込み鍵と読み取り鍵を秘密の入力ボックスに入れ、完了確認を `sitate bucket check` の `/ok` = `true` とする。`init-storage` は使わない。
- 手順 5 は `init-storage` を実行せず、完了確認は上の表の手順 5 と同じ。

## 参照名と定義の形

### 参照名(固定)

キーチェーンの秘密は参照名で指す。参照名はアプリが入力ボックスで受けた値の置き場の名前で、**別の資格情報に同じ名前を使わない**(取り違えると、元サーバーの鍵を保管先の管理用鍵として送ってしまう)。次の名前をそのまま使う。

| 参照名 | 何の値か | 使う呼び出し |
|---|---|---|
| `bak-admin` | バックアップサーバーの管理アクセスの鍵 | 手順 1 の `bootstrap --admin-ref bak-admin` |
| `source-admin` | 元サーバーの管理用アカウントの秘密鍵 | 手順 4 の `source survey --admin-ref source-admin`、手順 8 の `source enroll --admin-ref source-admin` |
| `admin` | 保管先の事業者の管理用鍵(保存しない。キー ID と鍵の 2 行) | 手順 2・5 の `init-storage --admin-ref admin` |
| `dest-admin` | 復元先の初期の資格情報 | 手順 3 の `restore-dest enroll --dry-run --admin-ref dest-admin`、手順 9 の `restore-dest enroll --admin-ref dest-admin` |
| 通知先と同じ名前(例 `hook`) | 通知先の Webhook の URL | 手順 4 の `notify set --to hook --secret-ref hook` |
| `storage` | 保管先の書き込み鍵・読み取り鍵(定義の `storage[0].key_ref`。`init-storage` が発行して秘密ファイルに置く) | 定義だけに書く |
| `restore-init` | 復元先の初期の資格情報の参照名(定義の `restore.dest.key_ref`) | 定義だけに書く |

秘密の値は道具の引数にもチャットにも書かない。引数に参照名を渡せば、アプリが入力ボックスを開いて人に値を求める(`secret_refs` を別に付ける必要は無い)。

### 定義の形(手順 4 で組み立てる)

Q1〜Q4 と手順 2・3 の値から、次の形の JSON を作って `sitate_def_validate` と `sitate_propose`(`seal`)の `definition` に渡す。キーの名前は仕様どおりで、勝手に増やさない。

```json
{
  "version": 1,
  "source": {
    "host": "<元サーバーの所在>",
    "targets": [
      { "id": "t1", "files": "/srv/app/data" },
      { "id": "t2", "postgresql": "appdb", "port": 5432 }
    ],
    "declined": [
      { "id": "d1", "mount": "/mnt/archive" },
      { "id": "d2", "postgresql_cluster": 5433 }
    ]
  },
  "storage": [
    { "endpoint": "<Q1 のエンドポイント>", "bucket": "<Q1 のバケット>", "key_ref": "storage", "prefix": "sitate/" }
  ],
  "restore": {
    "adapter": "ssh-host",
    "dest": { "at": "ssh://<ユーザー>@<復元先のホスト>:22", "key_ref": "restore-init" }
  },
  "notify": { "to": ["hook"] },
  "server_monthly_yen": 0
}
```

- 対象 `targets` の `id` は `t1`、`t2` の形(`t` と 1〜9999)。種別のキーは `files` か `postgresql` のどちらか 1 つ。除外 `declined` の `id` は `d1`、`d2` の形で、種別は `mount` か `postgresql_cluster`。
- `files` の根は絶対パスで、`/`、`/etc`、`/var`、`/home`、`/srv`、`/opt` そのものと、`/proc` などのシステムの場所は置けない(`DEF_INVALID` の `root_forbidden`)。
- `notify.to` は通知先の参照名(手順 4 の `notify set` で先に登録した名前)。メールアドレスの通知先はイテレーション 1 の範囲の外。
- `server_monthly_yen` は価格表に当たるプランが分からなければ書かない(0 のまま)。
- `DEF_INVALID` が返ったら、`detail` の `pointer` と `reason` の場所だけを直して出し直す。

## 手順のやり直し

- 中断・失敗した導入をやり直すときは、まず `sitate bucket check`(書き込み鍵)、`sitate status`、`sitate def show` で現状を確かめ、完了確認に合格している手順は飛ばして差分だけを行う。
- 手順 2 の完了は `sitate bucket check` で判定する。合格していれば手順 2 は済み。事業者の管理用鍵はキーチェーンに置かないので、`init-storage`(dry-run を含む)を管理用鍵の参照なしで実行し、`BUCKET_MISMATCH` が返ったときか書き込み鍵が無いときだけ参照 `admin` を付けて呼び直す。アプリはそのとき保存しないモードの入力ボックスを開く。
- 手順 5 の `init-storage` は、書き込み鍵で設定の一致を確かめられれば、管理用鍵なしで `ALREADY_INITIALIZED`(警告。成功)を返す。
- 設定が食い違うときだけ、人が事業者の管理用鍵を保存しないモードで入れて直す。これは警告への対応として扱い、人への問いには数えない。
- 手順 6 の `init` は、手順 7 の `phrase verify` で確定した後は `ALREADY_INITIALIZED` を返す。そのときはやり直さず、手順 7 の完了確認へ進む。
- 手順 8 で `actionable` に要素が残るとき: 新しい候補のうち、マウントと DB は人に問わずその候補を定義の `source.targets` に加える。DB のクラスタ(手順 4 の調査より後にできたもの)は、DB 名を得るため先に `sitate source survey` を再実行する(元サーバーには書かず、冪等)。人が断れば `source.declined` に書く。上の親マウントの下の直下の候補は Q4 の続きとして示し、足すか、断って `source.declined` に書く。続けて `sitate def validate --file -` の `/ok` = `true` を確かめ、`sitate source enroll` を再実行(冪等)してから、手順 8 の完了確認をやり直す。手順 5〜7 はやり直さない。取らないと分かった要素(大きなオブジェクト、対象外のマウント、対象外の DB)は、取らないことを人への報告に含める。

## 付録「事業者別(組 A)」

この版の事業者別付録は、保管先の B2 と、復元先の `ssh-host` の分だけを書く。EC2 の IAM ポリシーなど、ほかの事業者は後の版で足す。人が事業者の画面で発行する鍵には、名前に付録が指定する接頭辞 `sitate-` を付けさせる。

### B2 のバケットと鍵(H1)

保管先準備アダプタのある B2 では、手順 2 と 5 を `init-storage` で行う。この付録の手順は、アダプタを使わない場合と、アプリも配布元も無い全損のときに使う。

| 手順 | 目的 | 実行するもの | 完了確認コマンド | 期待する値 | 失敗時 |
|---|---|---|---|---|---|
| B1 | 消せない設定のバケットを作り、書き込み鍵と読み取り鍵を発行する | 外: 事業者の画面でバケットの作成と Object Lock の有効化と鍵 2 本の発行を行う(鍵の名前は sitate- で始める)、H1 | `sitate bucket check` | `/ok` = `true` | `BUCKET_NOT_OWNED` → バケットの所有者を確かめる。`KEY_MISSING_PERMISSION` → 鍵に足りない権限を足す。`KEY_TOO_POWERFUL` → 鍵の権限を絞る |
| B2 | 鍵をアプリに入れて、設定と権限を確かめる | 面: `sitate secrets set`、Q1 | `sitate bucket check` | `/result/lock_mode` = `COMPLIANCE`、`/result/versioning` = `true` | `BUCKET_MISMATCH` → 設定の食い違いを直す。`STORAGE_DENIED` → 鍵の権限と契約の状態を確かめる |
| B3 | 全損のときに、キットの範囲だけの読み取り鍵をコンソールで発行する | 外: 事業者のコンソールで読み取り鍵を発行する(範囲はキットのバケットと接頭辞だけ。名前は製品の印)、面: `sitate secrets set` | `sitate bucket check` | `/ok` = `true` | `KEY_MISSING_PERMISSION` → 読み取りの権限を足す。`STORAGE_DENIED` → 鍵の範囲と有効を確かめる |

### `ssh-host` の復元先(H3)

復元先を `ssh-host` にするときの前提を、手順 3 の前に満たす。

- OS は Linux(x86_64 または arm64)。
- ホスト鍵: 復元先のホスト鍵の指紋(Ed25519、`SHA256:` の書式)を事業者の画面などの別の経路で確かめられるときは、手順 3・9 の `restore-dest enroll` に `--host-key-fp` で付ける(`seal` の前だけ)。付けなければ初回の接続で固定する。
- 初期の資格情報(Q3)は、sudo を持つ管理用アカウントへの SSH。`sitate restore-dest enroll` は、専用ユーザー(root と sudo を持たない)の作成、復元先の鍵の設置、隔離の設定だけにこれを使い、以後の復元テストは専用ユーザーで行う。
- 使い捨ての暗号化領域のため、`gocryptfs` と FUSE が使えること。`enroll` が前提を確かめる。領域には復元 1 回分の量を置ける空きが要る。
- 本番のネットワークに接続せず、本番のホスト名・DNS 名を持たない。

| 手順 | 目的 | 実行するもの | 完了確認コマンド | 期待する値 | 失敗時 |
|---|---|---|---|---|---|
| S1 | 復元先の前提を満たし、登録できることを実行前に確かめる | 外: 復元先に `gocryptfs` と FUSE を入れる、H3、`sitate restore-dest enroll --dry-run --dest ... --adapter ssh-host --admin-ref ...` | `sitate restore-dest enroll --dry-run --dest ... --adapter ssh-host --admin-ref ...` | `/result/plan/errors` = `[]` | `RESTORE_HOST_DENIED` → 管理用アカウントの権限を確かめる。`RESTORE_HOSTKEY_MISMATCH` → ホスト鍵を人と確かめる |
| S2 | 復元先を登録し、隔離を確かめる | `sitate restore-dest enroll --admin-ref dest-admin` | `sitate restore-dest check` | `/result/isolation/<i>/ok` = `true` | `RESTORE_ISOLATION_FAILED` → 隔離の設定を直す。`RESTORE_ENROLL_MISMATCH` → 登録の内容を確かめる |
