// 封筒の整形(spec 3.7「送る欄の表」、完了条件 9・20)。核の結果の封筒から、表に載る欄だけを取り出し、元サーバー由来のパスを path_ref に置き換え、
// 制御文字と不可視の Unicode を除き、長さを制限して、データの区画に包む。表に行の無いサブコマンドは封筒の行の欄だけ。
import table from "../data/send-fields.json" with { type: "json" };
import { ENVELOPE_MAX_CHARS, ENVELOPE_MAX_ITEMS } from "../constants.ts";
import type { PathRefTable } from "./path-ref.ts";

interface Field { readonly pointer: string; readonly transform?: "path_ref" }
interface Row { readonly where: string; readonly fields: readonly Field[]; readonly when?: { readonly codes: readonly string[]; readonly on: readonly ("error" | "warnings")[] } }
const ROWS = (table as { rows: Row[] }).rows;

const isRec = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
/** 制御文字(改行・タブを含む)と不可視の Unicode(ゼロ幅・方向制御・タグ文字・私用領域・BOM)。 */
const INVISIBLE = /[\p{Cc}\p{Cf}\p{Co}\p{Cs}\p{Zl}\p{Zp}\u{E0000}-\u{E007F}]/gu;

export function sanitizeText(s: string, max = ENVELOPE_MAX_CHARS): string {
  const clean = s.replace(INVISIBLE, " ");
  return [...clean].length > max ? [...clean].slice(0, max).join("") : clean;
}

function applicable(row: Row, env: Record<string, unknown>, command: string): boolean {
  if (row.where !== "envelope" && row.where !== command) return false;
  if (row.when === undefined) return true;
  const err = isRec(env.error) ? env.error : null;
  const warnings = Array.isArray(env.warnings) ? env.warnings : [];
  const code = err !== null && typeof err.code === "string" ? err.code : null;
  const wcodes = warnings.filter(isRec).map((w) => w.code);
  return (row.when.on.includes("error") && code !== null && row.when.codes.includes(code))
    || (row.when.on.includes("warnings") && wcodes.some((c) => typeof c === "string" && row.when!.codes.includes(c)));
}

/** pointer(`*` は配列の要素)に当たる値を、transform を掛けて out へ写す。 */
function copy(src: unknown, out: Record<string, unknown>, segs: readonly string[], refs: PathRefTable, tf: Field["transform"]): void {
  const [head, ...rest] = segs;
  if (head === undefined) return;
  if (head === "*") return; // 呼び出し側が配列の要素ごとに扱う
  if (!isRec(src) || !Object.hasOwn(src, head)) return;
  const v = src[head];
  if (rest.length === 0) { out[head] = leaf(v, refs, tf); return; }
  if (rest[0] === "*") {
    if (!Array.isArray(v)) return;
    const arr = (Array.isArray(out[head]) ? out[head] : (out[head] = v.map(() => ({})))) as unknown[];
    v.slice(0, ENVELOPE_MAX_ITEMS).forEach((el, i) => {
      if (rest.length === 1) { arr[i] = leaf(el, refs, tf); return; }
      if (!isRec(arr[i])) arr[i] = {};
      copy(el, arr[i] as Record<string, unknown>, rest.slice(1), refs, tf);
    });
    return;
  }
  if (!isRec(out[head])) out[head] = {};
  copy(v, out[head] as Record<string, unknown>, rest, refs, tf);
}

function leaf(v: unknown, refs: PathRefTable, tf: Field["transform"]): unknown {
  if (tf === "path_ref" && typeof v === "string") return refs.refFor(v);
  if (typeof v === "string") return sanitizeText(v);
  if (Array.isArray(v)) return v.slice(0, ENVELOPE_MAX_ITEMS).map((x) => leaf(x, refs, tf));
  if (isRec(v)) return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, leaf(x, refs, tf)]));
  return v;
}

/** 表の欄だけを残した封筒。`error.next[].argv`・`warnings[].next`・`result.targets[].next` の元サーバー由来のパスも path_ref に置き換える(argv の絶対パスを参照名へ)。 */
export function shapeEnvelope(env: Record<string, unknown>, refs: PathRefTable): Record<string, unknown> {
  const command = Array.isArray(env.command) ? env.command.join(" ") : "";
  const out: Record<string, unknown> = {};
  for (const row of ROWS) {
    if (!applicable(row, env, command)) continue;
    for (const f of row.fields) copy(env, out, f.pointer.split("/").slice(1), refs, f.transform);
  }
  hidePathsInNext(out, refs);
  return out;
}

/** `error.next[].argv`・`warnings[].next[].argv`・`result.targets[].next[].argv`(D-0183)の絶対パス(元サーバー由来)を参照名にする。 */
function hidePathsInNext(out: Record<string, unknown>, refs: PathRefTable): void {
  const nexts: unknown[] = [];
  if (isRec(out.error) && Array.isArray(out.error.next)) nexts.push(...out.error.next);
  if (Array.isArray(out.warnings)) for (const w of out.warnings) if (isRec(w) && Array.isArray(w.next)) nexts.push(...w.next);
  const targets = isRec(out.result) && Array.isArray(out.result.targets) ? out.result.targets : [];
  for (const t of targets) if (isRec(t) && Array.isArray(t.next)) nexts.push(...t.next);
  for (const n of nexts) {
    if (isRec(n) && Array.isArray(n.argv)) n.argv = n.argv.map((a: unknown) => (typeof a === "string" && a.startsWith("/") ? refs.refFor(a) : a));
  }
}

/** モデルへ渡す文字列: データの区画(開始の行の command は封筒の command の語を空白 1 つでつないだもの)。 */
export function toDataSection(shaped: Record<string, unknown>): string {
  const command = Array.isArray(shaped.command) ? shaped.command.map(String).join(" ") : "";
  const body = JSON.stringify(shaped).replace(/<\/?sitate-data[^>]*>/gi, " ");
  return `<sitate-data command="${command.replace(/[^a-z -]/g, "")}">\n${body}\n</sitate-data>`;
}
