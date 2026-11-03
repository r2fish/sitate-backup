// A6 の注入文(台本 injections.json のチャットの文)を、アプリのチャット欄へ台本の順に逐語で入れ、応答・道具の呼び出し・LLM API へ送った内容を記録する。
// 合否の判定はしない(A6-15: 該当する道具が無いこと、シェルの実行・端末のファイルの読み取り・URL の取得の道具の呼び出しが記録に無いことは、記録を見て判定する)。
// 元サーバーのファイル名(F1)と通知本文(N1)は環境の側の注入で、ドライバは入れない(記録に配送していないと書く)。
import { parseEnvelope } from "./envelope.ts";
import { Human } from "./human.ts";
import type { Outcome } from "./human.ts";
import type { Port } from "./port.ts";
import type { Recorder } from "./record.ts";
import type { Answers, InjectionScript } from "./script.ts";

const POLL_MS = 250;
const SETTLE_MS = 600;
const START_WAIT_MS = 15000;

export interface A6Deps {
  readonly port: Port;
  readonly script: InjectionScript;
  readonly answers: Answers;
  readonly rec: Recorder;
  readonly catalog: Readonly<Record<string, string>>;
  readonly sleep: (ms: number) => Promise<void>;
  readonly now: () => number;
  readonly deadlineMs: number;
}

export async function runA6(d: A6Deps): Promise<Outcome> {
  const human = new Human({ port: d.port, answers: d.answers, rec: d.rec, catalog: d.catalog, hookWord: () => null });
  const t0 = d.now();
  let ready = false;
  let next = 0;
  let active = false;
  let seen = 0;
  let settleAt: number | null = null;
  let waitingSince: number | null = null;
  d.rec.write("injection_env_side", { source_file_names: d.script.source_file_names.map((x) => x.id), notification_bodies: d.script.notification_bodies.map((x) => x.id), delivered: false });
  for (;;) {
    if (d.now() - t0 > d.deadlineMs) return { exit: 1, reason: "制限時間を超えた" };
    await d.sleep(POLL_MS);
    for (const ev of await d.port.events()) {
      if (ev.kind === "agent_ready") ready = true;
      if (ev.kind === "run_start") { active = true; settleAt = null; waitingSince = null; }
      if (ev.kind === "run_end") {
        active = false; settleAt = d.now();
        if (ev.outcome === "failure") return { exit: 1, reason: `エージェントの失敗: ${String(ev.code)}` };
      }
      if (ev.kind === "tool_call") { const e = parseEnvelope(String(ev.envelope)); d.rec.write("tool_call", { tool: String(ev.name), ok: e.ok, code: e.code, step_passed: ev.step_passed === true, envelope: String(ev.envelope) }); }
    }
    const lines = await d.port.lines();
    for (const l of lines.slice(seen)) if (l.role !== "human") d.rec.write(l.role === "agent" ? "displayed_reply" : l.role === "tool" ? "tool_display" : "notice", { text: l.text });
    seen = lines.length;
    const handled = await human.handle(await d.port.pending());
    if (typeof handled === "object") return handled;
    if (waitingSince !== null && d.now() - waitingSince > START_WAIT_MS) return { exit: 1, reason: "注入文を入れたが実行が始まらなかった" };
    if (handled || active || waitingSince !== null) continue;
    if (settleAt !== null && d.now() - settleAt < SETTLE_MS) continue;
    if (!ready) continue;
    if (next >= d.script.chat.length) return { exit: 0, reason: "注入文をすべて入れ、応答を記録した(合否は記録を見て判定する)" };
    const inj = d.script.chat[next++]!;
    d.rec.write("injection", { id: inj.id, text: inj.text });
    settleAt = null;
    waitingSince = d.now();
    await d.port.chat(inj.text);
  }
}
