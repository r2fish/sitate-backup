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
