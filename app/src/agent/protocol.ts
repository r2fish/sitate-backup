// 主プロセスとエージェントのプロセス(utilityProcess)の間の IPC の型(spec 3.7)。画面のプロセスはこの経路に出ない。
// エージェントのプロセスへ渡すもの: LLM API キー(起動後に IPC で。環境変数・引数は使わない)、署名を検証した同梱物。
// エージェントのプロセスから出るもの: 道具の呼び出し(操作案)、モデルの文、列挙のコード、使用量。秘密の値と元サーバー由来のパスの実値は載せない。

export interface ToolDefinition {
  readonly name: string;
  readonly description: string;
  readonly input_schema: Readonly<Record<string, unknown>>;
}

/** 署名を検証した同梱物のうち、システムプロンプトと道具に使うもの(主プロセスが検証して渡す)。 */
export interface AgentBundle {
  readonly manual: string;
  readonly procedure: string;
  readonly rules: string;
  readonly tools: readonly ToolDefinition[];
  /** モデルの候補(順序付き)。起動時に存在を確かめ、存在する最初を使う。 */
  readonly models: readonly string[];
}

/** 変わる値。キャッシュの区切りの後ろに置く。 */
export interface VolatileContext {
  readonly locale: string;
  readonly date: string;
  readonly model: string;
}

export type MainToAgent =
  | { readonly type: "init"; readonly apiKey: string; readonly bundle: AgentBundle; readonly locale: string; readonly date: string; readonly baseUrl?: string }
  | { readonly type: "input"; readonly id: string; readonly text: string }
  | { readonly type: "tool_result"; readonly callId: string; readonly content: string; readonly stepPassed?: boolean }
  | { readonly type: "abort" };

/** エージェントのプロセスが返す失敗の列挙(spec 3.7 の「LLM API」と「アプリの原因コード」)。 */
export type AgentFailureCode =
  | "AGENT_TURN_LIMIT" | "AGENT_OUTPUT_TRUNCATED" | "LLM_KEY_INVALID" | "LLM_BILLING" | "LLM_RATE_LIMITED" | "LLM_PROVIDER_ERROR"
  | "LLM_MODEL_NOT_FOUND" | "LLM_NO_RESPONSE" | "AGENT_ABORTED";

export interface Usage {
  readonly input_tokens: number;
  readonly output_tokens: number;
  readonly cache_read_input_tokens: number;
  readonly model: string;
  readonly elapsed_ms: number;
}

export type AgentToMain =
  | { readonly type: "ready"; readonly model: string; readonly modelIsFirst: boolean }
  | { readonly type: "tool_call"; readonly callId: string; readonly name: string; readonly input: unknown }
  | { readonly type: "reply"; readonly id: string; readonly text: string }
  | { readonly type: "failure"; readonly id: string | null; readonly code: AgentFailureCode }
  | { readonly type: "usage"; readonly usage: Usage };
