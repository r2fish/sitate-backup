// LLM API の障害とエージェントのプロセスの停止(spec 3.7「LLM API」、完了条件 19)。SDK の再試行を超えた失敗は、カタログの固定文で
// 状態と報告の表示(とチャット欄)に出し、API の障害とプロセスの停止は OS 通知にも出す。切れた応答は使わず再送しない(AGENT_OUTPUT_TRUNCATED)。
// 往復の上限(AGENT_TURN_LIMIT)は続けるかどうかを人が決めるのでチャット欄だけ。エージェントのプロセスが無くても状態の表示と承認カードは使える(P7)。
import type { Translate } from "./approval-card.ts";

const KEYS = {
  LLM_KEY_INVALID: "app.llm.key_invalid", LLM_BILLING: "app.llm.billing", LLM_RATE_LIMITED: "app.llm.rate_limited", LLM_PROVIDER_ERROR: "app.llm.provider_error",
  LLM_MODEL_NOT_FOUND: "app.llm.model_not_found", LLM_NO_RESPONSE: "app.llm.no_response", AGENT_EXITED: "app.agent.exited",
  AGENT_TURN_LIMIT: "app.agent.turn_limit", AGENT_OUTPUT_TRUNCATED: "app.agent.output_truncated",
} as const;
export type ReportedCode = keyof typeof KEYS;
const NOTIFIED = new Set<string>(["LLM_KEY_INVALID", "LLM_BILLING", "LLM_RATE_LIMITED", "LLM_PROVIDER_ERROR", "LLM_MODEL_NOT_FOUND", "LLM_NO_RESPONSE", "AGENT_EXITED"]);

export interface FailureSinks {
  readonly t: Translate;
  /** 状態と報告の表示の固定文(古い情報の印とは別の一覧)。 */
  readonly showNotice: (text: string) => void;
  readonly chat: (text: string) => void;
  readonly osNotify: (text: string) => void;
  /** 無効なキー: 秘密の入力ボックスのアプリ自身が開くモードで差し替える案内。 */
  readonly replaceKey?: () => void;
}

/** 失敗のコードを固定文にして出す。中断(AGENT_ABORTED)は人の操作なので何も出さない。 */
export function reportFailure(code: string, s: FailureSinks): void {
  if (!Object.hasOwn(KEYS, code)) return;
  const text = s.t(KEYS[code as ReportedCode]);
  s.showNotice(text);
  s.chat(text);
  if (NOTIFIED.has(code)) s.osNotify(text);
  if (code === "LLM_KEY_INVALID") s.replaceKey?.();
}
