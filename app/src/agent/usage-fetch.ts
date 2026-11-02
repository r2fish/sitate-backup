// LLM API への要求ごとの使用量と時間の計測(spec 6.4 の M-20・M-22、完了条件 24)。fetch を包み、メッセージ作成の要求について、
// 送信から応答の本文を読み終えるまでの時間と、応答の usage(入出力・キャッシュから読んだトークン数)とモデル名だけを報告する。本文・会話の文は報告しない。
import type { Usage } from "./protocol.ts";

const MESSAGES_PATH = /\/v1\/messages(\?|$)/;

export function withUsageReport(base: typeof fetch, report: (u: Usage) => void, now: () => number = Date.now): typeof fetch {
  return (async (input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    const method = (init?.method ?? (typeof input === "object" && "method" in input ? input.method : "GET")).toUpperCase();
    const started = now();
    const res = await base(input, init);
    if (method !== "POST" || !MESSAGES_PATH.test(url) || !res.ok) return res;
    try {
      const body = (await res.clone().json()) as { model?: unknown; usage?: Record<string, unknown> };
      const u = body.usage ?? {};
      const n = (k: string): number => (typeof u[k] === "number" ? (u[k] as number) : 0);
      report({ input_tokens: n("input_tokens"), output_tokens: n("output_tokens"), cache_read_input_tokens: n("cache_read_input_tokens"), model: typeof body.model === "string" ? body.model : "", elapsed_ms: now() - started });
    } catch {
      // 計測の失敗は会話を止めない。
    }
    return res;
  }) as typeof fetch;
}
