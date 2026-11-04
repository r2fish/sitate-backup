// U3c の画面の試験の道具: 画面のプロセスの renderer.ts を最小の DOM に読み込み、主プロセスの描画の入力を流す。
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { FakeDocument } from "./fake-dom.ts";
import type { FakeEl } from "./fake-dom.ts";

export const appRoot = join(import.meta.dirname, "..", "..");
export const NEW_HTML = join(appRoot, "src", "renderer", "index.html");
export const OLD_HTML = join(import.meta.dirname, "fixtures", "index-before.html");
export const NEW_RENDERER = join(appRoot, "src", "renderer", "renderer.ts");
export const OLD_RENDERER = join(import.meta.dirname, "fixtures", "renderer-before.ts");

export interface Mounted { readonly doc: FakeDocument; readonly calls: unknown[][]; render(m: unknown): void; run(script: string): unknown }
let seq = 0;

/** 描画のモジュールは document と sitate を呼び出しのたびにグローバルから引く。同時に 2 つ載せず、1 つずつ使い切る。 */
export async function mount(renderer: string, html: string): Promise<Mounted> {
  const doc = FakeDocument.fromHtml(html);
  const calls: unknown[][] = [];
  let cb: ((m: unknown) => void) | null = null;
  const api = new Proxy({ onRender: (f: (m: unknown) => void) => { cb = f; } } as Record<string, unknown>, {
    get: (t, k: string) => t[k] ?? ((...a: unknown[]) => { calls.push([k, ...a]); return Promise.resolve(); }),
  });
  Object.assign(globalThis, { document: doc, sitate: api });
  await import(`${pathToFileURL(renderer).href}?m=${seq++}`);
  return {
    doc, calls,
    render: (m) => { Object.assign(globalThis, { document: doc }); cb!(m); },
    run: (script) => { Object.assign(globalThis, { document: doc }); return new Function("document", `return (${script})`)(doc); },
  };
}

export const words = (s: string): string[] => s.split(/\s+/).filter(Boolean);
export type { FakeEl };
