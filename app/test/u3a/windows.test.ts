// 面の列挙(完了条件 28): 配布物のコードを静的に検査し、開く窓と最上位の画面を列挙する。
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { SURFACES } from "../../src/main/ipc-channels.ts";

const src = join(import.meta.dirname, "..", "..", "src");
const read = (...p: string[]): string => readFileSync(join(src, ...p), "utf8");
const code = (dir: string): [string, string][] => readdirSync(join(src, dir)).filter((f) => f.endsWith(".ts")).map((f) => [`${dir}/${f}`, read(dir, f)]);

test("最上位の画面は 5 面だけで、HTML の最上位の data-surface が列挙と一致する", () => {
  const html = read("renderer", "index.html");
  const found = [...html.matchAll(/data-surface="([^"]+)"/g)].map((m) => m[1]);
  assert.deepEqual([...found].sort(), [...SURFACES].sort());
  assert.equal(new Set(found).size, found.length);
});

test("開く窓はアプリ本体の 1 つだけ。ほかは OS の標準の窓(フォルダの選択・印刷)だけで、window.open と新しい窓の作成は拒む", () => {
  const files = [...code("main"), ...code("renderer")];
  const creators = files.filter(([, s]) => /new BrowserWindow\(|new BrowserView\(|new WebContentsView\(|window\.open\(/.test(s)).map(([f]) => f);
  assert.deepEqual(creators, ["main/windows.ts"]);
  assert.equal((read("main", "windows.ts").match(/new BrowserWindow\(/g) ?? []).length, 1);
  assert.match(read("main", "windows.ts"), /setWindowOpenHandler\(\(\) => \(\{ action: "deny" \}\)\)/);
  const dialogs = files.flatMap(([f, s]) => [...s.matchAll(/dialog\.(\w+)\(/g)].map((m) => `${f}:${m[1]}`));
  for (const d of dialogs) assert.ok(/showOpenDialog|showErrorBox|showSaveDialog/.test(d), d);
  for (const [f, s] of code("renderer")) assert.ok(!/(^|\W)(alert|confirm|prompt)\(/.test(s.replace(/\/\/.*$/gm, "")), `${f} が OS の標準の窓以外の窓を開く`);
});

test("印刷の描画面は JavaScript を切り、終わりに必ず閉じる", () => {
  const w = read("main", "windows.ts");
  assert.match(w, /javascript: false/);
  assert.match(w, /finally \{\s*view\.webContents\.close\(\);/);
});

test("レンダラは Node の権限を持たない(contextIsolation・sandbox)", () => {
  const w = read("main", "windows.ts");
  assert.match(w, /contextIsolation: true/);
  assert.match(w, /sandbox: true/);
  assert.match(w, /nodeIntegration: false/);
});
