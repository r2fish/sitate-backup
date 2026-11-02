// 復旧キットの描画部(spec 4.7)。`kit` の fields + 24 語 + 言語 → 印刷物(HTML 1 文書と配置の構造)。
// 24 語は引数のメモリだけで扱い、ログ・ファイル・例外の文に出さない。収まらないときは HTML を作らず失敗を返す。
import type { KitFields, KitRenderResult, RenderKit } from "./types.ts";
import { backBlocks } from "./content-back.ts";
import { frontBlocks } from "./content-front.ts";
import { renderHtml } from "./html.ts";
import { placeAll } from "./layout.ts";

const PHRASE_WORDS = 24;

/** 値を最小の形にする(文字列は 1 字、構造は保つ)。 */
function shrink(v: unknown): unknown {
  if (typeof v === "string") return "x";
  if (Array.isArray(v)) return v.map(shrink);
  if (v !== null && typeof v === "object") return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, shrink(x)]));
  return v;
}

function fits(f: KitFields, phrase: readonly string[]): boolean {
  return placeAll([...frontBlocks(f, phrase), ...backBlocks(f)]) !== undefined;
}

/** 収まらなかったときに名指しする欄: その欄を最小にすると収まるもののうち、値が最も長い欄。どれでも収まらなければ最も長い欄。 */
function culprit(f: KitFields, phrase: readonly string[]): keyof KitFields {
  const keys = (Object.keys(f) as (keyof KitFields)[]).sort((a, b) => JSON.stringify(f[b]).length - JSON.stringify(f[a]).length);
  return keys.find((k) => fits({ ...f, [k]: shrink(f[k]) } as KitFields, phrase)) ?? keys[0];
}

export const renderKit: RenderKit = (input): KitRenderResult => {
  if (input.lang !== "ja") return { ok: false, reason: "lang_unsupported" };
  if (input.phrase.length !== PHRASE_WORDS) throw new Error("フレーズの語数が 24 ではない");
  const specs = [...frontBlocks(input.fields, input.phrase), ...backBlocks(input.fields)];
  const placed = placeAll(specs);
  if (placed === undefined) return { ok: false, reason: "does_not_fit", field: culprit(input.fields, input.phrase) };
  return { ok: true, printable: { html: renderHtml(placed, input.lang), layout: { paper: "A4-portrait", blocks: placed.map((p) => p.block) } } };
};
