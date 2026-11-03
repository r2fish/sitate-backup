// LLM の口の試験: 中継(proxy)はアプリが付けた認証ヘッダをそのまま上流へ渡し、送った内容(本文)を記録し、認証ヘッダ(キー)は記録しない。偽の口は台本の応答を返す。
import assert from "node:assert/strict";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { makeFakeAgent, planOf } from "./fake-agent.ts";
import { startLlmEndpoint } from "./llm-endpoint.ts";
import { Recorder } from "./record.ts";
import type { Answers } from "./script.ts";

const KEY = "sk-ant-fake-endpointtest0123456789";

test("中継: 認証ヘッダを上流へそのまま渡し、本文を記録し、キーは記録に出さない。フェイクの口はモデルの確認に答える", async () => {
  const seen: { auth: string | undefined; body: string }[] = [];
  const upstream = createServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on("data", (c: Buffer) => chunks.push(c));
    req.on("end", () => { seen.push({ auth: req.headers.authorization, body: Buffer.concat(chunks).toString("utf8") }); res.writeHead(200, { "content-type": "application/json" }); res.end(JSON.stringify({ id: "m", type: "message", role: "assistant", content: [], usage: {} })); });
  });
  await new Promise<void>((r) => upstream.listen(0, "127.0.0.1", () => r()));
  const dir = mkdtempSync(join(tmpdir(), "e2e-ep-"));
  const rec = new Recorder(dir, [KEY]);
  const proxy = await startLlmEndpoint({ mode: "proxy", rec, expectKey: KEY, upstream: `http://127.0.0.1:${(upstream.address() as AddressInfo).port}` });
  try {
    const r = await fetch(`${proxy.url}/v1/messages`, { method: "POST", headers: { authorization: `Bearer ${KEY}`, "content-type": "application/json" }, body: JSON.stringify({ model: "m1", messages: [{ role: "user", content: "こんにちは" }] }) });
    assert.equal(r.status, 200);
    assert.equal(seen[0]?.auth, `Bearer ${KEY}`, "認証ヘッダを上流へ渡した");
    assert.equal(proxy.requests(), 1);
    assert.equal(proxy.keyMismatches(), 0);
    const text = readFileSync(rec.path, "utf8");
    assert.ok(text.includes("こんにちは") && text.includes('"key_ok":true'));
    assert.ok(!text.includes(KEY), "キーは記録に出ない");
    await fetch(`${proxy.url}/v1/messages`, { method: "POST", headers: { authorization: "Bearer other", "content-type": "application/json" }, body: "{}" });
    assert.equal(proxy.keyMismatches(), 1, "別のキーは不一致として数える");
  } finally { await proxy.close(); upstream.close(); upstream.closeAllConnections(); rmSync(dir, { recursive: true, force: true }); }
});

test("偽の口: Models API に答え、メッセージの要求に台本の道具の呼び出しを返す。台本は履歴から次の一手を決める", async () => {
  const a = { adminUser: "u", backupHost: "backup", sourceHost: "source", destHost: "dest", destPort: 22, storageEndpoint: "http://s3.test", bucket: "b" } as Answers;
  const dir = mkdtempSync(join(tmpdir(), "e2e-ep-"));
  const rec = new Recorder(dir, [KEY]);
  const ep = await startLlmEndpoint({ mode: "fake", rec, expectKey: KEY, fake: makeFakeAgent(a, async () => undefined) });
  try {
    const m = await fetch(`${ep.url}/v1/models/claude-sonnet-5-5`, { headers: { authorization: `Bearer ${KEY}` } });
    assert.equal(((await m.json()) as { id: string }).id, "claude-sonnet-5-5");
    const first = await fetch(`${ep.url}/v1/messages`, { method: "POST", headers: { authorization: `Bearer ${KEY}` }, body: JSON.stringify({ messages: [{ role: "user", content: "始めて" }] }) });
    const j1 = (await first.json()) as { stop_reason: string; content: { type: string; name?: string }[] };
    assert.equal(j1.stop_reason, "tool_use");
    assert.equal(j1.content[0]?.name, "sitate_bootstrap");
    const history = [{ role: "user", content: "始めて" }, { role: "assistant", content: j1.content }, { role: "user", content: [{ type: "tool_result", tool_use_id: "x", content: "ok" }] }];
    const second = await fetch(`${ep.url}/v1/messages`, { method: "POST", headers: { authorization: `Bearer ${KEY}` }, body: JSON.stringify({ messages: history }) });
    assert.equal(((await second.json()) as { content: { name?: string }[] }).content[0]?.name, "sitate_version");
    assert.equal(planOf(a, async () => undefined).length, 6, "人の入力 6 回分の台本(Q1・Q3・Q4・Q4 の案・Q2 の問いを挟む)");
  } finally { await ep.close(); rmSync(dir, { recursive: true, force: true }); }
});
