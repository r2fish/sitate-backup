// 承認カードの欄の表(規則ファイル assets/rules/approval-card-format.md の `ops` と同じ内容)と、欄の見出し・値の描画。
// 見出しは `card.field.<op>.<欄名>` をカタログから引く。カタログに無い欄は描かず、カードは未知の欄として承認を無効にする。
import type { CardRow, Translate } from "./approval-card.ts";

export interface OpFields { readonly target: readonly string[]; readonly fields: readonly string[] }

const DEF = ["diff", "def_hash_after", "estimate_after_jpy", "cost_ceiling_yen", "over_ceiling"];
/** op ごとの欄(区画 2 に描く target と、区画 3 に描く fields。欄の順はそのまま描く順)。 */
export const CARD_FIELDS: Readonly<Record<string, OpFields>> = {
  "storage-init": { target: ["bucket"], fields: ["key_ref", "reissue", "revoke_key_ids"] },
  "read-key-issue": { target: ["bucket"], fields: [] },
  "restore-change": { target: ["host_key_fp"], fields: ["before", "after", "needs_secret"] },
  "restore-elsewhere": { target: ["backup", "to", "host_key_fp"], fields: ["read_bytes"] },
  "restore-session-extend": { target: ["session_id", "backup"], fields: ["expires_at_before", "expires_at_after"] },
  "restore-dest-release": { target: ["session_id", "backup", "authorized_key_fp", "allow_from"], fields: ["restore_change"] },
  "source-change": { target: ["admin_user", "host_key_fp"], fields: ["before", "after", "needs_secret"] },
  "notify-change": { target: [], fields: ["before", "after"] },
  "notify-add": { target: [], fields: ["before", "after"] },
  "key-rotate": { target: [], fields: ["keyring_version", "over_ceiling"] },
  "key-rotate-waive": { target: [], fields: ["cause", "adopted_at"] },
  "phrase-reissue": { target: [], fields: ["keyring_version"] },
  "delay-change": { target: [], fields: ["before", "after", "applies_to"] },
  "approval-key-change": { target: [], fields: ["key_fp_before", "key_fp_after", "key_type"] },
  "restore-test-reduce": { target: [], fields: ["from", "to"] },
  "restore-test-increase": { target: [], fields: ["from", "to"] },
  "storage-read-extra": { target: ["backup"], fields: ["read_bytes", "month_read_bytes", "reserved_bytes", "planned_bytes"] },
  "def-change": { target: [], fields: DEF },
  "def-change-unlisted": { target: [], fields: DEF },
  "checks-weaken": { target: [], fields: DEF },
  "hold-release": { target: [], fields: ["causes", "since", "days_since_last_ok"] },
  "adapter-add": { target: ["kind", "name"], fields: ["version", "sha256", "signature_ok", "replaces"] },
  seal: { target: [], fields: ["definition", "host_keys", "approval_key_fp", "weaker_items", "definition_from"] },
  attest: { target: [], fields: [] },
};
/** 共通の欄と、描き方を別に決めた欄(区画 4〜7)。 */
export const COMMON_FIELDS: readonly string[] = ["reversible", "errors", "estimate_delta_jpy", "conflicts", "proof_required", "machine"];
/** attest の plan は本文の材料(区画 7)で、欄の一覧を持たない。 */
const FREE_FORM_OPS: readonly string[] = ["attest"];

const DATE_FIELDS = ["expires_at_before", "expires_at_after", "adopted_at", "since", "at", "stored_at"];
const BYTES_FIELDS = ["read_bytes", "month_read_bytes", "reserved_bytes", "planned_bytes"];
const YEN_FIELDS = ["estimate_after_jpy", "cost_ceiling_yen"];
const BACKUP_PARTS = ["at", "stored_at", "health", "targets_ok", "restore_verified"];

const isRec = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
export const fieldKey = (op: string, name: string): string => `card.field.${op}.${name}`;
export const isFreeForm = (op: string): boolean => FREE_FORM_OPS.includes(op);

function has(t: Translate, key: string): boolean {
  try { t(key); return true; } catch { return false; }
}

/** op の欄のうち plan にあるものの名前(target と fields の順)。 */
export function presentFields(op: string, plan: Record<string, unknown>): string[] {
  const spec = CARD_FIELDS[op];
  return spec === undefined ? [] : [...spec.target, ...spec.fields].filter((n) => plan[n] !== undefined);
}

