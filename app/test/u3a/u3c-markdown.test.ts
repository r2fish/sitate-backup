// U3c エージェントの文の書式(5.14c の完了条件 2): 対象の書式が要素になり、HTML・リンク・画像は書かれたとおりの文字で描かれ、主プロセスが入れた値の範囲は書式の対象にならない。
import assert from "node:assert/strict";
import { test } from "node:test";
import { renderAgentText } from "../../src/main/agent-text.ts";
import { PathRefTable } from "../../src/main/path-ref.ts";
import { renderMarkdown } from "../../src/renderer/markdown.ts";
import type { MdDoc } from "../../src/renderer/markdown.ts";
import { FakeDocument } from "./fake-dom.ts";
import type { FakeEl } from "./fake-dom.ts";

const doc = new FakeDocument() as unknown as MdDoc & FakeDocument;
const md = (text: string, spans: readonly (readonly [number, number])[] = []): FakeEl[] => renderMarkdown(doc, text, spans) as unknown as FakeEl[];
const shape = (e: FakeEl | string): unknown => (typeof e === "string" ? e : e.children.length === 0 ? e.tag : { [e.tag]: e.children.map(shape) });
const all = (nodes: FakeEl[]): FakeEl[] => nodes.flatMap((n) => [n, ...n.descendants()]);
const text = (nodes: FakeEl[]): string => nodes.map((n) => n.textContent).join("");

test("[U3c-2] 対象の書式が要素になる: 段落・箇条書き・番号付き・太字・行内のコード・見出し・コードの塊", () => {
  const p = md("一つ目の段落\n続き\n\n二つ目");
  assert.deepEqual(p.map((e) => e.tag), ["p", "p"]);
  assert.equal(p[0]!.textContent, "一つ目の段落続き");
  assert.equal(p[0]!.children.filter((c) => typeof c !== "string" && c.tag === "br").length, 1, "段落の中の改行は br");
  const ul = md("- a\n* b");
  assert.equal(ul[0]!.tag, "ul");
  assert.deepEqual(ul[0]!.children.map((c) => (c as FakeEl).tag), ["li", "li"]);
  const ol = md("1. 一\n2. 二");
  assert.equal(ol[0]!.tag, "ol");
  assert.equal(ol[0]!.children.length, 2);
  const b = md("前 **太** 後");
  assert.ok(all(b).some((e) => e.tag === "strong" && e.textContent === "太"));
  assert.equal(text(b), "前 太 後");
  const c = md("前 `a**b` 後");
  assert.ok(all(c).some((e) => e.tag === "code" && e.textContent === "a**b"), "コードの中は書式にしない");
  for (const h of ["# 大", "## 中", "### 小"]) {
    const n = md(h)[0]!;
    assert.equal(n.getAttribute("data-md"), "heading");
    assert.ok(all([n]).some((e) => e.tag === "strong"), "見出しは太字の 1 行");
    assert.equal(n.textContent, h.replace(/^#+ /, ""));
  }
  const pre = md("```\nx <b>y</b>\n**z**\n```\n後");
  assert.deepEqual(pre.map((e) => e.tag), ["pre", "p"]);
  assert.equal(pre[0]!.textContent, "x <b>y</b>\n**z**", "コードの塊は書かれたとおり");
});

test("[U3c-2] <img onerror>・[a](javascript:)・![x](http://)・<script> は書かれたとおりの文字で描かれ、要素にならない", () => {
  for (const s of ['<img src=x onerror=alert(1)>', "[a](javascript:alert(1))", "![x](http://example.invalid/a.png)", "<script>alert(1)</script>", "<a href=\"x\">y</a>", "| a | b |\n|---|---|", "<b>**x**</b>"]) {
    const out = md(s);
    const tags = new Set(all(out).map((e) => e.tag));
    for (const bad of ["img", "a", "script", "b", "table", "iframe"]) assert.ok(!tags.has(bad), `${bad} が要素になった: ${s}`);
    for (const e of all(out)) assert.deepEqual([...e.attrs.keys()].filter((k) => k !== "data-md"), [], `属性: ${s}`);
    const expected = s.includes("**x**") ? "<b>x</b>" : s.replace(/\n/g, "");
    assert.equal(text(out), expected, s);
  }
});

test("[U3c-2] 主プロセスが入れた値の範囲は書式の対象にならない: 名前 /srv/**old** はそのまま描かれる", () => {
  const t = (k: string): string => k;
  const refs = new PathRefTable();
  const ref = refs.refFor("/srv/**old**/a**b");
  const r = renderAgentText(`対象は ${ref} と **太字** です。`, t, refs);
  assert.equal(r.text, "対象は /srv/**old**/a**b と **太字** です。");
  assert.equal(r.spans.length, 1);
  assert.equal(r.text.slice(r.spans[0]![0], r.spans[0]![1]), "/srv/**old**/a**b");
  const out = md(r.text, r.spans);
  const strongs = all(out).filter((e) => e.tag === "strong");
  assert.deepEqual(strongs.map((e) => e.textContent), ["太字"], "値の外の太字だけが要素になる");
  assert.equal(text(out), "対象は /srv/**old**/a**b と 太字 です。");
  // 範囲を渡さなければ名前の中の ** が書式になる(範囲の効果の対照)
  assert.ok(all(md(r.text)).filter((e) => e.tag === "strong").length >= 2);
  // 値が行頭にあっても見出し・箇条書きにならない
  const head = renderAgentText(`${refs.refFor("# x")}`, t, refs);
  assert.deepEqual(md(head.text, head.spans).map((e) => e.tag), ["p"]);
});

test("[U3c-2] 引用の印の値も範囲に入る: 印 [[…=値]] の値に ** があっても書式にならない", () => {
  const t = (k: string): string => k;
  const r = renderAgentText("最後は [[status:/result/x=**v**]] です", t, new PathRefTable());
  assert.equal(r.text, "最後は **v** です");
  assert.deepEqual(r.spans, [[4, 9]]);
  assert.ok(!all(md(r.text, r.spans)).some((e) => e.tag === "strong"));
});

test("[U3c-2] 番号付きの箇条書きは元の番号を保つ: 「3. a\\n4. b」は 3・4", () => {
  const ol = md("3. a\n4. b")[0]!;
  assert.equal(ol.tag, "ol");
  assert.equal(ol.getAttribute("start"), "3");
  assert.deepEqual(ol.children.map((c) => (c as FakeEl).getAttribute("value")), ["3", "4"]);
});

test("[U3c-2] 空行を挟んだ同じ種類の項目は 1 つのリストにつなぐ: 「1. a\\n\\n2. b」の 2 つ目は 2", () => {
  const out = md("1. a\n\n2. b");
  assert.equal(out.length, 1, "リストは 1 つ");
  const ol = out[0]!;
  assert.deepEqual(ol.children.map((c) => (c as FakeEl).getAttribute("value")), ["1", "2"]);
  assert.equal(md("- a\n\n- b").length, 1);
  assert.equal(md("- a\n\n1. b").length, 2, "種類が違えば分ける");
});
