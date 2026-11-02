// 試験用ドライバ(完了条件 12): 5 面の操作の命令が画面の操作になり、往復で結果が返る。製品のビルドにドライバが入らないことは build-app.mjs の検査と下の静的検査。
import assert from "node:assert/strict";
import { mkdtempSync, readdirSync, readFileSync, realpathSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { Driver, SURFACES } from "../e2e/driver.ts";
import { scriptFor, startDriverServer } from "../../src/testing/driver-server.ts";
import { SURFACES as APP_SURFACES } from "../../src/main/ipc-channels.ts";

test("ドライバは 5 面を操作できる(命令 → 画面の操作 → 結果)", async () => {
  assert.deepEqual([...SURFACES], [...APP_SURFACES]);
  const dir = realpathSync(mkdtempSync(join(tmpdir(), "u3a-drv-")));
  const evals: string[] = [];
  let lang = "";
  let d: Driver | null = null;
  const server = startDriverServer(join(dir, "d.sock"), { eval: async (s: string) => { evals.push(s); return s.includes("innerText") ? "面の文" : true; } }, (l: string) => { lang = l; });
  try {
    await new Promise((r) => setTimeout(r, 50));
    d = await Driver.connect(join(dir, "d.sock"));
    await d.chat("こんにちは");
    for (const s of SURFACES) assert.equal(await d.read(s), "面の文");
    assert.equal(await d.click("approval-card", "approve"), true);
    assert.equal(await d.fill("approval-card", "a@example.test", "ocean rapid tiger"), true);
    assert.equal(await d.click("phrase-panel", "verify"), true);
    await d.setLanguage("en");
    assert.equal(lang, "en");
    assert.ok(evals.some((e) => e.includes("chat-input") && e.includes("こんにちは")));
    assert.ok(evals.some((e) => e.includes('data-surface="approval-card"') && e.includes('data-action="approve"')));
  } finally { d?.close(); server.close(); rmSync(dir, { recursive: true, force: true }); }
  assert.ok(scriptFor({ op: "read", surface: "status" }).includes("innerText"));
});

test("主プロセスと画面の本体のソースはドライバのモジュールを import しない(製品のビルドに入らない)", () => {
  const root = join(import.meta.dirname, "..", "..", "src");
  for (const dir of ["main", "renderer", "agent"]) {
    for (const f of readdirSync(join(root, dir)).filter((x) => x.endsWith(".ts"))) {
      const text = readFileSync(join(root, dir, f), "utf8");
      // 唯一の例外: 主プロセスの入口の `if (__TEST_BUILD__)` の枝(製品のビルドでは束ねられない。build.test.ts が成果物で確かめる)。
      const allowed = `${dir}/${f}` === "main/index.ts" ? text.replace(/if \(__TEST_BUILD__\) \(await import\("\.\.\/testing\/attach\.ts"\)\)[^\n]*\n/, "") : text;
      assert.ok(!/testing\/|e2e\//.test(allowed), `${dir}/${f}`);
    }
  }
});
