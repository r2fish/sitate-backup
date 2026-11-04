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

interface Cur { readonly doc: MdDoc; readonly lines: Line[]; readonly prot: (i: number) => boolean; k: number }
const marker = (c: Cur, l: Line, re: RegExp): RegExpExecArray | null => (c.prot(l.at) ? null : re.exec(l.text));
const isFence = (c: Cur, l: Line): boolean => !c.prot(l.at) && l.text.startsWith("```");

function codeBlock(c: Cur): MdNode {
  const body: string[] = [];
  c.k++;
  while (c.k < c.lines.length && !isFence(c, c.lines[c.k]!)) { body.push(c.lines[c.k]!.text); c.k++; }
  c.k++; // 閉じの ```(無ければ最後まで)
  const pre = c.doc.createElement("pre");
  pre.append(el(c.doc, "code", body.join("\n")));
  return pre;
}

function heading(c: Cur, l: Line, h: RegExpExecArray): MdNode {
  const p = el(c.doc, "p", undefined, { "data-md": "heading" });
  const strong = c.doc.createElement("strong");
  strong.append(...inline(c.doc, l.text.slice(h[0].length), l.at + h[0].length, c.prot));
  p.append(strong);
  c.k++;
  return p;
}

function list(c: Cur, ordered: boolean): MdNode {
  const ul = c.doc.createElement(ordered ? "ol" : "ul");
  let firstNo: string | null = null;
  while (c.k < c.lines.length) {
    // 空行を挟んだ同じ種類の項目は 1 つのリストにつなぐ。
    let j = c.k;
    while (j < c.lines.length && c.lines[j]!.text.trim() === "") j++;
    const m = j < c.lines.length ? marker(c, c.lines[j]!, ordered ? ORDERED : BULLET) : null;
    if (m === null) break;
    c.k = j;
    const li = c.doc.createElement("li");
    if (ordered) {
      // 元の番号を保つ(最初の数を ol の start に、各項目の数を li の value に入れる)。
      const no = /^\d+/.exec(m[0])![0];
      if (firstNo === null) { firstNo = no; ul.setAttribute("start", no); }
      li.setAttribute("value", no);
    }
    const l = c.lines[c.k]!;
    li.append(...inline(c.doc, l.text.slice(m[0].length), l.at + m[0].length, c.prot));
    ul.append(li);
    c.k++;
  }
  return ul;
}

/** 段落: 空行・ブロックの始まりまで。行は改行のまま保つ(br)。 */
function paragraph(c: Cur): MdNode {
  const p = c.doc.createElement("p");
  let first = true;
  while (c.k < c.lines.length) {
    const l = c.lines[c.k]!;
    const blockStart = marker(c, l, HEADING) !== null || marker(c, l, BULLET) !== null || marker(c, l, ORDERED) !== null || isFence(c, l);
    if (l.text.trim() === "" || (!first && blockStart)) break;
    if (!first) p.append(c.doc.createElement("br"));
    p.append(...inline(c.doc, l.text, l.at, c.prot));
    first = false;
    c.k++;
  }
  return p;
}

export function renderMarkdown(doc: MdDoc, text: string, spans: readonly Span[] = []): MdNode[] {
  const c: Cur = { doc, lines: splitLines(text), prot: (i) => spans.some(([a, b]) => i >= a && i < b), k: 0 };
  const out: MdNode[] = [];
  while (c.k < c.lines.length) {
    const l = c.lines[c.k]!;
    if (l.text.trim() === "") { c.k++; continue; }
    const h = marker(c, l, HEADING);
    if (isFence(c, l)) out.push(codeBlock(c));
    else if (h !== null) out.push(heading(c, l, h));
    else if (marker(c, l, BULLET) !== null || marker(c, l, ORDERED) !== null) out.push(list(c, ORDERED.test(l.text)));
    else out.push(paragraph(c));
  }
  return out;
}
