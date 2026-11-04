// 画面のプロセスの試験用の最小の DOM(U3c)。index.html を読んで木を作り、renderer が使う API だけを持つ。実際の配置(CSS)は持たない。
// innerText は「ブロックの要素は改行、インラインの要素(span)は空白」で近似する(実機の配置との差は、語の列で比べて吸収する)。
import { readFileSync } from "node:fs";

const VOID = new Set(["meta", "input", "link", "br"]);
const INLINE = new Set(["span", "strong", "code", "button", "label", "input"]);

export class FakeEl {
  readonly tag: string;
  attrs = new Map<string, string>();
  children: (FakeEl | string)[] = [];
  parent: FakeEl | null = null;
  listeners = new Map<string, ((e: unknown) => void)[]>();
  hidden = false;
  open = false;
  disabled = false;
  value = "";
  scrollTop = 0;
  scrollHeight = 0;
  onclick: (() => void) | null = null;
  constructor(tag: string) { this.tag = tag; }
  get id(): string { return this.attrs.get("id") ?? ""; }
  setAttribute(k: string, v: string): void { this.attrs.set(k, v); }
  getAttribute(k: string): string | null { return this.attrs.get(k) ?? null; }
  addEventListener(t: string, cb: (e: unknown) => void): void { this.listeners.set(t, [...(this.listeners.get(t) ?? []), cb]); }
  fire(t: string, e: unknown = { preventDefault() {} }): void { for (const cb of this.listeners.get(t) ?? []) cb(e); }
  click(): void { this.fire("click"); this.onclick?.(); }
  showModal(): void { this.open = true; }
  close(): void { this.open = false; this.fire("close"); }
  requestSubmit(): void { this.fire("submit"); }
  #adopt(nodes: (FakeEl | string)[]): (FakeEl | string)[] { return nodes.map((n) => { if (typeof n !== "string") { n.parent?.children.splice(n.parent.children.indexOf(n), 1); n.parent = this; } return n; }); }
  append(...nodes: (FakeEl | string)[]): void { this.children.push(...this.#adopt(nodes)); }
  prepend(...nodes: (FakeEl | string)[]): void { this.children.unshift(...this.#adopt(nodes)); }
  replaceChildren(...nodes: (FakeEl | string)[]): void { for (const c of this.children) if (typeof c !== "string") c.parent = null; this.children = []; this.append(...nodes); }
  get textContent(): string { return this.children.map((c) => (typeof c === "string" ? c : c.textContent)).join(""); }
  set textContent(v: string | null) { this.children = (v ?? "") === "" ? [] : [v!]; }
  get innerText(): string {
    const parts = this.children.map((c) => (typeof c === "string" ? c : c.innerText)).filter((x) => x !== "");
    const kids = this.children.filter((c): c is FakeEl => typeof c !== "string");
    return parts.join(kids.length > 0 && kids.every((k) => INLINE.has(k.tag)) ? " " : "\n");
  }
  descendants(): FakeEl[] { return this.children.flatMap((c) => (typeof c === "string" ? [] : [c, ...c.descendants()])); }
  querySelectorAll(sel: string): FakeEl[] { return query(this, sel); }
  querySelector(sel: string): FakeEl | null { return query(this, sel)[0] ?? null; }
  /** 試験の確認用: 要素の木を簡単な文字列にする。 */
  tags(): string[] { return this.descendants().map((d) => d.tag); }
}

function matchesCompound(e: FakeEl, c: string): boolean {
  const re = /(#[\w-]+)|(\[[^\]]+\])|([a-z0-9]+)|(\.[\w-]+)/g;
  for (const m of c.matchAll(re)) {
    const t = m[0];
    if (t.startsWith("#")) { if (e.id !== t.slice(1)) return false; }
    else if (t.startsWith(".")) { if (!(e.getAttribute("class") ?? "").split(" ").includes(t.slice(1))) return false; }
    else if (t.startsWith("[")) {
      const mm = /^\[([\w-]+)(?:=(?:"([^"]*)"|'([^']*)'|([^\]]*)))?\]$/.exec(t)!;
      const v = e.getAttribute(mm[1]!);
      const want = mm[2] ?? mm[3] ?? mm[4];
      if (v === null || (want !== undefined && v !== want)) return false;
    } else if (e.tag !== t) return false;
  }
  return true;
}

function query(root: FakeEl, selector: string): FakeEl[] {
  const out = new Set<FakeEl>();
  for (const one of selector.split(",").map((s) => s.trim())) {
    const toks = one.replace(/>/g, " > ").split(/\s+/).filter(Boolean);
    const test = (e: FakeEl, i: number): boolean => {
      if (i < 0) return true;
      const t = toks[i]!;
      if (t === ">") return false;
      if (!matchesCompound(e, t)) return false;
      if (i === 0) return true;
      if (toks[i - 1] === ">") { return e.parent !== null && e.parent !== undefined && test(e.parent, i - 2); }
      for (let p = e.parent; p !== null; p = p.parent) if (test(p, i - 1)) return true;
      return false;
    };
    for (const d of root.descendants()) if (test(d, toks.length - 1)) out.add(d);
  }
  return [...out];
}

export class FakeDocument {
  readonly body = new FakeEl("body");
  createElement(tag: string): FakeEl { return new FakeEl(tag); }
  getElementById(id: string): FakeEl | null { return this.body.descendants().find((d) => d.id === id) ?? null; }
  querySelectorAll(sel: string): FakeEl[] { return this.body.querySelectorAll(sel); }
  querySelector(sel: string): FakeEl | null { return this.body.querySelector(sel); }
  /** index.html の body を木にする。 */
  static fromHtml(path: string): FakeDocument {
    const doc = new FakeDocument();
    const html = readFileSync(path, "utf8");
    const body = html.slice(html.indexOf("<body>") + 6, html.indexOf("</body>")).replace(/<script[\s\S]*?<\/script>/g, "");
    const stack: FakeEl[] = [doc.body];
    for (const m of body.matchAll(/<(\/?)([a-z0-9]+)([^>]*)>/g)) {
      const [, close, tag, rest] = m as unknown as [string, string, string, string];
      if (close === "/") { stack.pop(); continue; }
      const e = new FakeEl(tag);
      for (const a of rest.matchAll(/([\w-]+)(?:="([^"]*)")?/g)) e.setAttribute(a[1]!, a[2] ?? "");
      if (e.attrs.has("hidden")) e.hidden = true;
      stack[stack.length - 1]!.append(e);
      if (!VOID.has(tag)) stack.push(e);
    }
    return doc;
  }
}
