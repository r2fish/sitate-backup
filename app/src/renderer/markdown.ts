// エージェントの文の書式(spec 3.7「エージェントの文の書式」)。Markdown の一部だけを、createElement と textContent だけで描く。HTML として解釈しない。
// 対象: 段落(空行で区切る)、箇条書き(行頭の `- `・`* `・`1. `)、太字(`**…**`)、行内のコード、見出し(行頭の `#`〜`###`。太字の 1 行)、コードの塊(```)。
// リンク・画像・表・HTML の札は解釈せず、書かれたとおりの文字で描く。spans(主プロセスが文に入れた値の範囲)は書式の対象にしない。

/** 使う DOM の部分(試験では差し替える)。 */
export interface MdNode { textContent: string | null; setAttribute(name: string, value: string): void; append(...nodes: MdNode[]): void }
export interface MdDoc { createElement(tag: string): MdNode }

type Span = readonly [number, number];

function el(doc: MdDoc, tag: string, text?: string, attrs: Record<string, string> = {}): MdNode {
  const e = doc.createElement(tag);
  if (text !== undefined) e.textContent = text;
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v);
  return e;
}

interface Line { readonly text: string; readonly at: number }

function splitLines(text: string): Line[] {
  const out: Line[] = [];
  let at = 0;
  for (const raw of text.split("\n")) { out.push({ text: raw, at }); at += raw.length + 1; }
  return out;
}

/** 行内の書式: 太字と行内のコード。印(`**`・`` ` ``)が値の範囲の外にあるときだけ解釈する。 */
function inline(doc: MdDoc, s: string, base: number, prot: (i: number) => boolean): MdNode[] {
  const out: MdNode[] = [];
  let plain = "";
  const flush = (): void => { if (plain !== "") { out.push(el(doc, "span", plain)); plain = ""; } };
  let i = 0;
  while (i < s.length) {
    if (!prot(base + i) && s.startsWith("**", i) && !prot(base + i + 1)) {
      const j = s.indexOf("**", i + 2);
      if (j > i + 2 && !prot(base + j) && !prot(base + j + 1)) {
        flush();
        const b = doc.createElement("strong");
        b.append(...inline(doc, s.slice(i + 2, j), base + i + 2, prot));
        out.push(b);
        i = j + 2;
        continue;
      }
    }
    if (!prot(base + i) && s[i] === "`") {
      const j = s.indexOf("`", i + 1);
      if (j > i + 1 && !prot(base + j)) { flush(); out.push(el(doc, "code", s.slice(i + 1, j))); i = j + 1; continue; }
    }
    plain += s[i]!;
    i++;
  }
  flush();
  return out;
}

const BULLET = /^([-*]) /;
const ORDERED = /^\d+\. /;
const HEADING = /^(#{1,3}) /;

export function renderMarkdown(doc: MdDoc, text: string, spans: readonly Span[] = []): MdNode[] {
  const prot = (i: number): boolean => spans.some(([a, b]) => i >= a && i < b);
  const lines = splitLines(text);
  const out: MdNode[] = [];
  let k = 0;
  const startsMarker = (l: Line, re: RegExp): RegExpExecArray | null => (prot(l.at) ? null : re.exec(l.text));
  while (k < lines.length) {
    const l = lines[k]!;
    if (l.text.trim() === "") { k++; continue; }
    if (!prot(l.at) && l.text.startsWith("```")) {
      const body: string[] = [];
      k++;
      while (k < lines.length && !(!prot(lines[k]!.at) && lines[k]!.text.startsWith("```"))) { body.push(lines[k]!.text); k++; }
      k++; // 閉じの ```(無ければ最後まで)
      const pre = doc.createElement("pre");
      pre.append(el(doc, "code", body.join("\n")));
      out.push(pre);
      continue;
    }
    const h = startsMarker(l, HEADING);
    if (h !== null) {
      const p = el(doc, "p", undefined, { "data-md": "heading" });
      const strong = doc.createElement("strong");
      strong.append(...inline(doc, l.text.slice(h[0].length), l.at + h[0].length, prot));
      p.append(strong);
      out.push(p);
      k++;
      continue;
    }
    const first = startsMarker(l, BULLET) ?? startsMarker(l, ORDERED);
    if (first !== null) {
      const ordered = ORDERED.test(l.text);
      const list = doc.createElement(ordered ? "ol" : "ul");
      while (k < lines.length) {
        const m = startsMarker(lines[k]!, ordered ? ORDERED : BULLET);
        if (m === null) break;
        const li = doc.createElement("li");
        li.append(...inline(doc, lines[k]!.text.slice(m[0].length), lines[k]!.at + m[0].length, prot));
        list.append(li);
        k++;
      }
      out.push(list);
      continue;
    }
    // 段落: 空行・ブロックの始まりまで。行は改行のまま保つ(CSS の white-space)。
    const p = doc.createElement("p");
    let first_ = true;
    while (k < lines.length) {
      const c = lines[k]!;
      if (c.text.trim() === "" || (!first_ && (startsMarker(c, HEADING) !== null || startsMarker(c, BULLET) !== null || startsMarker(c, ORDERED) !== null || (!prot(c.at) && c.text.startsWith("```"))))) break;
      if (!first_) p.append(doc.createElement("br"));
      p.append(...inline(doc, c.text, c.at, prot));
      first_ = false;
      k++;
    }
    out.push(p);
  }
  return out;
}
