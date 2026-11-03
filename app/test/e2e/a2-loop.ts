// A2 の駆動(作業指示書 5.14 の完了条件 12、8 章「A2 の記録の欄と試験用ドライバの規則」)。
// 画面(チャット欄の行、人の操作の面の状態)と主プロセスの出来事(道具の呼び出し、実行の始まりと終わり)を見て、台本の固定の入力を入れる。
//  - 問い: 導入の完了の前に入力待ちに入り、直前の応答の最後の文が問いの終わり方のもの。台本の Q の語に当たれば Q の答え、当たらなければ固定の答え(問いとして数える)。
//    同じ Q の続き(Q4 の示し直し、Q1 の入れ直し)は問いの回数に足さない。
//  - 答えを求めない停止: 固定の続行の入力を入れ、続行の介入として別に数える(問いに数えない)。
//  - AGENT_TURN_LIMIT: 台本の続けるための入力。台本に無いカード・秘密の入力ボックスは終了コード 2。H5 は終了コード 3。
import { isH5, judgeStatus, parseEnvelope } from "./envelope.ts";
import type { Envelope } from "./envelope.ts";
import { Human } from "./human.ts";
import type { HumanDeps, Outcome } from "./human.ts";
import type { Port } from "./port.ts";
import type { Recorder } from "./record.ts";
import { initialMessage, isQuestion, matchQuestion } from "./script.ts";
import type { A2Script, Answers, QId } from "./script.ts";
import type { TestEvent } from "../../src/testing/driver-server.ts";

const POLL_MS = 250;
const SETTLE_MS = 600;
const START_WAIT_MS = 15000;
const MAX_INTERVENTIONS = 40;
const LAST_STEP = 13;

export interface LoopDeps {
  readonly port: Port;
  readonly script: A2Script;
  readonly answers: Answers;
  readonly rec: Recorder;
  readonly variant: string;
  readonly endings: readonly string[];
  readonly catalog: Readonly<Record<string, string>>;
  readonly hookWord: (recipient: string) => string | null;
  /** 環境の操作(変種 add の add-extra mount / db)。 */
  readonly envOp: (kind: "mount" | "db") => Promise<void>;
  readonly sleep: (ms: number) => Promise<void>;
  readonly now: () => number;
  readonly deadlineMs: number;
}

type Mark = "fail" | "pass" | "intervene";

export class A2Loop {
  readonly #d: LoopDeps;
  readonly #human: Human;
  readonly stepsPassed = new Set<number>();
  readonly marks: Mark[] = [];
  readonly counts = { questions: 0, unscripted: 0, continues: 0, turnLimit: 0, toolCalls: 0, withheld: 0 };
  status: ReturnType<typeof judgeStatus> | null = null;
  #lines = 0;
  #active = false;
  #ready = false;
  #started = false;
  #startedAt = 0;
  #expectUntil = 0;
  #idleAt: number | null = null;
  #lastRun: TestEvent | null = null;
  #lastQ: QId | null = null;
  #q4: "none" | "wished" | "accepted" = "none";
  #surveyed = false;
  #lastAgent = "";

  constructor(d: LoopDeps) {
    this.#d = d;
    this.#human = new Human({ port: d.port, answers: d.answers, rec: d.rec, catalog: d.catalog, hookWord: d.hookWord } satisfies HumanDeps);
  }

  get humanWork(): ReadonlySet<string> { return this.#human.humanWork; }

  async run(): Promise<Outcome> {
    const t0 = this.#d.now();
    for (;;) {
      if (this.#d.now() - t0 > this.#d.deadlineMs) return { exit: 1, reason: "制限時間を超えた" };
      await this.#d.sleep(POLL_MS);
      const out = await this.#tick();
      if (out !== null) return out;
    }
  }

  async #tick(): Promise<Outcome | null> {
    const stop = await this.#drainEvents();
    if (stop !== null) return stop;
    await this.#drainLines();
    const handled = await this.#human.handle(await this.#d.port.pending());
    if (typeof handled === "object") return handled;
    if (handled) return null;
    return this.#idle();
  }

  async #drainEvents(): Promise<Outcome | null> {
    for (const ev of await this.#d.port.events()) {
      const stop = await this.#onEvent(ev);
      if (stop !== null) return stop;
    }
    return null;
  }

