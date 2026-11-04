// 画面写しの撮影(作業指示書 5.14c の 7)。偽の LLM で A2 を最後まで進め、各面が出た時点で明と暗の PNG を test/records/U3c/ に残す。
//   node app/test/shots/cli.ts --name <環境の名前> [--variant add] [--out <PNG の置き場。test/records/U3c/ の下>] [--card-text <承認カードの文を書く先>]
// e2e のモジュール(起動・偽の LLM・環境の橋渡し・A2 の駆動)を import して使うだけで、app/test/e2e/ は変えない。値は偽の LLM と受入試験の環境の合成値だけ。
// フレーズのパネルは、撮る前に語の欄を隠す(試験ビルドの mask)。隠れていなければ capture が失敗し、PNG を作らない。
import { connect } from "node:net";
import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { parseArgs } from "node:util";
import { A2Loop } from "../e2e/a2-loop.ts";
import { startBridge } from "../e2e/bridge.ts";
import type { Driver } from "../e2e/driver.ts";
import type { PendingView } from "../e2e/driver.ts";
import { makeFakeAgent } from "../e2e/fake-agent.ts";
import { launchApp, prepareState, disposeState, appRoot } from "../e2e/launch.ts";
import { startLlmEndpoint } from "../e2e/llm-endpoint.ts";
import { Recorder } from "../e2e/record.ts";
import type { Port } from "../e2e/port.ts";
import { answersFor, readJson, secretsOf } from "../e2e/script.ts";
import type { A2Script } from "../e2e/script.ts";
import { addExtra, hookWordFrom, loadCatalog, placeDoubles, questionEndings } from "../e2e/support.ts";
import type { TestEvent } from "../../src/testing/driver-server.ts";

const repo = resolve(appRoot, "..");
const sleep = (ms: number): Promise<void> => new Promise((r) => setTimeout(r, ms));
const THEMES = ["light", "dark"] as const;

/** Driver の別の接続。#send は private なので、同じ線の約束(1 行 1 命令の JSON)で自前に送る。 */
function rawClient(path: string): Promise<{ call(c: unknown): Promise<unknown>; close(): void }> {
  return new Promise((res, rej) => {
    const s = connect(path, () => {
      let buf = "";
      const waiters: ((r: { result?: unknown; error?: string }) => void)[] = [];
      s.setEncoding("utf8");
      s.on("data", (d: string) => { buf += d; for (let nl = buf.indexOf("\n"); nl >= 0; nl = buf.indexOf("\n")) { const l = buf.slice(0, nl); buf = buf.slice(nl + 1); waiters.shift()?.(JSON.parse(l)); } });
      res({
        call: async (c) => { const p = new Promise<{ result?: unknown; error?: string }>((r) => waiters.push(r)); s.write(JSON.stringify(c) + "\n"); const r = await p; if (r.error !== undefined) throw new Error(r.error); return r.result; },
        close: () => s.end(),
      });
    });
    s.on("error", rej);
  });
}

async function main(): Promise<number> {
  const { values: v } = parseArgs({ options: { name: { type: "string" }, variant: { type: "string", default: "add" }, out: { type: "string", default: resolve(repo, "test/records/U3c") }, "card-text": { type: "string" } } });
  if (v.name === undefined) throw new Error("--name が要る");
  const out = resolve(v.out!);
  const script = readJson<A2Script>(resolve(repo, "test/acceptance/scripts/A2/script.json"));
  const answers = answersFor("fake", v.name, repo, process.env, true, false);
  const rec = new Recorder(resolve(repo, "dist", "shots-rec"), secretsOf(answers));
  const catalog = loadCatalog(repo, "ja");
  const doubles = () => placeDoubles(resolve(repo, "dist/test/wiring"));
  const llm = await startLlmEndpoint({ mode: "fake", rec, expectKey: answers.llmKey, fake: makeFakeAgent(answers, doubles, undefined, { adapter: "minio", extras: false, declineSrv: false }) });
  const bridge = await startBridge(answers.containers);
  const state = prepareState(undefined, false);
  const app = await launchApp({ state, llmBaseUrl: llm.url, hostMap: bridge.hostMap, lang: "ja" });
  const raw = await rawClient(resolve(state.dir, "d.sock"));
  const send = (c: Record<string, unknown>): Promise<unknown> => raw.call(c);
  const shots: string[] = [];
  const shoot = async (name: string): Promise<void> => { for (const theme of THEMES) { await send({ op: "capture", path: `${out}/${name}-${theme}.png`, theme }); shots.push(`${name}-${theme}.png`); } };
  const done = new Set<string>();
  let waitDone = 0;
  const port: Port = {
    ...bind(app.driver),
    async lines() {
      const ls = await app.driver.lines();
      if (waitDone < THEMES.length && ls.filter((l) => l.role === "tool").length >= 3) {
        const theme = THEMES[waitDone]!;
        if ((await send({ op: "capture", path: `${out}/chat-installing-${theme}.png`, theme, require: "wait" })) === true) { shots.push(`chat-installing-${theme}.png`); waitDone++; }
      }
      if (!done.has("chat") && ls.length >= 2) { done.add("chat"); await shoot("chat"); }
      return ls;
    },
    async pending() {
      const p: PendingView = await app.driver.pending();
      if (p.secret !== null && !done.has("secret")) { done.add("secret"); await shoot("secret-input"); }
      if (p.card !== null && !done.has("card")) {
        done.add("card");
        if (v["card-text"] !== undefined) writeFileSync(v["card-text"], JSON.stringify({ text: p.card.text, actions: p.card.actions }, null, 1) + "\n");
        await shoot("approval-card");
      }
      if (p.phrase !== null && !done.has("phrase")) { done.add("phrase"); await send({ op: "mask" }); await shoot("phrase-panel"); }
      return p;
    },
  };
  let exit = 1;
  try {
    const loop = new A2Loop({ port, script, answers, rec, variant: v.variant!, endings: questionEndings(repo), catalog,
      hookWord: (r) => hookWordFrom(answers.hookRecordsFile, r), envOp: async (k) => addExtra(repo, v.name!, k), sleep, now: Date.now, deadlineMs: 60 * 60000 });
    const o = await loop.run();
    exit = o.exit;
    console.log(`shots: A2 終了コード ${o.exit}: ${o.reason}`);
    await shoot("status-and-report");
  } finally {
    raw.close();
    await app.stop(); bridge.close(); await llm.close(); disposeState(state);
  }
  console.log("shots: " + shots.join(" "));
  return exit;
}

function bind(d: Driver): Pick<Port, "events" | "chat" | "fill" | "click"> {
  return { events: (): Promise<TestEvent[]> => d.events(), chat: (t) => d.chat(t), fill: (s, t, x) => d.fill(s, t, x), click: (s, a) => d.click(s, a) };
}

main().then((c) => { process.exitCode = c; }, (e: unknown) => { console.error("shots: " + String(e)); process.exitCode = 1; });
