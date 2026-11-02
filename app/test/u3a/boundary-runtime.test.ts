import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { join } from "node:path";
import { readFileSync } from "node:fs";

// 完了条件 1・2・25 の補足: SDK のルートの import は静的には agent-toolset と child_process を引くが、実行時にそれらが評価される経路があるかを確かめる。
const here = import.meta.dirname;
const run = (env: Record<string, string> = {}) => spawnSync(process.execPath, ["--import", join(here, "load-hook.mjs"), join(here, "load-agent.ts")], { encoding: "utf8", env: { PATH: process.env.PATH ?? "", ...env } });

test("boundary runtime: running the agent path never evaluates internal/node (child_process, fs) or any agent-toolset / memory toolset module that does work", () => {
  const r = run();
  assert.match(r.stdout, /DONE/, r.stderr);
  const loaded = r.stdout.split("\n").filter((l) => l.startsWith("LOADED"));
  // sync-interval は定数と検査だけの補助(fs・child_process に触れない)。それ以外は 1 つも評価されない。
  assert.deepEqual(loaded.filter((l) => !/agent-toolset\/sync-interval\.mjs$/.test(l)), []);
});

test("boundary runtime: the client is built with the API key and the default endpoint given explicitly, so the environment cannot redirect it", () => {
  const src = readFileSync(join(here, "..", "..", "src", "agent", "entry.ts"), "utf8");
  assert.match(src, /new Anthropic\(\{ apiKey: msg\.apiKey, baseURL: LLM_API_BASE_URL, fetch: withUsageReport\(makeGuardedFetch\(fetch\), .*\) \}\)/);
  const r = spawnSync(process.execPath, ["-e", `
    import("${join(here, "..", "..", "node_modules", "@anthropic-ai", "sdk", "index.mjs")}").then(async ({ default: A }) => {
      let url = ""; const c = new A({ apiKey: "k", baseURL: "https://api.anthropic.com", maxRetries: 0, fetch: async (u) => { url = String(u); return new Response("{}", { status: 200, headers: { "content-type": "application/json" } }); } });
      await c.models.retrieve("m"); console.log(url);
    });`], { encoding: "utf8", env: { PATH: process.env.PATH ?? "", ANTHROPIC_BASE_URL: "https://evil.invalid" } });
  assert.match(r.stdout, /^https:\/\/api\.anthropic\.com\//, r.stdout + r.stderr);
});
