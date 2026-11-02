// LLM API の失敗を列挙のコードに写す(spec 3.7 の「LLM API」。完了条件 19)。SDK の再試行を超えた失敗だけがここに来る。
import Anthropic from "@anthropic-ai/sdk";
import type { AgentFailureCode } from "./protocol.ts";
import { AgentFailure } from "./runtime.ts";

const HTTP_UNAUTHORIZED = 401;
const HTTP_PAYMENT_REQUIRED = 402;
const HTTP_FORBIDDEN = 403;
const HTTP_NOT_FOUND = 404;
const HTTP_RATE_LIMITED = 429;

export function failureCodeOf(e: unknown): AgentFailureCode {
  if (e instanceof AgentFailure) return e.code;
  if (e instanceof Anthropic.APIUserAbortError) return "AGENT_ABORTED";
  if (e instanceof Anthropic.APIConnectionTimeoutError) return "LLM_NO_RESPONSE";
  if (e instanceof Anthropic.APIError) return byStatus(e.status);
  return "LLM_PROVIDER_ERROR";
}

function byStatus(status: number | undefined): AgentFailureCode {
  switch (status) {
    case HTTP_UNAUTHORIZED: return "LLM_KEY_INVALID";
    case HTTP_PAYMENT_REQUIRED: return "LLM_BILLING";
    case HTTP_FORBIDDEN: return "LLM_KEY_INVALID";
    case HTTP_NOT_FOUND: return "LLM_MODEL_NOT_FOUND";
    case HTTP_RATE_LIMITED: return "LLM_RATE_LIMITED";
    default: return "LLM_PROVIDER_ERROR";
  }
}

/** 候補の順に Models API で存在を確かめ、存在する最初を返す。全部無いときは繰り下げず null(呼び出し側が LLM_MODEL_NOT_FOUND の固定文を出す)。 */
export async function selectModel(client: Anthropic, candidates: readonly string[]): Promise<{ model: string; isFirst: boolean } | null> {
  for (const [i, id] of candidates.entries()) {
    try {
      await client.models.retrieve(id);
      return { model: id, isFirst: i === 0 };
    } catch (e) {
      if (!(e instanceof Anthropic.NotFoundError)) throw e;
    }
  }
  return null;
}
