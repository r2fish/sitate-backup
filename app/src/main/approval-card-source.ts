// 承認カードの区画 3 のうち、定義の `source` 節(seal・source-change)の描画(spec 3.7。完了条件 5)。
// 実名(根のパス・DB 名)はアプリが端末の中で `pending` の記録から描く。モデルへは送らない。
import type { CardRow, Translate } from "./approval-card.ts";

/** 同じ会話の `source survey` の結果から主プロセスが作る、対象 id → 大きさの表記。 */
export type SurveySizes = Readonly<Record<string, string>>;

const isRec = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

interface Entry { readonly id: string; readonly kind: string; readonly name: string; readonly port: string | null }

function entryOf(raw: unknown): Entry | null {
  if (!isRec(raw) || typeof raw.id !== "string") return null;
  for (const kind of ["files", "postgresql", "mount", "postgresql_cluster"]) {
    const v = raw[kind];
    if (typeof v === "string" || typeof v === "number") {
      const port = kind === "postgresql_cluster" ? String(v) : typeof raw.port === "number" || typeof raw.port === "string" ? String(raw.port) : null;
      return { id: raw.id, kind, name: String(v), port };
    }
  }
  return { id: raw.id, kind: "", name: "", port: null };
}

/** plan から source 節を探す: seal は定義の source、source-change は after。 */
function sourceNode(op: string, plan: Record<string, unknown>): Record<string, unknown> | null {
  if (op === "source-change") return isRec(plan.after) ? plan.after : null;
  if (op === "seal") {
    const def = isRec(plan.definition) ? plan.definition : isRec(plan.def) ? plan.def : null;
    return def !== null && isRec(def.source) ? def.source : isRec(plan.source) ? plan.source : null;
  }
  return null;
}

export function sourceRows(op: string, plan: Record<string, unknown>, sizes: SurveySizes | null, undeclared: number, t: Translate): CardRow[] {
  const node = sourceNode(op, plan);
  if (node === null) return [];
  const rows: CardRow[] = [];
  const targets = Array.isArray(node.targets) ? node.targets.map(entryOf).filter((e): e is Entry => e !== null) : [];
  if (targets.length > 0) rows.push({ label: t("card.source.targets_heading"), value: "" });
  for (const e of targets) {
    const size = sizes !== null && sizes[e.id] !== undefined ? sizes[e.id]! : t("card.source.size_note");
    rows.push({ label: `${e.id} (${e.kind})`, value: `${e.name} / ${size}` });
  }
  const declined = Array.isArray(node.declined) ? node.declined.map(entryOf).filter((e): e is Entry => e !== null) : [];
  if (declined.length > 0) rows.push({ label: t("card.source.excluded_heading"), value: "" });
  for (const e of declined) rows.push({ label: `${e.id} (${e.kind})`, value: e.port !== null ? `${e.name} :${e.port}` : e.name });
  if (undeclared > 0) rows.push({ label: t("card.source.undeclared_candidates", { count: undeclared }), value: "" });
  return rows;
}