  async #onEvent(ev: TestEvent): Promise<Outcome | null> {
    switch (ev.kind) {
      case "agent_ready": this.#ready = true; this.#d.rec.write("agent_ready", { model: ev.model }); return null;
      case "run_start": this.#active = true; this.#idleAt = null; this.#started = true; this.#d.rec.write("run_start", { id: ev.id }); return null;
      case "run_end": return this.#onRunEnd(ev);
      case "tool_call": return this.#onToolCall(ev);
      default: return null;
    }
  }

  #onRunEnd(ev: TestEvent): Outcome | null {
    this.#active = false;
    this.#lastRun = ev;
    this.#idleAt = this.#d.now();
    if (ev.outcome === "reply") { this.#d.rec.write("run_end", { id: ev.id, outcome: "reply" }); return null; }
    this.#d.rec.write("run_end", { id: ev.id, outcome: "failure", code: ev.code });
    return ev.code === "AGENT_TURN_LIMIT" ? null : { exit: 1, reason: `エージェントの失敗: ${String(ev.code)}` };
  }

  async #onToolCall(ev: TestEvent): Promise<Outcome | null> {
    const name = String(ev.name);
    const env: Envelope = parseEnvelope(String(ev.envelope));
    this.counts.toolCalls += 1;
    if (env.ok === false) this.marks.push("fail");
    this.#d.rec.write("tool_call", { tool: name, ok: env.ok, code: env.code, step_passed: ev.step_passed === true, envelope: String(ev.envelope) });
    if (name === "sitate_status" && env.ok === true) this.status = judgeStatus(env);
    if (isH5(name, env)) return { exit: 3, reason: "H5 が起きた(元サーバーに道具が無い)。この回は無効" };
    if (name === "sitate_source_survey" && env.ok === true && !this.#surveyed) {
      this.#surveyed = true;
      if (this.#d.variant === "add") { await this.#d.envOp("mount"); await this.#d.envOp("db"); this.#d.rec.write("env_op", { op: "add-extra", kinds: ["mount", "db"] }); }
    }
    return null;
  }

  async #drainLines(): Promise<void> {
    const lines = await this.#d.port.lines();
    for (const l of lines.slice(this.#lines)) {
      if (l.role === "agent") { this.#lastAgent = l.text; this.#d.rec.write("displayed_reply", { text: l.text }); }
      else if (l.role === "tool") this.#onToolLine(l.text);
      else if (l.role === "notice") this.#onNotice(l.text);
    }
    this.#lines = lines.length;
  }

  #onToolLine(text: string): void {
    this.#d.rec.write("tool_display", { text });
    const m = /手順 (\d+) が合格しました/.exec(text);
    if (m !== null) { this.stepsPassed.add(Number(m[1])); this.marks.push("pass"); this.#d.rec.write("step_passed", { step: Number(m[1]) }); }
  }

  #onNotice(text: string): void {
    if (text === this.#d.catalog["chat.quote.rejected_guide"]) {
      this.counts.withheld += 1;
      this.#d.rec.write("withheld_reply", { text: String((this.#lastRun?.text as string | undefined) ?? "") });
    } else this.#d.rec.write("notice", { text });
  }

  async #idle(): Promise<Outcome | null> {
    const now = this.#d.now();
    if (!this.#started) return this.#boot();
    if (this.#active) return null;
    if (this.#idleAt === null) {
      if (now > this.#expectUntil) return { exit: 1, reason: "入力を送ったが実行が始まらなかった" };
      return null;
    }
    if (now - this.#idleAt < SETTLE_MS) return null;
    this.#idleAt = null;
    return this.#afterRun();
  }

  /** 起動: エージェントの準備ができたら、開始時の入力を入れる。 */
  async #boot(): Promise<Outcome | null> {
    if (!this.#ready) return null;
    if (this.#startedAt !== 0) return this.#d.now() > this.#expectUntil ? { exit: 1, reason: "開始時の入力で実行が始まらなかった" } : null;
    this.#startedAt = this.#d.now();
    const text = initialMessage(this.#d.script, this.#d.answers);
    this.#d.rec.write("start_input", { text });
    await this.#d.port.chat(text);
    this.#expectUntil = this.#d.now() + START_WAIT_MS;
    return null;
  }

  async #afterRun(): Promise<Outcome | null> {
    await this.#drainLines();
    if (this.stepsPassed.has(LAST_STEP)) return this.#finish();
    if (this.counts.continues + this.counts.turnLimit + this.counts.unscripted > MAX_INTERVENTIONS) return { exit: 1, reason: "進まない(入力の回数が上限を超えた)" };
    if (this.#lastRun?.outcome === "failure") return this.#say("turn_limit_input", this.#d.script.turn_limit_input, () => { this.counts.turnLimit += 1; });
    const reply = this.#lastAgent;
    if (!isQuestion(reply, this.#d.endings)) return this.#say("continue_intervention", this.#d.script.continue_input, () => { this.counts.continues += 1; this.marks.push("intervene"); });
    return this.#answer(reply);
  }

  async #answer(reply: string): Promise<Outcome | null> {
    const s = this.#d.script;
    let q = matchQuestion(reply, s);
    if (q === null && this.#lastQ === "Q4" && this.#q4 !== "none") q = "Q4";
    const continued = q !== null && q === this.#lastQ;
    if (q === null) { this.counts.unscripted += 1; this.counts.questions += 1; this.marks.push("intervene"); return this.#say("question", s.unscripted_question_answer, undefined, { q: null, continued: false }); }
    if (!continued) this.counts.questions += 1;
    this.marks.push("intervene");
    this.#lastQ = q;
    return this.#say("question", this.#answerOf(q), undefined, { q, continued });
  }

  #answerOf(q: QId): string {
    const a = this.#d.answers;
    const s = this.#d.script.questions;
    switch (q) {
      case "Q1": return `保管先のエンドポイントは ${a.storageEndpoint}、バケット名は ${a.bucket} です。管理用鍵と通知先の URL は、秘密の入力ボックスから入れます。`;
      case "Q2": return s.Q2.answer!;
      case "Q3": return `復元先は ${a.destHost}(SSH ${a.destPort} 番)、管理アカウントは ${a.adminUser} です。資格情報は秘密の入力ボックスから入れます。`;
      case "Q4": {
        if (this.#q4 === "none") { this.#q4 = "wished"; return s.Q4.answers![this.#d.variant === "base" ? "add_after_survey" : variantKey(this.#d.variant)]!; }
        this.#q4 = "accepted";
        return s.Q4.accept_proposal!;
      }
    }
  }

  async #say(kind: string, text: string, after?: () => void, extra: Record<string, unknown> = {}): Promise<Outcome | null> {
    after?.();
    this.#d.rec.write(kind, { text, ...extra });
    await this.#d.port.chat(text);
    this.#expectUntil = this.#d.now() + START_WAIT_MS;
    return null;
  }

  #finish(): Outcome {
    const missing = Array.from({ length: LAST_STEP }, (_, i) => i + 1).filter((n) => !this.stepsPassed.has(n));
    if (missing.length > 0) return { exit: 1, reason: `手順 ${missing.join(",")} が手順書の判定で合格していない` };
    const s = this.status;
    if (s === null || !s.lastBackupOk || !s.lastRestoreTestOk || !s.proofConfirmed) return { exit: 1, reason: "status が /result/last_backup/ok・/result/last_restore_test/ok・/result/notify/proof_confirmed の全部を true で返していない" };
    if (this.humanWork.size > 4) return { exit: 1, reason: `人の作業が 4 個を超えた(${this.humanWork.size})` };
    return { exit: 0, reason: "導入が完了し、機械で判定する項目を満たした" };
  }

  /** 自力で立て直せた割合: ok:false の後、問い・人の作業・入力の前に次の手順の合格が記録された割合。 */
  recovery(): { failures: number; recovered: number } {
    let failures = 0;
    let recovered = 0;
    for (let i = 0; i < this.marks.length; i++) {
      if (this.marks[i] !== "fail") continue;
      failures += 1;
      const next = this.marks.slice(i + 1).find((m) => m !== "fail");
      if (next === "pass") recovered += 1;
    }
    return { failures, recovered };
  }
}

const variantKey = (v: string): string => (v === "lo" ? "large_object" : v === "nocluster" ? "decline_cluster" : "add_after_survey");
