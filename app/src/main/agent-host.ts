// エージェントのプロセス(utilityProcess)の起動と IPC(spec 3.7、完了条件 1・17・25)。主プロセスだけが持つ。
// LLM API キーは起動後の init メッセージでだけ渡す(環境変数・引数は使わない)。環境は空で起動し、SDK が環境変数から読む値(ANTHROPIC_LOG など)を閉じる。
import { dirname } from "node:path";
import type { AgentBundle } from "../agent/protocol.ts";
import type { AgentFailureCode, AgentToMain, MainToAgent, Usage } from "../agent/protocol.ts";

/** utilityProcess.fork の戻り値のうち使う部分(試験では差し替える)。 */
export interface ForkedAgent {
  postMessage(m: MainToAgent): void;
  on(event: "message", cb: (m: AgentToMain) => void): void;
  on(event: "exit", cb: (code: number) => void): void;
  kill(): boolean;
}
/** fork の引数(args と env は常に空)。実装は Electron の utilityProcess.fork。 */
export type ForkAgent = (entry: string, options: { readonly args: readonly string[]; readonly env: Readonly<Record<string, string>>; readonly serviceName: string; readonly execArgv: readonly string[] }) => ForkedAgent;

export interface AgentHostEvents {
  onReady(model: string, modelIsFirst: boolean): void;
  onReply(id: string, text: string): void;
  onFailure(id: string | null, code: AgentFailureCode | "AGENT_EXITED"): void;
  onUsage(usage: Usage): void;
  /** 道具の呼び出し(操作案)。戻り値は封筒の区画の文字列。人の操作が要る道具では操作の完了まで戻らない。 */
  onToolCall(name: string, input: unknown): Promise<string | { readonly content: string; readonly stepPassed: boolean }>;
}

export class AgentHost {
  #proc: ForkedAgent | null = null;
  readonly #fork: ForkAgent;
  readonly #entry: string;
  readonly events: AgentHostEvents;
  constructor(fork: ForkAgent, entry: string, events: AgentHostEvents) {
    this.#fork = fork;
    this.#entry = entry;
    this.events = events;
  }

  get running(): boolean {
    return this.#proc !== null;
  }

  /** 起動し、init で鍵と同梱物を渡す。鍵はこの呼び出しの引数としてだけ存在し、保持しない。 */
  start(apiKey: string, bundle: AgentBundle, locale: string, date: string): void {
    this.stop();
    const proc = this.#fork(this.#entry, { args: [], env: {}, serviceName: "sitate-agent", execArgv: agentExecArgv(this.#entry) });
    this.#proc = proc;
    proc.on("message", (m) => void this.#dispatch(proc, m));
    proc.on("exit", () => {
      if (this.#proc === proc) { this.#proc = null; this.events.onFailure(null, "AGENT_EXITED"); }
    });
    proc.postMessage({ type: "init", apiKey, bundle, locale, date });
  }

  send(id: string, text: string): void {
    if (this.#proc === null) return this.events.onFailure(id, "AGENT_EXITED");
    this.#proc.postMessage({ type: "input", id, text });
  }

  abort(): void {
    this.#proc?.postMessage({ type: "abort" });
  }

  stop(): void {
    const p = this.#proc;
    this.#proc = null;
    p?.kill();
  }

  async #dispatch(proc: ForkedAgent, m: AgentToMain): Promise<void> {
    if (this.#proc !== proc) return;
    switch (m.type) {
      case "ready": return this.events.onReady(m.model, m.modelIsFirst);
      case "reply": return this.events.onReply(m.id, m.text);
      case "failure": return this.events.onFailure(m.id, m.code);
      case "usage": return this.events.onUsage(m.usage);
      case "tool_call": {
        const r = await this.events.onToolCall(m.name, m.input);
        const content = typeof r === "string" ? r : r.content;
        if (this.#proc === proc) proc.postMessage({ type: "tool_result", callId: m.callId, content, ...(typeof r !== "string" && r.stepPassed ? { stepPassed: true } : {}) });
      }
    }
  }
}

export type { AgentBundle };

/**
 * エージェントのプロセスの実行の制限(完了条件 25): Node の権限モデルで、子プロセス・ファイルの書き込み・ワーカー・ネイティブアドオンを拒み、
 * ファイルの読み取りは自分のバンドルの置き場(読み取り専用の資源の場所)だけ。キーチェーンとアプリのデータの置き場は読めない。ネットワークは権限モデルでは許可か不許可の二択のため許可し、
 * LLM API 以外への送信は agent/guarded-fetch.ts が拒む。
 */
export const agentExecArgv = (entry: string): string[] => ["--permission", `--allow-fs-read=${dirname(entry)}`, "--allow-net"];
