// 承認カードの描画モデル(spec 3.7 の承認カードの書式、完了条件 5)。入力は `pending --id` の記録だけ。文はカタログの固定文だけで、
// モデルの生成文は「AI の説明」の別枠に分けて入れる(区画 9)。署名する値はこの記録から計算した canonical_hash で、モデルを経由しない。
import { canonicalHash } from "./approval-sign.ts";
import type { PendingRecord } from "./approval-sign.ts";
import { sourceRows } from "./approval-card-source.ts";
import type { SurveySizes } from "./approval-card-source.ts";
import { CARD_FIELDS, changeFieldRows, hasUnknownField, labelledRows } from "./approval-card-fields.ts";
import type { FormatArgs } from "../i18n/format.ts";

export type Translate = (key: string, args?: FormatArgs) => string;

export interface CardRow { readonly label: string; readonly value: string }
export interface CardSection {
  /** 区画の番号(spec の書式の表: 1 見出し … 9 AI の説明)。 */
  readonly no: 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9;
  readonly title: string;
  readonly rows: readonly CardRow[];
  readonly lines: readonly string[];
}

export type DisabledReason = "unknown_op" | "unknown_field" | "hash_mismatch";

export interface CardModel {
  readonly changeId: string;
  readonly op: string;
  /** 署名する値(記録から計算)。 */
  readonly canonicalHash: string;
  readonly sections: readonly CardSection[];
  /** 確認語の入力欄を置く宛先(proof_required[] の順)。 */
  readonly proofRecipients: readonly string[];
  /** 確認語の入力欄の横に出す文(受け取り手ごと。完了条件 5)。 */
  readonly proofLabels: Readonly<Record<string, string>>;
  /** ボタンの文(カタログの固定文)。 */
  readonly labels: { readonly approve: string; readonly cancel: string; readonly close: string };
  readonly approveEnabled: boolean;
  readonly disabledReason: DisabledReason | null;
  /** 未知の op・欄のときの、アプリの更新を促す固定文。 */
  readonly updatePrompt: string | null;
}

export interface CardContext {
  readonly t: Translate;
  /** エージェントの説明(モデルの文)。区画 9 の別枠にだけ入る。 */
  readonly aiExplanation?: string | null;
  /** 同じ会話の `source survey` の対象 id → 大きさの表記。無ければ null。 */
  readonly surveySizes?: SurveySizes | null;
  /** 調査で見つけて対象にも除外にも無い候補の数。 */
  readonly undeclaredCandidates?: number;
  readonly locale?: string;
}

const PROOF_OPS = ["seal", "notify-add", "notify-change"];

/** seal の定義は plan でなく args.definition にある(核は plan に定義を載せない)。描く側には plan に定義を足した見え方で渡す。 */
const withDefinition = (op: string, record: PendingRecord, plan: Record<string, unknown>): Record<string, unknown> =>
  op === "seal" && isRec(record.args) && isRec(record.args.definition) ? { ...plan, definition: record.args.definition } : plan;

const isRec = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const show = (v: unknown): string => (typeof v === "string" ? v : typeof v === "number" || typeof v === "boolean" ? String(v) : JSON.stringify(v));
const strs = (v: unknown): string[] => (Array.isArray(v) ? v.map(show) : []);

function unknownReason(op: string, plan: Record<string, unknown>, tHas: boolean, t: Translate): DisabledReason | null {
  if (!tHas || !Object.hasOwn(CARD_FIELDS, op)) return "unknown_op";
  return hasUnknownField(op, plan, t) ? "unknown_field" : null;
}

