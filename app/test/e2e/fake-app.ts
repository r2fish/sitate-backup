// 試験用の偽のアプリ(Port の実装): 人の入力(chat)ごとに、台本の「エージェントの番」(出来事、チャット欄の行、開く面)を進める。
// ドライバの規則(問いの数え方、続行の介入、台本に無いカード、H5 など)を、実物の Electron なしで確かめるために使う。
import type { PendingView } from "./driver.ts";
import type { Port } from "./port.ts";
import type { TestEvent } from "../../src/testing/driver-server.ts";

export interface AgentTurn {
  /** この番で起きる出来事(run_start / run_end は自動で付く。run_end の形は end で決める)。 */
  readonly events?: Record<string, unknown>[];
  readonly lines?: { role: string; text: string }[];
  /** 番の途中で開く面(ドライバが処理するまで run_end を出さない)。 */
  readonly surface?: Partial<PendingView>;
  readonly end?: { outcome: "reply"; text?: string } | { outcome: "failure"; code: string };
}

export const envelope = (command: string, body: Record<string, unknown>): string =>
  `<sitate-data command="${command}">\n${JSON.stringify({ schema: "sitate.out/v1", command: [command], ok: true, error: null, warnings: [], result: {}, ...body })}\n</sitate-data>`;
export const toolCall = (name: string, body: Record<string, unknown> = {}, stepPassed = false): Record<string, unknown> => ({ kind: "tool_call", name, envelope: envelope(name, body), step_passed: stepPassed });

export class FakeApp implements Port {
  readonly chats: string[] = [];
  readonly clicks: string[] = [];
  readonly fills: { surface: string; target: string }[] = [];
  readonly #turns: AgentTurn[];
  #turn = 0;
  readonly #queue: TestEvent[] = [{ at: "", kind: "agent_ready", model: "m" }];
  readonly #lines: { role: string; text: string }[] = [];
  #pending: PendingView = { secret: null, card: null, phrase: null };
  #waiting: AgentTurn | null = null;

  constructor(turns: AgentTurn[]) { this.#turns = turns; }

  async chat(text: string): Promise<void> {
    this.chats.push(text);
    this.#lines.push({ role: "human", text });
    const t = this.#turns[this.#turn++];
    if (t === undefined) throw new Error("偽のアプリ: 台本の番が尽きた");
    this.#queue.push({ at: "", kind: "run_start", id: `i${this.#turn}` });
    if (t.surface !== undefined) { this.#pending = { secret: null, card: null, phrase: null, ...t.surface }; this.#waiting = t; return; }
    this.#finish(t);
  }

  #finish(t: AgentTurn): void {
    for (const e of t.events ?? []) this.#queue.push({ at: "", ...e } as TestEvent);
    this.#lines.push(...(t.lines ?? []));
    const end = t.end ?? { outcome: "reply" as const };
    this.#queue.push({ at: "", kind: "run_end", id: `i${this.#turn}`, ...end });
  }

  async events(): Promise<TestEvent[]> { return this.#queue.splice(0); }
  async lines(): Promise<{ role: string; text: string }[]> { return [...this.#lines]; }
  async pending(): Promise<PendingView> { return this.#pending; }
  async fill(surface: string, target: string): Promise<boolean> { this.fills.push({ surface, target }); return true; }

  async click(surface: string, action: string): Promise<boolean> {
    this.clicks.push(`${surface}:${action}`);
    const open = this.#pending;
    const closes = (surface === "secret-box" && open.secret !== null) || (surface === "approval-card" && open.card !== null) || (surface === "phrase-panel" && action === "confirm");
    if (closes) {
      this.#pending = { secret: null, card: null, phrase: null };
      if (this.#waiting !== null) { const t = this.#waiting; this.#waiting = null; this.#finish(t); }
    }
    return true;
  }
}
