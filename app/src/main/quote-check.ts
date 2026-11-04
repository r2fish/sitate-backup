// モデルの文の引用の照合(T-55。spec 3.7「引用の形と判定」、完了条件 22・26・29)。
// 印 `[[<サブコマンド>:<JSON Pointer>=<値>]]` を、その会話でそのサブコマンドの最後の結果と照らす。照合を掛けるのは核の結果の値を主張する文だけ。
import { appErrorSection } from "./app-section.ts";
import type { PathRefTable } from "./path-ref.ts";

const MARK = /\[\[([^:[\]]+):(\/[^=\]]*)=([^\]]*)\]\]/g;
/** 問いの終わり方の一覧(規則ファイル quote-check-forms の既定。規則ファイルが中身を持つまでの既定)。 */
export const QUESTION_ENDINGS = ["ですか", "でしょうか", "ますか", "ませんか", "かな"];
/** 値の語の形(spec 3.7: 原因コード・識別子・日付と時刻・単位付きの大きさ・金額・件数)。単位の無い数は値に数えない。 */
const VALUE_FORMS: readonly RegExp[] = [
  /\b[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+\b/g, // 原因コード・アプリの原因コード
  /\bc-\d{8}-\d{4,}\b/g, // change_id
  /\b(?:sha256|SHA256):[A-Za-z0-9+/=]{8,}/g, // ハッシュ・ホスト鍵の指紋
  /\b\d{4}-\d{2}-\d{2}(?:T\d{2}:\d{2}(?::\d{2})?(?:Z|[+-]\d{2}:\d{2})?)?/g, // 日付と時刻
  /\d[\d,]*(?:\.\d+)?\s?(?:[KMGT]i?B|B)\b/g, // 単位付きの大きさ
  /\d[\d,]*\s?円/g, // 金額
  /\d[\d,]*\s?件/g, // 件数
];

/** その会話のサブコマンドごとの最後の結果(送る欄だけに絞った封筒)。キーは封筒の command を空白 1 つでつないだもの。 */
export type LastResults = ReadonlyMap<string, unknown>;

export interface QuoteInput {
  readonly text: string;
  readonly results: LastResults;
  /** 人が同じ会話のチャット欄に書いた文字列。 */
  readonly humanTexts?: readonly string[];
  /** 引用した説明書の節・手順書の手順の本文。 */
  readonly citedBodies?: readonly string[];
}

export type QuoteVerdict =
  | { readonly ok: true; readonly text: string }
  | { readonly ok: false; readonly rejected: readonly string[]; readonly missing: readonly string[]; readonly unquoted: number };

function pointerGet(root: unknown, pointer: string): { found: boolean; value?: unknown } {
  let cur = root;
  for (const raw of pointer.split("/").slice(1)) {
    const seg = raw.replace(/~1/g, "/").replace(/~0/g, "~");
    if (Array.isArray(cur) && /^\d+$/.test(seg) && Number(seg) < cur.length) cur = cur[Number(seg)];
    else if (typeof cur === "object" && cur !== null && !Array.isArray(cur) && Object.hasOwn(cur, seg)) cur = (cur as Record<string, unknown>)[seg];
    else return { found: false };
  }
  return { found: true, value: cur };
}

/** 印の値と相手の欄の値の一致(文字列は完全一致、数は数値として、真偽値は true/false)。表記の揺れは認めない。 */
function matches(markValue: string, actual: unknown): boolean {
  if (typeof actual === "string") return markValue === actual;
  if (typeof actual === "number") return /^-?\d+(\.\d+)?(e[+-]?\d+)?$/i.test(markValue) && Number(markValue) === actual;
  if (typeof actual === "boolean") return markValue === String(actual);
  if (actual === null) return markValue === "null";
  return markValue === JSON.stringify(actual);
}

export function splitSentences(text: string): string[] {
  return text.split(/(?<=[。！!？?\n])/).map((s) => s.trim()).filter((s) => s !== "");
}

const isQuestion = (s: string): boolean => /[？?]$/.test(s) || QUESTION_ENDINGS.some((e) => s.replace(/[。.\s]+$/, "").endsWith(e));

function outsideValues(sentence: string): string[] {
  const bare = sentence.replace(MARK, " ");
  return VALUE_FORMS.flatMap((re) => [...bare.matchAll(new RegExp(re.source, re.flags))].map((m) => m[0]));
}

export function checkQuotes(input: QuoteInput): QuoteVerdict {
  const rejected: string[] = [];
  const quotedPointers = new Set<string>();
  let unquoted = 0;
  for (const sentence of splitSentences(input.text)) {
    for (const m of sentence.matchAll(MARK)) {
      const [, sub, pointer, val] = m as unknown as [string, string, string, string];
      const key = `${sub}:${pointer}`;
      quotedPointers.add(key);
      const root = input.results.get(sub);
      const got = root === undefined ? { found: false } : pointerGet(root, pointer);
      if (!got.found || !matches(val, (got as { value?: unknown }).value)) rejected.push(key);
    }
    const values = outsideValues(sentence);
    if (values.length === 0 || isQuestion(sentence)) continue;
    const human = input.humanTexts ?? [];
    const cited = input.citedBodies ?? [];
    const exempt = values.every((v) => human.some((h) => h.includes(v))) || values.every((v) => cited.some((c) => c.includes(v)));
    if (!exempt) unquoted++;
  }
  const missing = requiredDumps(input.results, quotedPointers);
  if (rejected.length === 0 && missing.length === 0 && unquoted === 0) return { ok: true, text: input.text };
  return { ok: false, rejected: [...new Set(rejected)], missing, unquoted };
}

/** 直前の結果に dumps[] がある報告では dumps[].loaded の引用が必須(3.1.2.4)。 */
function requiredDumps(results: LastResults, quoted: ReadonlySet<string>): string[] {
  const restore = results.get("restore");
  const dumps = pointerGet(restore, "/result/dumps");
  if (!dumps.found || !Array.isArray(dumps.value)) return [];
  return dumps.value.map((_d, i) => `restore:/result/dumps/${i}/loaded`).filter((k) => !quoted.has(k));
}

/** 表示しなかった応答の扱い(D-0120): 次の入力の前に置く区画を 1 つにまとめ、取り出した時点で消費する。モデルは呼び直さない。 */
export class QuoteRejections {
  #rejected = new Set<string>();
  #missing = new Set<string>();
  #unquoted = 0;
  #pending = false;

  record(v: Extract<QuoteVerdict, { ok: false }>): void {
    v.rejected.forEach((r) => this.#rejected.add(r));
    v.missing.forEach((m) => this.#missing.add(m));
    this.#unquoted += v.unquoted;
    this.#pending = true;
  }

  /** 入力を渡す直前に呼ぶ。区画の文字列(無ければ null)。呼んだ時点で消費する。 */
  take(refs: PathRefTable): string | null {
    if (!this.#pending) return null;
    const section = appErrorSection("QUOTE_REJECTED", ["app", "quote-check"], refs, { rejected: [...this.#rejected], missing: [...this.#missing], unquoted: this.#unquoted });
    this.#rejected.clear();
    this.#missing.clear();
    this.#unquoted = 0;
    this.#pending = false;
    return section;
  }
}
