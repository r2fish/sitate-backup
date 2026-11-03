import { test } from "node:test";
import assert from "node:assert/strict";
import { createAgentRuntime, AgentFailure } from "../../src/agent/runtime.ts";
import { buildSystemBlocks } from "../../src/agent/system-prompt.ts";
import { ToolRegistry } from "../../src/main/tool-registry.ts";
import { fakeLlm } from "./fake-llm.ts";
import { join } from "node:path";

// AgentRuntime(作業指示書 5.14 の完了条件 2): 往復、中断、人の入力待ち、道具の往復の上限、失敗のコード。本物の SDK の Tool Runner に偽の fetch を差し込んで動かす。
const registry = ToolRegistry.fromDir(join(import.meta.dirname, "..", "..", "src", "tools"));
const bundle = { manual: "MANUAL", procedure: "PROCEDURE", rules: "RULES", tools: registry.definitions(), models: ["claude-sonnet-5-5"] };
const system = buildSystemBlocks(bundle, { locale: "ja", date: "2026-11-02", model: "claude-sonnet-5-5" });

test("runtime: a tool call goes to the main process callback and the envelope returns to the model, then the text comes back", async () => {
  const llm = fakeLlm([{ toolUses: [{ name: "sitate_status", input: {} }] }, { text: "確認しました。" }]);
  const calls: { name: string; input: unknown }[] = [];
  const rt = createAgentRuntime({ client: llm.client, model: "claude-sonnet-5-5", system, tools: bundle.tools, onToolCall: async (c) => { calls.push({ name: c.name, input: c.input }); return "ENVELOPE-RESULT"; } });
  assert.equal(await rt.run("状態は?"), "確認しました。");
  assert.deepEqual(calls, [{ name: "sitate_status", input: {} }]);
  assert.equal(llm.requests.length, 2);
  assert.match(llm.requests[1]!, /ENVELOPE-RESULT/);
});

test("runtime: the second input carries the history, and tools are exactly the definitions given", async () => {
  const llm = fakeLlm([{ text: "一つ目" }, { text: "二つ目" }]);
  const rt = createAgentRuntime({ client: llm.client, model: "claude-sonnet-5-5", system, tools: bundle.tools, onToolCall: async () => "x" });
  await rt.run("first-input");
  await rt.run("second-input");
  const second = JSON.parse(llm.requests[1]!);
  assert.equal(second.messages.length, 3);
  assert.match(JSON.stringify(second.messages[0]), /first-input/);
  assert.deepEqual(second.tools.map((t: any) => t.name).sort(), registry.names().sort());
  assert.ok(!second.tools.some((t: any) => /bash|str_replace|memory|computer/.test(t.name)), "agent-toolset の道具が無い");
});

test("runtime: the system prompt is manual, procedure, rules in this order with the cache break after the rules, volatile values after", () => {
  assert.deepEqual(system.map((b) => b.text.split("\n")[0]), ["MANUAL", "PROCEDURE", "RULES", "表示言語: ja"]);
  assert.deepEqual(system.map((b) => b.cache_control !== undefined), [false, false, true, false]);
  assert.ok(!registry.names().some((n) => /manual|procedure|rules/.test(n)), "同梱物を読む道具が無い");
});

test("runtime: the callback waits for a human operation; the model is not called while it waits", async () => {
  const llm = fakeLlm([{ toolUses: [{ name: "sitate_propose", input: { kind: "key-rotate" } }] }, { text: "done" }]);
  let release: (v: string) => void = () => {};
  const rt = createAgentRuntime({ client: llm.client, model: "m", system, tools: bundle.tools, onToolCall: () => new Promise<string>((r) => { release = r; }) });
  const p = rt.run("go");
  await new Promise((r) => setTimeout(r, 50));
  assert.equal(llm.requests.length, 1, "人の操作の待ちの間にモデルを呼ばない");
  release("APPROVED");
  assert.equal(await p, "done");
});

test("runtime: the round trip limit ends the turn with AGENT_TURN_LIMIT", async () => {
  const llm = fakeLlm(() => ({ toolUses: [{ name: "sitate_status", input: {} }] }));
  const rt = createAgentRuntime({ client: llm.client, model: "m", system, tools: bundle.tools, roundtripMax: 3, onToolCall: async () => "x" });
  await assert.rejects(rt.run("loop"), (e: unknown) => e instanceof AgentFailure && e.code === "AGENT_TURN_LIMIT");
  assert.ok(llm.requests.length <= 5, `要求が増え続けない: ${llm.requests.length}`);
});

test("runtime: abort stops the next tool call but lets the running callback finish", async () => {
  const llm = fakeLlm(() => ({ toolUses: [{ name: "sitate_status", input: {} }] }));
  let finished = false;
  let rt: ReturnType<typeof createAgentRuntime>;
  let n = 0;
  rt = createAgentRuntime({ client: llm.client, model: "m", system, tools: bundle.tools, onToolCall: async () => { n += 1; rt.abort(); await new Promise((r) => setTimeout(r, 20)); finished = true; return "x"; } });
  await assert.rejects(rt.run("go"), (e: unknown) => e instanceof AgentFailure && e.code === "AGENT_ABORTED");
  assert.equal(n, 1);
  assert.ok(finished, "実行中のコールバックは終わるまで待つ");
});

test("runtime: a reply cut by max_tokens is not used and not resent", async () => {
  const llm = fakeLlm([{ text: "途中で切れ", stop: "max_tokens" }]);
  const rt = createAgentRuntime({ client: llm.client, model: "m", system, tools: bundle.tools, onToolCall: async () => "x" });
  await assert.rejects(rt.run("go"), (e: unknown) => e instanceof AgentFailure && e.code === "AGENT_OUTPUT_TRUNCATED");
  assert.equal(llm.requests.length, 1);
});

test("[mp-a2] runtime: a reply with no content is not kept in the history, so the next request is valid", async () => {
  const llm = fakeLlm([{}, { text: "二つ目" }]);
  const rt = createAgentRuntime({ client: llm.client, model: "m", system, tools: bundle.tools, onToolCall: async () => "x" });
  assert.equal(await rt.run("first-input"), "");
  assert.equal(await rt.run("second-input"), "二つ目");
  const second = JSON.parse(llm.requests[1]!);
  assert.ok(!second.messages.some((m: any) => m.role === "assistant" && m.content.length === 0));
  assert.match(JSON.stringify(second.messages), /first-input/);
});

test("[mp-a2] runtime: after AGENT_TURN_LIMIT the earlier round trips stay in the history and the next request is valid", async () => {
  let phase = 0;
  const llm = fakeLlm(() => (phase === 0 ? { toolUses: [{ name: "sitate_status", input: {} }] } : { text: "続き" }));
  const rt = createAgentRuntime({ client: llm.client, model: "m", system, tools: bundle.tools, roundtripMax: 2, onToolCall: async () => "RESULT-KEPT" });
  await assert.rejects(rt.run("loop-input"), (e: unknown) => e instanceof AgentFailure && e.code === "AGENT_TURN_LIMIT");
  phase = 1;
  assert.equal(await rt.run("next-input"), "続き");
  const last = JSON.parse(llm.requests[llm.requests.length - 1]!);
  const text = JSON.stringify(last.messages);
  assert.match(text, /loop-input/);
  assert.match(text, /RESULT-KEPT/);
});