/** plan に、定義に無い欄、または見出しのキーがカタログに無い欄があるか。 */
export function hasUnknownField(op: string, plan: Record<string, unknown>, t: Translate): boolean {
  const spec = CARD_FIELDS[op];
  if (spec === undefined || isFreeForm(op)) return false;
  const known = [...spec.target, ...spec.fields];
  if (Object.keys(plan).some((k) => !COMMON_FIELDS.includes(k) && !known.includes(k))) return true;
  return presentFields(op, plan).some((n) => !has(t, fieldKey(op, n)));
}

function size(n: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1000 && i < units.length - 1) { v /= 1000; i += 1; }
  return `${new Intl.NumberFormat("en", { maximumFractionDigits: 1 }).format(v)} ${units[i]!}`;
}

/** 値の型ごとの書式: 日付は現地時刻、大きさは単位付き、金額は円、真偽値は固定文。 */
export function formatValue(name: string, v: unknown, t: Translate): string {
  if (v === null) return t("card.value.none");
  if (typeof v === "boolean") return t(v ? "card.value.true" : "card.value.false");
  if (typeof v === "string" && DATE_FIELDS.includes(name) && !Number.isNaN(Date.parse(v))) return t("card.value.date", { at: new Date(v) });
  if (typeof v === "number" && BYTES_FIELDS.includes(name)) return size(v);
  if (typeof v === "number" && YEN_FIELDS.includes(name)) return t("card.value.yen", { amount: v });
  if (typeof v === "string") return v;
  if (typeof v === "number") return String(v);
  if (Array.isArray(v) && v.every((x) => typeof x === "string")) return v.join(", ");
  return JSON.stringify(v);
}

/** 世代(backup)は区画 2 に at・stored_at・health・targets_ok・restore_verified を描き、stored_at_mismatch が true なら印を付ける。 */
function backupRows(op: string, v: Record<string, unknown>, t: Translate): CardRow[] {
  const rows: CardRow[] = [{ label: t(fieldKey(op, "backup")), value: "" }];
  for (const p of BACKUP_PARTS) if (v[p] !== undefined) rows.push({ label: t(`card.backup.${p}`), value: formatValue(p, v[p], t) });
  if (v.stored_at_mismatch === true) rows.push({ label: t("card.backup.stored_at_mismatch"), value: t("card.value.true") });
  return rows;
}

/** 欄の名前の並びを、見出し(カタログ)と書式付きの値の行にする。見出しの無い欄は描かない。 */
export function labelledRows(op: string, plan: Record<string, unknown>, names: readonly string[], t: Translate): CardRow[] {
  const rows: CardRow[] = [];
  for (const n of names) {
    const v = plan[n];
    if (v === undefined || !has(t, fieldKey(op, n))) continue;
    if (n === "backup" && isRec(v)) rows.push(...backupRows(op, v, t));
    else rows.push({ label: t(fieldKey(op, n)), value: formatValue(n, v, t) });
  }
  return rows;
}

/** 区画 3 の欄: fields の順。別の区画で描く欄は除く。diff は JSON Pointer ごとに 1 行。 */
export function changeFieldRows(op: string, plan: Record<string, unknown>, t: Translate, costShown: boolean): CardRow[] {
  const spec = CARD_FIELDS[op];
  if (spec === undefined || isFreeForm(op)) return [];
  const skip = [
    ...(op === "seal" ? ["definition"] : []), // source の対象は sourceRows が描く。宛先(host)はここで 1 行描く
    ...(op === "hold-release" ? spec.fields : []), // 区画 7 に描く
    ...(costShown ? YEN_FIELDS : []), // 区画 5 に描く
  ];
  const rows: CardRow[] = [];
  const host = op === "seal" && isRec(plan.definition) && isRec(plan.definition.source) ? plan.definition.source.host : undefined;
  if (typeof host === "string" && has(t, fieldKey(op, "definition"))) rows.push({ label: t(fieldKey(op, "definition")), value: host });
  for (const n of spec.fields) {
    if (skip.includes(n) || plan[n] === undefined) continue;
    const v = plan[n];
    if (n === "diff" && Array.isArray(v)) {
      if (has(t, fieldKey(op, n))) rows.push({ label: t(fieldKey(op, n)), value: "" });
      for (const d of v) if (isRec(d)) rows.push({ label: String(d.pointer), value: `${JSON.stringify(d.before)} → ${JSON.stringify(d.after)}` });
    } else rows.push(...labelledRows(op, plan, [n], t));
  }
  return rows;
}
