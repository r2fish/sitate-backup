import { test } from "node:test";
import assert from "node:assert/strict";
import Anthropic from "@anthropic-ai/sdk";
import { failureCodeOf, selectModel } from "../../src/agent/llm-errors.ts";

// LLM API の失敗の写像とモデルの候補(完了条件 18・19)。実際の API は呼ばない(偽の fetch)。
const clientWith = (status: (url: string) => number) => new Anthropic({ apiKey: "test-key-not-real", maxRetries: 0, fetch: (async (url: unknown) => {
  const s = status(String(url));
  return new Response(JSON.stringify(s === 200 ? { id: "m", type: "model", display_name: "m", created_at: "2026-01-01T00:00:00Z" } : { type: "error", error: { type: "x", message: "m" } }), { status: s, headers: { "content-type": "application/json" } });
}) as typeof fetch });

test("llm: failure statuses map to the enumerated codes", async () => {
  const cases: [number, string][] = [[401, "LLM_KEY_INVALID"], [402, "LLM_BILLING"], [404, "LLM_MODEL_NOT_FOUND"], [429, "LLM_RATE_LIMITED"], [500, "LLM_PROVIDER_ERROR"], [529, "LLM_PROVIDER_ERROR"]];
  for (const [status, code] of cases) {
    const err = await clientWith(() => status).models.retrieve("x").catch((e: unknown) => e);
    assert.equal(failureCodeOf(err), code, String(status));
  }
});

test("llm: the first candidate that exists is used, a later one is marked, and none left means no fallback", async () => {
  const exists = (ids: string[]) => clientWith((url) => (ids.some((i) => url.endsWith("/" + i) || url.includes("/" + i + "?")) ? 200 : 404));
  const list = ["claude-sonnet-5-5", "claude-sonnet-5", "claude-sonnet-4-6"];
  assert.deepEqual(await selectModel(exists(list), list), { model: "claude-sonnet-5-5", isFirst: true });
  assert.deepEqual(await selectModel(exists(list.slice(1)), list), { model: "claude-sonnet-5", isFirst: false });
  assert.equal(await selectModel(exists([]), list), null);
});
