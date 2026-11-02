// 手順の合格の判定(spec 3.7「手順の合格の判定」、完了条件 30)。同梱の手順書の表から手順の完了確認コマンドと期待する値(「JSON Pointer 演算子 値」)を読み、
// 道具の結果(整形した封筒=送る欄だけ)を受けるたびに、その会話でまだ合格していない手順を照らす。合格は 1 つの会話で手順ごとに 1 度だけ数える。
// 判定は表示と往復の数のためだけに使う: 手順の進め方と秘密の扱いの判定には使わない(呼び出し側もそうする)。
const HEADER = "| 手順 | 目的 | 実行するもの | 完了確認コマンド | 期待する値 | 失敗時 |";

type Op = "=" | "⊇" | "⊆";
interface Expect { readonly pointer: string; readonly op: Op; readonly rhs: string }
interface Check { readonly words: readonly string[]; readonly dryRun: boolean; readonly expects: readonly Expect[] }
export interface Step { readonly id: string; readonly checks: readonly Check[] }

/** 印({{名前}})の値。無い印を含む期待は満たされない扱い。 */
export type Marks = Readonly<Record<string, unknown>>;

export function parseSteps(procedure: string): Step[] {
  const lines = procedure.split("\n").map((l) => l.trimEnd());
  const at = lines.indexOf(HEADER);
  if (at < 0) return [];
  const steps: Step[] = [];
  for (const line of lines.slice(at + 2)) {
    if (!line.startsWith("|")) break;
    const cells = line.split("|").slice(1, -1).map((c) => c.trim());
    if (cells.length !== 6) continue;
    const id = cells[0]!;
    const cmds = [...(cells[3] ?? "").matchAll(/`sitate ([^`]+)`/g)].map((m) => m[1]!.trim().split(/\s+/));
    const sections = splitSections(cells[4] ?? "", cmds.length);
    steps.push({ id, checks: cmds.map((tokens, i) => ({ words: subcommandWords(tokens), dryRun: tokens.includes("--dry-run"), expects: parseExpects(sections[i] ?? "") })) });
  }
  return steps;
}

/** `init-storage --dry-run ...` → ["init-storage"]。フラグ・プレースホルダ・`...` の手前までの語。 */
const subcommandWords = (tokens: readonly string[]): string[] => { const w: string[] = []; for (const t of tokens) { if (t.startsWith("-") || t.startsWith("<") || t === "..." || t.includes("=")) break; w.push(t); } return w; };

function splitSections(text: string, n: number): string[] {
  if (n <= 1) return [text];
  const parts = text.split(/(?:^|。)\s*\d+ つ目:/).slice(1);
  return parts.length === n ? parts : [text];
}

function parseExpects(text: string): Expect[] {
  const out: Expect[] = [];
  for (const part of text.split("、")) {
    const m = /^\s*`?(\/\S*?)`?\s*(=|⊇|⊆)\s*(.+?)\s*$/.exec(part.replace(/。$/, ""));
    if (m) out.push({ pointer: m[1]!, op: m[2] as Op, rhs: m[3]!.replace(/^`|`$/g, "") });
  }
  return out;
}

/** JSON Pointer の評価。`<i>` は配列の全要素に展開する(値の列を返す)。 */
function resolve(doc: unknown, pointer: string, index: readonly number[] = []): unknown[] | undefined {
  const parts = pointer.split("/").slice(1);
  let level = 0;
  let cur: unknown[] = [doc];
  for (const p of parts) {
    const next: unknown[] = [];
    for (const c of cur) {
      if (p === "<i>") {
        if (!Array.isArray(c)) return undefined;
        if (index[level] !== undefined) next.push(c[index[level]!]); else next.push(...c);
        continue;
      }
      if (typeof c !== "object" || c === null || !Object.hasOwn(c, p)) return undefined;
      next.push((c as Record<string, unknown>)[p]);
    }
    if (p === "<i>") level++;
    cur = next;
  }
  return cur;
}

const same = (a: unknown, b: unknown): boolean => JSON.stringify(a) === JSON.stringify(b);

function literal(rhs: string, marks: Marks): { ok: true; value: unknown } | { ok: false } {
  const m = /^\{\{([A-Z_]+)\}\}$/.exec(rhs);
  if (m) return Object.hasOwn(marks, m[1]!) ? { ok: true, value: marks[m[1]!] } : { ok: false };
  try { return { ok: true, value: JSON.parse(rhs) }; } catch { return { ok: false }; }
}

function holds(env: unknown, e: Expect, marks: Marks): boolean {
  if (e.rhs.startsWith("/")) {
    // 別の欄への参照: 同じ要素番号の対で等しい。
    const left = resolve(env, e.pointer);
    const right = resolve(env, e.rhs);
    return left !== undefined && right !== undefined && left.length === right.length && left.every((v, i) => same(v, right[i]));
  }
  const rhs = literal(e.rhs, marks);
  if (!rhs.ok) return false;
  const vals = resolve(env, e.pointer);
  if (vals === undefined) return false;
  if (e.op === "=") return vals.length > 0 && vals.every((v) => same(v, rhs.value));
  if (!Array.isArray(rhs.value)) return false;
  if (e.op === "⊇") { const have = vals.length === 1 && Array.isArray(vals[0]) ? (vals[0] as unknown[]) : vals; return rhs.value.every((x) => have.some((h) => same(h, x))); }
  return vals.every((v) => (rhs.value as unknown[]).some((x) => same(v, x))); // ⊆: 許容一覧の外を含まない(空は満たす)
}

export class StepJudge {
  readonly #steps: readonly Step[];
  readonly #marks: Marks;
  readonly #passed = new Set<string>();
  /** 手順 → コマンドの番号 → 最後の結果。完了確認コマンドが 2 つの手順は両方の最後の結果で判定する。 */
  readonly #last = new Map<string, Map<number, unknown>>();

  constructor(procedure: string, marks: Marks = {}) {
    this.#steps = parseSteps(procedure);
    this.#marks = marks;
  }

  get stepCount(): number {
    return this.#steps.length;
  }

  /** 会話を捨てたとき(新しい会話)。合格の記録を捨てる。 */
  reset(): void {
    this.#passed.clear();
    this.#last.clear();
  }

  /** 道具の結果(整形した封筒)を受ける。この結果で新しく合格した手順の ID を返す(同じ手順は 2 度返さない)。 */
  judge(env: { command?: unknown; dry_run?: unknown } & Record<string, unknown>): string[] {
    const cmd = Array.isArray(env.command) ? (env.command as unknown[]).map(String) : [];
    const dry = env.dry_run === true;
    const fresh: string[] = [];
    for (const step of this.#steps) {
      if (this.#passed.has(step.id)) continue;
      step.checks.forEach((c, i) => {
        if (c.words.length > 0 && same(c.words, cmd) && c.dryRun === dry) {
          let m = this.#last.get(step.id);
          if (m === undefined) this.#last.set(step.id, (m = new Map()));
          m.set(i, env);
        }
      });
      const m = this.#last.get(step.id);
      if (step.checks.length === 0 || m === undefined || step.checks.some((_c, i) => !m.has(i))) continue;
      if (step.checks.every((c, i) => c.expects.length > 0 && c.expects.every((e) => holds(m.get(i), e, this.#marks)))) { this.#passed.add(step.id); fresh.push(step.id); }
    }
    return fresh;
  }
}
