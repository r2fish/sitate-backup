// エージェントのプロセスの起動・鍵の受け渡し・実行の制限(完了条件 1・17・25)。
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, realpathSync, writeFileSync, rmSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { EgressDeniedError, makeGuardedFetch } from "../../src/agent/guarded-fetch.ts";
import type { AgentToMain, MainToAgent } from "../../src/agent/protocol.ts";
import { AgentHost, agentExecArgv } from "../../src/main/agent-host.ts";
import type { ForkAgent, ForkedAgent } from "../../src/main/agent-host.ts";

const bundle = { manual: "m", procedure: "p", rules: "r", tools: [], models: ["claude-sonnet-5-5"] };

function fakeFork() {
  const forks: { entry: string; options: Parameters<ForkAgent>[1] }[] = [];
  const posted: MainToAgent[] = [];
  let onMsg: (m: AgentToMain) => void = () => undefined;
  let onExit: (c: number) => void = () => undefined;
  const proc: ForkedAgent = {
    postMessage: (m) => { posted.push(m); },
    on: ((ev: string, cb: never) => { if (ev === "message") onMsg = cb; else onExit = cb; }) as ForkedAgent["on"],
    kill: () => true,
  };
  const fork: ForkAgent = (entry, options) => { forks.push({ entry, options }); return proc; };
  return { fork, forks, posted, emit: (m: AgentToMain) => onMsg(m), exit: () => onExit(1) };
}

test("LLM API キーは起動後の init メッセージだけで渡され、引数・環境変数に現れない。画面のプロセスへ出る経路が無い", async () => {
  const f = fakeFork();
  const failures: unknown[] = [];
  const host = new AgentHost(f.fork, "/app/agent.js", { onReady: () => undefined, onReply: () => undefined, onFailure: (id, code) => failures.push(code), onUsage: () => undefined, onToolCall: async (n) => `result:${n}` });
  host.start("sk-ant-KEY-SENTINEL-0123456789", bundle, "ja", "2026-11-03");
  const opts = f.forks[0]!.options;
  assert.deepEqual(opts.args, []);
  assert.deepEqual(opts.env, {});
  assert.ok(!JSON.stringify(opts).includes("SENTINEL"));
  assert.equal(f.posted[0]!.type, "init");
  assert.ok(JSON.stringify(f.posted[0]).includes("SENTINEL"));
  f.emit({ type: "tool_call", callId: "c0", name: "sitate_version", input: {} });
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(f.posted[1], { type: "tool_result", callId: "c0", content: "result:sitate_version" });
  f.exit();
  assert.deepEqual(failures, ["AGENT_EXITED"]);
  assert.equal(host.running, false);
});

test("実行の制限: 権限モデルの下で、子プロセスの起動・ほかのファイルの読み取り・書き込みが失敗する", () => {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), "u3a-perm-")));
  try {
    mkdirSync(join(dir, "bundle"));
    const probe = join(dir, "bundle", "probe.mjs");
    const other = join(dir, "other.txt");
    writeFileSync(other, "x");
    writeFileSync(probe, `
      import { readFileSync, writeFileSync } from "node:fs";
      import { spawnSync } from "node:child_process";
      const r = {};
      for (const [k, f] of Object.entries({
        spawn: () => { const x = spawnSync("/bin/echo", ["hi"]); if (x.error) throw x.error; },
        read: () => readFileSync(${JSON.stringify(other)}),
        write: () => writeFileSync(${JSON.stringify(join(dir, "w.txt"))}, "y"),
      })) { try { f(); r[k] = "allowed"; } catch (e) { r[k] = e.code ?? "error"; } }
      console.log(JSON.stringify(r));`);
    const out = spawnSync(process.execPath, [...agentExecArgv(probe), probe], { encoding: "utf8", env: {} });
    assert.ok(out.stdout.trim() !== "", out.stderr);
    const r = JSON.parse(out.stdout.trim().split("\n").pop()!) as Record<string, string>;
    assert.deepEqual(r, { spawn: "ERR_ACCESS_DENIED", read: "ERR_ACCESS_DENIED", write: "ERR_ACCESS_DENIED" });
  } finally { rmSync(dir, { recursive: true, force: true }); }
});

test("LLM API 以外への送信は拒まれる(許可した origin だけ通る)", async () => {
  const server = createServer((_q, res) => res.end("ok"));
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  const port = (server.address() as { port: number }).port;
  try {
    const g = makeGuardedFetch(fetch, `http://127.0.0.1:${port}`);
    assert.equal(await (await g(`http://127.0.0.1:${port}/x`)).text(), "ok");
    await assert.rejects(g(`http://127.0.0.1:${port + 1}/x`), EgressDeniedError);
    await assert.rejects(g("https://example.invalid/"), EgressDeniedError);
    await assert.rejects(g("not a url"), EgressDeniedError);
  } finally { server.close(); }
});
