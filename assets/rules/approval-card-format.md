# 承認カードの書式の定義

承認カードは `pending --id` の記録だけから、メッセージカタログの固定文と、この定義の型ごとの書式で描く。モデルの生成文を混ぜない。

## 読み方(アプリ向け)

- 下の JSON が機械が読む本体。区画の順、`op` ごとの欄の順、見出しのキーを持つ。JSON の外の文は説明。
- 見出しのキーは、区画は `card.section.<区画名>`、`op` の名前は `card.op.<op>.title`、欄は `card.field.<op>.<欄名>`。キーがカタログに無い欄は、描かずに更新を促す固定文(`card.unknown.update_prompt`)を出して承認の操作を有効にしない。
- `fields` に書いた欄の順に、区画 3(変更前後の値)へ描く。`target_fields` は区画 2(対象)へ描く。`plan` に `fields` と `target_fields` と共通の欄(`reversible`、`errors`、`estimate_delta_jpy`、`conflicts`)の外の欄があれば、未知の欄として扱う。
- 値の型の書式: 日付と時刻は現地時刻、大きさは単位付き、金額は円、真偽値は固定文。

## 区画の順

1. 見出し(`op` の名前と遅延の有無。戻せない変更には印)
2. 対象
3. 変更前後の値
4. 取り消しの可否
5. 費用の増減(`estimate_delta_jpy` のある `op` だけ)
6. 時期(適用の時期と `expires_at`。`conflicts` が空でなければここに並べる)
7. `op` 固有の区画(確認語の入力欄、停止の原因、`attest` の本文)
8. 対象名の入力(イテレーション 4 以降。対象名の入力を要する `op` だけ)
9. AI の説明(別枠。見出しは固定文)

## op ごとの欄

```json
{
  "sections": ["heading", "target", "change", "reversible", "cost", "timing", "op_specific", "target_name", "ai_explanation"],
  "ops": {
    "storage-init": {"target_fields": ["bucket"], "fields": ["key_ref", "reissue", "revoke_key_ids"]},
    "read-key-issue": {"target_fields": ["bucket"], "fields": []},
    "restore-change": {"target_fields": ["host_key_fp"], "fields": ["before", "after", "needs_secret"]},
    "restore-elsewhere": {"target_fields": ["backup", "to", "host_key_fp"], "fields": ["read_bytes"]},
    "restore-session-extend": {"target_fields": ["session_id", "backup"], "fields": ["expires_at_before", "expires_at_after"]},
    "restore-dest-release": {"target_fields": ["session_id", "backup", "authorized_key_fp", "allow_from"], "fields": ["restore_change"]},
    "source-change": {"target_fields": ["host_key_fp"], "fields": ["before", "after", "needs_secret"]},
    "notify-change": {"target_fields": [], "fields": ["before", "after"]},
    "notify-add": {"target_fields": [], "fields": ["before", "after"]},
    "key-rotate": {"target_fields": [], "fields": ["keyring_version", "over_ceiling"]},
    "key-rotate-waive": {"target_fields": [], "fields": ["cause", "adopted_at"]},
    "phrase-reissue": {"target_fields": [], "fields": ["keyring_version"]},
    "delay-change": {"target_fields": [], "fields": ["before", "after", "applies_to"]},
    "approval-key-change": {"target_fields": [], "fields": ["key_fp_before", "key_fp_after", "key_type"]},
    "restore-test-reduce": {"target_fields": [], "fields": ["from", "to"]},
    "restore-test-increase": {"target_fields": [], "fields": ["from", "to"]},
    "storage-read-extra": {"target_fields": ["backup"], "fields": ["read_bytes", "month_read_bytes", "reserved_bytes", "planned_bytes"]},
    "def-change": {"target_fields": [], "fields": ["diff", "def_hash_after", "estimate_after_jpy", "cost_ceiling_yen", "over_ceiling"]},
    "def-change-unlisted": {"target_fields": [], "fields": ["diff", "def_hash_after", "estimate_after_jpy", "cost_ceiling_yen", "over_ceiling"]},
    "checks-weaken": {"target_fields": [], "fields": ["diff", "def_hash_after", "estimate_after_jpy", "cost_ceiling_yen", "over_ceiling"]},
    "hold-release": {"target_fields": [], "fields": ["causes", "since", "days_since_last_ok"]},
    "adapter-add": {"target_fields": ["kind", "name"], "fields": ["version", "sha256", "signature_ok", "replaces"]},
    "seal": {"target_fields": [], "fields": ["definition", "host_keys", "approval_key_fp", "weaker_items", "definition_from"]},
    "attest": {"target_fields": [], "fields": []}
  },
  "special_fields": {
    "source": "対象の一覧(対象 id・種別・実名・大きさ)と除外の一覧(除外 id・種別・実名・ポート)を区画 3 に描く。実名はアプリが端末の中で記録から描き、モデルへ送らない。大きさが無いときは固定文(card.source.size_note)を描く。調査で見つけて対象にも除外にも無い候補があれば、その数を固定文(card.source.undeclared_candidates)で示す。",
    "backup": "区画 2 に世代の at・stored_at・health・targets_ok・restore_verified を描き、stored_at_mismatch が true なら印を付ける。",
    "host_key_fp": "区画 2 に SHA256: の表記を省かずに描き、ホストの側で確かめた指紋と照らすよう固定文(card.host_key_fp.verify_note)で示す。",
    "conflicts": "空でなければ区画 6 に change_id を並べ、どれかが先に適用されるとこの変更は失敗して提案し直しが要ることを固定文(card.conflicts.note)で示す。",
    "proof_required": "notify-add と notify-change の区画 7 に、確認語の入力欄を宛先ごとに 1 つ置く。machine の宛先には欄を置かず、応答を確かめた宛先として並べる(card.proof.machine_heading)。"
  },
  "unknown_op": "定義に無い op と、既知の op の plan に定義に無い欄があるときは、カードを描くが承認の操作を有効にせず、更新を促す固定文を出す。取り消しはできる。"
}
```
