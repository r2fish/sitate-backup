// A2 の回ごとの記録の欄の追加分(D-0177。判定は変えない): 核の `sitate version` の結果と、M-20・M-22 のアプリの記録(完了条件 24)の集計。
// どちらも秘密を含まない値だけを取り出す(version・build・signing_key_fp は公開の識別子、アプリの記録は数値とモデル名だけ)。
import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

const VERSION_CMD = "cd /; runuser -u sitate -- env SITATE_TEST_TRUST_PUBKEY_FILE=/etc/sitate-test/trust.pub /usr/local/bin/sitate version";
const PERCENTILE = 0.9;

export interface CoreVersion { readonly version: string | null; readonly build: string | null; readonly signing_key_fp: string | null; readonly error?: string }

/** backup の核の `sitate version` の結果(version・build・signing_key_fp だけ。sim_time などは載せない)。取れなければ error を持つ(判定に使わない)。 */
export function coreVersion(container: string): CoreVersion {
  const none = { version: null, build: null, signing_key_fp: null };
  const r = spawnSync("docker", ["exec", container, "bash", "-c", VERSION_CMD], { encoding: "utf8", timeout: 30000 });
  if (r.status !== 0) return { ...none, error: `docker exec が終了コード ${String(r.status)}` };
  try {
    const res = (JSON.parse(r.stdout) as { result?: Record<string, unknown> }).result ?? {};
    const s = (k: string): string | null => (typeof res[k] === "string" ? (res[k] as string) : null);
    return { version: s("version"), build: s("build"), signing_key_fp: s("signing_key_fp") };
  } catch { return { ...none, error: "sitate version の出力が JSON でない" }; }
}

interface Req { model?: unknown; input_tokens?: unknown; output_tokens?: unknown; cache_read_input_tokens?: unknown; elapsed_ms?: unknown }
interface Run { elapsed_ms?: unknown }
const n = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? v : 0);
const sum = (xs: number[]): number => xs.reduce((a, b) => a + b, 0);

/** M-22: 要求ごとの送信から応答の完了までの時間の分布(件数・最小・中央値・90 パーセンタイル・最大)。 */
export function distribution(xs: number[]): { count: number; min: number | null; median: number | null; p90: number | null; max: number | null } {
  if (xs.length === 0) return { count: 0, min: null, median: null, p90: null, max: null };
  const s = [...xs].sort((a, b) => a - b);
  const at = (q: number): number => s[Math.min(s.length - 1, Math.ceil(q * s.length) - 1)]!;
  return { count: s.length, min: s[0]!, median: at(0.5), p90: at(PERCENTILE), max: s.at(-1)! };
}

/** アプリのデータの置き場の app-record.json を集計する。無ければ null(出していない)。 */
export function appRecordSummary(userData: string): Record<string, unknown> | null {
  const file = join(userData, "app-record.json");
  if (!existsSync(file)) return null;
  try {
    const j = JSON.parse(readFileSync(file, "utf8")) as { requests?: Req[]; runs?: Run[] };
    const reqs = Array.isArray(j.requests) ? j.requests : [];
    const runs = Array.isArray(j.runs) ? j.runs : [];
    return {
      // M-20: 入力ごとの所要時間の合計、入出力のトークン数、キャッシュから読んだトークン数、使用したモデル名
      runs: runs.length, runs_elapsed_ms_total: sum(runs.map((r) => n(r.elapsed_ms))),
      requests: reqs.length, input_tokens: sum(reqs.map((r) => n(r.input_tokens))), output_tokens: sum(reqs.map((r) => n(r.output_tokens))),
      cache_read_input_tokens: sum(reqs.map((r) => n(r.cache_read_input_tokens))),
      models: [...new Set(reqs.map((r) => String(r.model)))].sort(),
      // M-22: 要求ごとの応答時間の分布
      request_elapsed_ms: distribution(reqs.map((r) => n(r.elapsed_ms))),
    };
  } catch { return { error: "app-record.json を読めない" }; }
}
