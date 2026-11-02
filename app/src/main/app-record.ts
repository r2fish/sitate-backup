// アプリの記録(spec 6.4 の M-20・M-22、完了条件 24)。アプリのデータの置き場に JSON で残す: 入力ごとの所要時間、LLM API への要求ごとの
// 入出力のトークン数・キャッシュから読んだトークン数・使用したモデル名・送信から応答の完了までの時間。会話の本文と秘密は残さない(数値とモデル名だけ)。
import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";
import type { Usage } from "../agent/protocol.ts";

interface RequestEntry { readonly at: string; readonly model: string; readonly input_tokens: number; readonly output_tokens: number; readonly cache_read_input_tokens: number; readonly elapsed_ms: number }
interface RunEntry { readonly at: string; readonly elapsed_ms: number }
interface RecordFile { readonly requests: RequestEntry[]; readonly runs: RunEntry[] }

const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? v : 0);

export class AppRecorder {
  readonly #path: string;
  readonly #now: () => Date;
  readonly #started = new Map<string, number>();

  constructor(path: string, now: () => Date = () => new Date()) {
    this.#path = path;
    this.#now = now;
  }

  #load(): RecordFile {
    try {
      const j = JSON.parse(readFileSync(this.#path, "utf8")) as Partial<RecordFile>;
      return { requests: Array.isArray(j.requests) ? j.requests : [], runs: Array.isArray(j.runs) ? j.runs : [] };
    } catch {
      return { requests: [], runs: [] };
    }
  }

  #save(f: RecordFile): void {
    mkdirSync(dirname(this.#path), { recursive: true });
    const tmp = this.#path + ".tmp";
    writeFileSync(tmp, JSON.stringify(f, null, 2), { mode: 0o600 });
    renameSync(tmp, this.#path);
  }

  /** LLM API への要求 1 回分。 */
  request(u: Usage): void {
    const f = this.#load();
    f.requests.push({ at: this.#now().toISOString(), model: String(u.model), input_tokens: num(u.input_tokens), output_tokens: num(u.output_tokens), cache_read_input_tokens: num(u.cache_read_input_tokens), elapsed_ms: num(u.elapsed_ms) });
    this.#save(f);
  }

  /** 入力の送信(人の入力 1 つ)。応答か失敗で runEnd を呼ぶ。 */
  runStart(id: string): void {
    this.#started.set(id, this.#now().getTime());
  }

  runEnd(id: string): void {
    const t0 = this.#started.get(id);
    if (t0 === undefined) return;
    this.#started.delete(id);
    const f = this.#load();
    f.runs.push({ at: new Date(t0).toISOString(), elapsed_ms: this.#now().getTime() - t0 });
    this.#save(f);
  }
}
