// LLM API 以外への送信を拒む fetch(完了条件 25)。SDK のクライアントへ明示して渡す。送り先は Anthropic の既定のエンドポイントの origin だけ。
import { LLM_API_BASE_URL } from "../constants.ts";

export class EgressDeniedError extends Error {
  constructor() {
    super("EGRESS_DENIED");
  }
}

export function makeGuardedFetch(inner: typeof fetch, allowedBase: string = LLM_API_BASE_URL): typeof fetch {
  const origin = new URL(allowedBase).origin;
  return (input, init) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : (input as Request).url;
    let o: string;
    try { o = new URL(url).origin; } catch { return Promise.reject(new EgressDeniedError()); }
    if (o !== origin) return Promise.reject(new EgressDeniedError());
    return inner(input, { ...init, redirect: "error" });
  };
}
