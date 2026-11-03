// LLM API の口(試験用のアプリの SDK の baseURL をここへ向ける。試験用のビルドだけで効く)。
//   fake: 偽のエージェントの台本で応答する(LLM API キーは使わない)。
//   proxy: 実物の LLM API へ中継する(キーはアプリの要求の認証ヘッダをそのまま渡す。記録には出さない)。
// どちらも、アプリが LLM API へ送った内容(要求の本文)を記録する(GB-126)。ヘッダ(キー)は記録しない。
import { createServer } from "node:http";
import type { IncomingMessage, Server, ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import type { Recorder } from "./record.ts";

export interface FakeStep { readonly text?: string; readonly toolUses?: readonly { name: string; input: unknown }[] }
/** 偽のエージェント: 要求の本文から次の応答を決める(待ちを入れてよい)。 */
export type FakeResponder = (body: { messages: unknown[] }) => Promise<FakeStep> | FakeStep;

export interface LlmEndpoint { readonly url: string; readonly requests: () => number; readonly keyMismatches: () => number; close(): Promise<void> }

const UPSTREAM = "https://api.anthropic.com";
// SDK(1.3.0)は API キーを Authorization: Bearer で送る(x-api-key ではない)。どちらも、アプリが付けたものをそのまま中継する。
const PASS_REQUEST = ["x-api-key", "authorization", "anthropic-version", "anthropic-beta", "content-type"];
const PASS_RESPONSE = ["content-type", "request-id", "retry-after", "retry-after-ms", "x-should-retry"];

async function readBody(req: IncomingMessage): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const c of req) chunks.push(c as Buffer);
  return Buffer.concat(chunks).toString("utf8");
}

function send(res: ServerResponse, status: number, body: unknown): void {
  res.writeHead(status, { "content-type": "application/json" });
  res.end(JSON.stringify(body));
}

function messageOf(n: number, step: FakeStep): unknown {
  const content: unknown[] = [];
  if (step.text !== undefined) content.push({ type: "text", text: step.text });
  (step.toolUses ?? []).forEach((t, i) => content.push({ type: "tool_use", id: `toolu_${n}_${i}`, name: t.name, input: t.input }));
  const tool = (step.toolUses ?? []).length > 0;
  return { id: `msg_${n}`, type: "message", role: "assistant", model: "claude-sonnet-5-5", content, stop_reason: tool ? "tool_use" : "end_turn", stop_sequence: null, usage: { input_tokens: 10, output_tokens: 5, cache_read_input_tokens: 3 } };
}

async function proxy(upstream: string, req: IncomingMessage, res: ServerResponse, raw: string): Promise<void> {
  const headers: Record<string, string> = {};
  for (const h of PASS_REQUEST) { const v = req.headers[h]; if (typeof v === "string") headers[h] = v; }
  const up = await fetch(upstream + (req.url ?? ""), { method: req.method ?? "GET", headers, ...(req.method === "POST" ? { body: raw } : {}) });
  const out: Record<string, string> = {};
  for (const h of PASS_RESPONSE) { const v = up.headers.get(h); if (v !== null) out[h] = v; }
  res.writeHead(up.status, out);
  res.end(Buffer.from(await up.arrayBuffer()));
}

export async function startLlmEndpoint(opts: { mode: "fake" | "proxy"; rec: Recorder; fake?: FakeResponder; expectKey: string; upstream?: string }): Promise<LlmEndpoint> {
  let n = 0;
  let mismatches = 0;
  const handle = async (req: IncomingMessage, res: ServerResponse): Promise<void> => {
    const raw = await readBody(req);
    const url = req.url ?? "";
    const isMessages = req.method === "POST" && /^\/v1\/messages(\?|$)/.test(url);
    if (isMessages) {
      n += 1;
      let body: unknown = null;
      try { body = JSON.parse(raw); } catch { /* 本文が JSON でない要求も記録する */ }
      // アプリが IPC で受けた LLM API キーを SDK へ明示して渡したことの確認(値は記録しない。一致したかだけ)。
      const keyOk = req.headers["x-api-key"] === opts.expectKey || req.headers["authorization"] === `Bearer ${opts.expectKey}`;
      if (!keyOk) mismatches += 1;
      opts.rec.write("llm_request", { n, model: (body as { model?: string } | null)?.model ?? null, key_ok: keyOk, body });
    }
    if (opts.mode === "proxy") return proxy(opts.upstream ?? UPSTREAM, req, res, raw);
    if (req.method === "GET" && /^\/v1\/models\/[^/?]+/.test(url)) return send(res, 200, { id: decodeURIComponent(url.split("/")[3]!.split("?")[0]!), type: "model", display_name: "fake", created_at: "2026-01-01T00:00:00Z" });
    if (!isMessages) return send(res, 404, { type: "error", error: { type: "not_found_error", message: "not found" } });
    const step = await opts.fake!(JSON.parse(raw) as { messages: unknown[] });
    send(res, 200, messageOf(n, step));
  };
  const server: Server = createServer((req, res) => {
    handle(req, res).catch((e: unknown) => { opts.rec.write("llm_endpoint_error", { message: String(e).slice(0, 300) }); if (!res.headersSent) send(res, 502, { type: "error", error: { type: "api_error", message: "endpoint error" } }); else res.end(); });
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
  const port = (server.address() as AddressInfo).port;
  return { url: `http://127.0.0.1:${port}`, requests: () => n, keyMismatches: () => mismatches, close: () => new Promise<void>((r) => { server.close(() => r()); server.closeAllConnections(); }) };
}
