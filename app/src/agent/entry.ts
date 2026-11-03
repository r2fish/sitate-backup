// エージェントのプロセス(utilityProcess)の入口。主プロセスから IPC で init(LLM API キーと署名を検証した同梱物)を受け、会話を実行する。
// 環境変数・引数からキーを読まない。ファイル・キーチェーン・SSH・子プロセスの API を import しない。
import Anthropic from "@anthropic-ai/sdk";
import { LLM_API_BASE_URL } from "../constants.ts";
import type { AgentToMain, MainToAgent } from "./protocol.ts";
import { failureCodeOf, selectModel } from "./llm-errors.ts";
import { makeGuardedFetch } from "./guarded-fetch.ts";
import { withUsageReport } from "./usage-fetch.ts";
import { createAgentRuntime } from "./runtime.ts";
import type { AgentRuntime } from "./runtime.ts";
import { buildSystemBlocks } from "./system-prompt.ts";

interface ParentPort {
  postMessage(m: AgentToMain): void;
  on(event: "message", cb: (e: { data: MainToAgent }) => void): void;
}
const parentPort = (process as unknown as { parentPort: ParentPort }).parentPort;
const send = (m: AgentToMain): void => parentPort.postMessage(m);

let runtime: AgentRuntime | null = null;
const pendingCalls = new Map<string, (content: string) => void>();
let nextCall = 0;

async function init(msg: Extract<MainToAgent, { type: "init" }>): Promise<void> {
  const client = new Anthropic({ apiKey: msg.apiKey, baseURL: LLM_API_BASE_URL, fetch: withUsageReport(makeGuardedFetch(fetch), (usage) => send({ type: "usage", usage })) });
  try {
    const picked = await selectModel(client, msg.bundle.models);
    if (picked === null) return send({ type: "failure", id: null, code: "LLM_MODEL_NOT_FOUND" });
    runtime = createAgentRuntime({
      client,
      model: picked.model,
      system: buildSystemBlocks(msg.bundle, { locale: msg.locale, date: msg.date, model: picked.model }),
      tools: msg.bundle.tools,
      onToolCall: (call) => new Promise<string>((resolve) => {
        const callId = `c${nextCall++}`;
        pendingCalls.set(callId, resolve);
        send({ type: "tool_call", callId, name: call.name, input: call.input });
      }),
    });
    send({ type: "ready", model: picked.model, modelIsFirst: picked.isFirst });
  } catch (e) {
    send({ type: "failure", id: null, code: failureCodeOf(e) });
  }
}

async function input(msg: Extract<MainToAgent, { type: "input" }>): Promise<void> {
  if (runtime === null) return send({ type: "failure", id: msg.id, code: "AGENT_ABORTED" });
  try {
    send({ type: "reply", id: msg.id, text: await runtime.run(msg.text) });
  } catch (e) {
    send({ type: "failure", id: msg.id, code: failureCodeOf(e) });
  }
}

/** 入力は 1 つずつ実行する(実行中に次の入力が来ても、会話の履歴と中断の状態を同時に触らない)。 */
let inputChain: Promise<void> = Promise.resolve();

parentPort.on("message", ({ data }) => {
  if (data.type === "init") void init(data);
  else if (data.type === "input") inputChain = inputChain.then(() => input(data));
  else if (data.type === "abort") runtime?.abort();
  else {
    if (data.stepPassed === true) runtime?.resetRoundtrips();
    pendingCalls.get(data.callId)?.(data.content);
  }
});
