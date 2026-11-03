// 試験用の偽の LLM API。本物の SDK のクライアントに fetch を差し込み、台本の応答を返す。送った要求の本文を全部記録する(「LLM API へ送った内容」の検査に使う)。
import Anthropic from "@anthropic-ai/sdk";

export type Scripted = { text?: string; toolUses?: { name: string; input: unknown }[]; stop?: string };
export interface FakeLlm {
  readonly client: Anthropic;
  /** 送った要求の本文(JSON の文字列)。 */
  readonly requests: string[];
}

export function fakeLlm(script: Scripted[] | ((n: number, body: any) => Scripted)): FakeLlm {
  const requests: string[] = [];
  let n = 0;
  const fetchFn = async (_url: unknown, init?: { body?: unknown }): Promise<Response> => {
    const raw = String(init?.body ?? "");
    requests.push(raw);
    const bad = invalidHistory(JSON.parse(raw));
    if (bad !== null) return new Response(JSON.stringify({ type: "error", error: { type: "invalid_request_error", message: bad } }), { status: 400, headers: { "content-type": "application/json" } });
    const step = typeof script === "function" ? script(n, JSON.parse(raw)) : script[Math.min(n, script.length - 1)]!;
    n += 1;
    const content: unknown[] = [];
    if (step.text !== undefined) content.push({ type: "text", text: step.text });
    (step.toolUses ?? []).forEach((t, i) => content.push({ type: "tool_use", id: `toolu_${n}_${i}`, name: t.name, input: t.input }));
    const body = { id: `msg_${n}`, type: "message", role: "assistant", model: "claude-sonnet-5-5", content, stop_reason: step.stop ?? (step.toolUses?.length ? "tool_use" : "end_turn"), stop_sequence: null, usage: { input_tokens: 10, output_tokens: 5, cache_read_input_tokens: 3 } };
    return new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
  };
  const client = new Anthropic({ apiKey: "test-key-not-real", fetch: fetchFn as typeof fetch, maxRetries: 0 });
  return { client, requests };
}

/** 実 API が 400 にする履歴の規則: 中身の無い assistant、道具の結果の付かない道具の呼び出し。 */
function invalidHistory(body: { messages?: { role: string; content: unknown }[] }): string | null {
  const ms = body.messages ?? [];
  for (let i = 0; i < ms.length; i++) {
    const m = ms[i]!;
    if (m.role !== "assistant") continue;
    const blocks = Array.isArray(m.content) ? (m.content as { type: string; id?: string }[]) : [];
    if (typeof m.content === "string" ? m.content === "" : blocks.length === 0) return `messages.${i}: all messages must have non-empty content except for the optional final assistant message`;
    const next = ms[i + 1];
    const results = new Set(Array.isArray(next?.content) ? (next!.content as { type: string; tool_use_id?: string }[]).filter((b) => b.type === "tool_result").map((b) => b.tool_use_id) : []);
    for (const b of blocks) if (b.type === "tool_use" && !results.has(b.id)) return `messages.${i}: tool_use ids were found without tool_result blocks immediately after: ${String(b.id)}`;
  }
  return null;
}
