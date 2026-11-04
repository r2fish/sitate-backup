// 画面に描くエージェントの文(spec 3.7「エージェントの文の書式」)。引用の印の値と参照名から戻した名前を入れ、その範囲(UTF-16 の位置の対)を返す。
// 範囲は画面で書式の対象にしない(名前の中の `**` などで見え方が変わらないようにする)。
import type { PathRefTable } from "./path-ref.ts";
import { renderMarksParts } from "./quote-render.ts";

export type Span = readonly [number, number];
export interface AgentText { readonly text: string; readonly spans: readonly Span[] }

const REF = /pr:[0-9a-f]{16}/g;

export function renderAgentText(text: string, t: (key: string) => string, refs: PathRefTable, timeZone?: string): AgentText {
  let out = "";
  const spans: [number, number][] = [];
  const put = (s: string, inserted: boolean): void => {
    if (inserted && s !== "") spans.push([out.length, out.length + s.length]);
    out += s;
  };
  for (const part of renderMarksParts(text, t, timeZone)) {
    if (part.value) { put(refs.renderText(part.text), true); continue; }
    let at = 0;
    for (const m of part.text.matchAll(REF)) {
      put(part.text.slice(at, m.index), false);
      const real = refs.renderText(m[0]);
      put(real, real !== m[0]);
      at = m.index + m[0].length;
    }
    put(part.text.slice(at), false);
  }
  return { text: out, spans };
}
