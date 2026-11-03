// 継ぎ目 AgentRuntime(spec 3.7。D-0091)。エージェントのプロセスの中で、Anthropic SDK の Tool Runner が道具の往復のループを持つ。
// 道具の呼び出し {id, name, input} ごとに主プロセスのコールバックを呼ぶ。道具は主プロセスが渡した定義だけを betaTool で読み込む。
// このファイルは Electron・ファイル・キーチェーン・SSH・子プロセスの API を import しない(静的検査: test/u3a/boundary.test.ts)。
import { betaTool } from "@anthropic-ai/sdk/helpers/beta/json-schema";
import type { AgentFailureCode, ToolDefinition, Usage } from "./protocol.ts";
import type { SystemBlock } from "./system-prompt.ts";
import { LLM_MAX_TOKENS, TOOL_ROUNDTRIP_MAX } from "../constants.ts";

/** SDK の Tool Runner が使うクライアントの最小の面(試験では偽のモデルを差し込む)。 */
export interface RunnerClient {
  readonly beta: { readonly messages: { toolRunner(params: any, options?: any): any } };
}

export interface ToolCallRequest {
  readonly id: string;
  readonly name: string;
  readonly input: unknown;
}

export interface AgentRuntimeOptions {
  readonly client: RunnerClient;
  readonly model: string;
  readonly system: readonly SystemBlock[];
  readonly tools: readonly ToolDefinition[];
  /** 主プロセスのコールバック。人の操作が要る道具では、その操作の完了か取り消しまで戻らない。戻り値は整形済みの封筒(文字列)。 */
  readonly onToolCall: (call: ToolCallRequest) => Promise<string>;
  readonly onUsage?: (usage: Usage) => void;
  readonly roundtripMax?: number;
}

export class AgentFailure extends Error {
  readonly code: AgentFailureCode;
  constructor(code: AgentFailureCode) {
    super(code);
    this.code = code;
  }
}

export interface AgentRuntime {
  /** チャット欄の入力 1 つに対する会話の実行。モデルの最後の文を返す(表示の前に主プロセスが引用の照合を通す)。 */
  run(text: string): Promise<string>;
  /** 道具の往復の数を数え直す(導入で手順が合格したとき。主プロセスの合図)。 */
  resetRoundtrips(): void;
  /** 次の道具の呼び出しを止める。実行中のコールバックは終わるまで待つ。 */
  abort(): void;
}

export function createAgentRuntime(opts: AgentRuntimeOptions): AgentRuntime {
  const history: unknown[] = [];
  const max = opts.roundtripMax ?? TOOL_ROUNDTRIP_MAX;
  let stop: AbortController | null = null;
  let failure: AgentFailure | null = null;
  let roundtrips = 0;
  /** 失敗を記録して次のモデルの呼び出しを止める。SDK は道具の中の例外を道具の結果にして続けるので、例外では止められない。 */
  const halt = (code: AgentFailureCode): string => {
    failure ??= new AgentFailure(code);
    stop?.abort();
    return code;
  };
  return {
    resetRoundtrips: () => {
      roundtrips = 0;
    },
    abort: () => {
      halt("AGENT_ABORTED");
    },
    async run(text: string): Promise<string> {
      stop = new AbortController();
      failure = null;
      roundtrips = 0;
      const tools = opts.tools.map((def) =>
        betaTool({
          name: def.name,
          description: def.description,
          inputSchema: def.input_schema as any,
          run: async (_args: unknown, ctx?: { toolUse?: { id: string; input: unknown } }) => {
            if (failure !== null) return failure.code;
            roundtrips += 1;
            if (roundtrips > max) return halt("AGENT_TURN_LIMIT");
            return opts.onToolCall({ id: ctx?.toolUse?.id ?? "", name: def.name, input: ctx?.toolUse?.input ?? _args });
          },
        }),
      );
      const started = Date.now();
      const runner = opts.client.beta.messages.toolRunner({
        model: opts.model,
        max_tokens: LLM_MAX_TOKENS,
        system: opts.system,
        tools,
        thread: false,
        messages: [...history, { role: "user", content: text }],
      }, { signal: stop.signal });
      const keep = (): void => { history.splice(0, history.length, ...settled([...runner.params.messages] as Msg[])); };
      const final = await runner.runUntilDone().catch((e: unknown) => {
        keep();
        throw failure ?? e;
      });
      if (failure !== null) { keep(); throw failure; }
      if (final.stop_reason === "max_tokens") throw new AgentFailure("AGENT_OUTPUT_TRUNCATED");
      const kept = [...runner.params.messages] as Msg[];
      if (kept[kept.length - 1]?.role !== "assistant") kept.push({ role: final.role, content: final.content } as Msg);
      history.splice(0, history.length, ...settled(kept));
      opts.onUsage?.(usageOf(final, opts.model, Date.now() - started));
      return textOf(final.content);
    },
  };
}

type Msg = { role: string; content?: unknown };
const blocksOf = (m: Msg): { type?: string; id?: string; tool_use_id?: string }[] => (Array.isArray(m.content) ? (m.content as { type?: string }[]) : []);

/**
 * 次の要求に使える履歴にする(実 API の規則): 中身の無い assistant(本文も道具の呼び出しも無い応答)は残さない。
 * 道具の結果が付いていない道具の呼び出しの assistant(中断・上限で打ち切った回)は、その後ろごと捨てる。
 * ここまでの呼び出しの記憶(途中の道具の往復)は残す。
 */
function settled(messages: Msg[]): Msg[] {
  const out: Msg[] = [];
  for (let i = 0; i < messages.length; i++) {
    const m = messages[i]!;
    if (m.role !== "assistant") { out.push(m); continue; }
    const blocks = blocksOf(m);
    if (typeof m.content === "string" ? m.content === "" : blocks.length === 0) continue;
    const uses = blocks.filter((b) => b.type === "tool_use").map((b) => b.id);
    if (uses.length > 0) {
      const have = new Set(blocksOf(messages[i + 1] ?? { role: "" }).filter((b) => b.type === "tool_result").map((b) => b.tool_use_id));
      if (messages[i + 1]?.role !== "user" || !uses.every((id) => have.has(id))) break;
    }
    out.push(m);
  }
  return out;
}

function textOf(content: readonly { type: string; text?: string }[]): string {
  return content.filter((b) => b.type === "text").map((b) => b.text ?? "").join("");
}

function usageOf(msg: { usage?: Record<string, number> }, model: string, elapsed: number): Usage {
  const u = msg.usage ?? {};
  return { input_tokens: u.input_tokens ?? 0, output_tokens: u.output_tokens ?? 0, cache_read_input_tokens: u.cache_read_input_tokens ?? 0, model, elapsed_ms: elapsed };
}