export function buildCard(record: PendingRecord, ctx: CardContext): CardModel {
  const t = ctx.t;
  const op = typeof record.op === "string" ? record.op : "";
  const plan = isRec(record.plan) ? record.plan : {};
  const hash = canonicalHash(record);
  const titleKey = `card.op.${op}.title`;
  const hasTitle = ((): boolean => { try { t(titleKey); return true; } catch { return false; } })();
  const reason: DisabledReason | null = typeof record.canonical_hash === "string" && record.canonical_hash !== hash
    ? "hash_mismatch" : unknownReason(op, plan, hasTitle, t);
  const sections: CardSection[] = [];
  const delay = typeof record.delay_hours === "number" ? record.delay_hours : 0;
  const heading = hasTitle ? t(titleKey) : op;
  sections.push({ no: 1, title: heading, rows: [], lines: [delay > 0 ? t("card.delay.hours", { hours: delay }) : t("card.delay.none")] });
  const target = record.target === null || record.target === undefined ? [] : [{ label: t("card.section.target"), value: show(record.target) }];
  sections.push({ no: 2, title: t("card.section.target"), rows: [...target, ...labelledRows(op, plan, CARD_FIELDS[op]?.target ?? [], t)], lines: op === "restore-change" || plan.host_key_fp !== undefined ? [t("card.host_key_fp.verify_note")] : [] });
  const delta = plan.estimate_delta_jpy;
  const costShown = typeof delta === "number";
  sections.push({ no: 3, title: t("card.section.change"), rows: [...changeFieldRows(op, plan, t, costShown), ...sourceRows(op, withDefinition(op, record, plan), ctx.surveySizes ?? null, ctx.undeclaredCandidates ?? 0, t)], lines: [] });
  sections.push({ no: 4, title: t("card.section.reversible"), rows: [], lines: plan.reversible === false ? [t("card.mark.irreversible")] : [] });
  if (costShown) {
    const lines = [t("card.cost.delta", { delta_jpy: delta })];
    const after = plan.estimate_after_jpy;
    if (typeof after === "number" && typeof plan.cost_ceiling_yen === "number") lines.push(t("card.cost.after", { monthly_jpy: after, ceiling_jpy: plan.cost_ceiling_yen }));
    sections.push({ no: 5, title: t("card.section.cost"), rows: [], lines });
  }
  sections.push(timingSection(record, delay, plan, t));
  const proofs = proofsOf(op, withDefinition(op, record, plan));
  sections.push(opSection(op, record, plan, proofs, t));
  if (ctx.aiExplanation !== undefined && ctx.aiExplanation !== null) sections.push({ no: 9, title: t("card.ai_explanation.heading"), rows: [], lines: [ctx.aiExplanation] });
  else if (op === "attest" && isRec(record.args) && typeof record.args.note === "string") sections.push({ no: 9, title: t("card.ai_explanation.heading"), rows: [], lines: [record.args.note] });
  return {
    changeId: typeof record.change_id === "string" ? record.change_id : "", op, canonicalHash: hash, sections, proofRecipients: proofs, proofLabels: Object.fromEntries(proofs.map((r) => [r, t("card.confirm_word.label", { recipient: r })])),
    labels: { approve: t("card.actions.approve"), cancel: t("card.actions.cancel"), close: t("card.actions.close") },
    approveEnabled: reason === null, disabledReason: reason, updatePrompt: reason === "unknown_op" || reason === "unknown_field" ? t("card.unknown.update_prompt") : null,
  };
}

/** 確認語を入れさせる宛先。seal は核が渡した定義の notify.to の全部(plan に proof_required は無い)、notify-add・notify-change は plan.proof_required[]。 */
function proofsOf(op: string, plan: Record<string, unknown>): string[] {
  if (op === "seal") return isRec(plan.definition) && isRec(plan.definition.notify) ? strs(plan.definition.notify.to) : [];
  return PROOF_OPS.includes(op) ? strs(plan.proof_required) : [];
}

function timingSection(record: PendingRecord, delay: number, plan: Record<string, unknown>, t: Translate): CardSection {
  const lines = [delay > 0 ? t("card.timing.after_delay", { hours: delay }) : t("card.timing.immediate")];
  if (typeof record.expires_at === "string") lines.push(t("card.timing.expires", { expires_at: new Date(record.expires_at) }));
  const conflicts = strs(plan.conflicts);
  if (conflicts.length > 0) lines.push(t("card.conflicts.note"), ...conflicts);
  return { no: 6, title: t("card.section.timing"), rows: [], lines };
}

function opSection(op: string, record: PendingRecord, plan: Record<string, unknown>, proofs: readonly string[], t: Translate): CardSection {
  const lines: string[] = [];
  const rows: CardRow[] = [];
  for (const r of proofs) lines.push(t("card.confirm_word.label", { recipient: r }));
  if (proofs.length > 0) lines.push(t("card.confirm_word.note"));
  const machine = strs(plan.machine);
  if (machine.length > 0) lines.push(t("card.proof.machine_heading"), ...machine);
  if (op === "attest" && isRec(record.args)) {
    if (record.args.step === "R3") lines.push(t("card.attest.r3.body", { old_key_ids: strs(plan.old_key_ids).join(", ") }));
    if (record.args.step === "R7") lines.push(t("card.attest.r7.body", { secret_refs: strs(plan.secret_refs).join(", ") }));
  }
  if (op === "hold-release") rows.push(...labelledRows(op, plan, CARD_FIELDS["hold-release"]!.fields, t));
  return { no: 7, title: t("card.section.op_specific"), rows, lines };
}
