// 完了条件 2: 道具は betaTool(JSON Schema)で読み込み、Zod の補助を使わない(依存にも入れない)。
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";

const srcDir = new URL("../../src/", import.meta.url).pathname;
const walk = (d: string): string[] => readdirSync(d, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? walk(join(d, e.name)) : e.name.endsWith(".ts") ? [join(d, e.name)] : []));

test("agent runtime: tools come from betaTool of the JSON Schema helper and no source imports zod", () => {
  const rt = readFileSync(join(srcDir, "agent", "runtime.ts"), "utf8");
  assert.match(rt, /import \{ betaTool \} from "@anthropic-ai\/sdk\/helpers\/beta\/json-schema"/);
  assert.ok(!/helpers\/beta\/zod/.test(rt));
  for (const f of walk(srcDir)) assert.ok(!/from "(zod|@anthropic-ai\/sdk\/helpers\/beta\/zod)/.test(readFileSync(f, "utf8")), f);
  const pkg = JSON.parse(readFileSync(new URL("../../package.json", import.meta.url), "utf8")) as { dependencies?: Record<string, string> };
  assert.ok(!("zod" in (pkg.dependencies ?? {})));
});
