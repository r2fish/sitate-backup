import { test } from "node:test";
import assert from "node:assert/strict";
import { ESLint } from "eslint";
import { appRoot } from "./helpers.ts";

// コードの大きさの検査(作業指示書 1 章)が、ファイル内の印では外せないことと、.tsx も対象になることを、設定そのものに文を通して確かめる。
const lines = (n: number) => Array.from({ length: n }, (_, i) => `const v${i} = ${i};`).join("\n") + "\n";
const longFn = "export function f() {\n" + Array.from({ length: 80 }, (_, i) => `  const x${i} = ${i};`).join("\n") + "\n}\n";

async function rules(filePath: string, code: string): Promise<string[]> {
  const eslint = new ESLint({ cwd: appRoot });
  const [r] = await eslint.lintText(code, { filePath });
  return r.messages.filter((m) => m.severity === 2).map((m) => m.ruleId ?? "");
}

test("eslint size: an inline disable comment does not lift the file or function limit", async () => {
  assert.ok((await rules("src/x.ts", "/* eslint-disable */\n" + lines(500))).includes("max-lines"));
  assert.ok((await rules("src/x.ts", "// eslint-disable-next-line max-lines-per-function\n" + longFn)).includes("max-lines-per-function"));
});

test("eslint size: .tsx and .mts files are checked, and a short file passes", async () => {
  assert.ok((await rules("src/x.tsx", lines(500))).includes("max-lines"));
  assert.ok((await rules("src/x.mts", lines(500))).includes("max-lines"));
  assert.deepEqual(await rules("src/x.ts", lines(10)), []);
});
