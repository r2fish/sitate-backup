// 試験用ドライバのコマンドラインの入口(作業指示書 5.14 の完了条件 12、GB-127)。
//   node app/test/e2e/cli.ts --script <台本の JSON> [--variant add|lo|nocluster|base] --out <記録の置き場> [--mode real|fake] [--lang ja] [--name <環境の名前>]
// 入力は台本の JSON(A2 は script.json、A6 は injections.json)、出力は 1 件 1 行の JSON の記録(<out>/record.jsonl)。
// 終了コード: 0 完了 / 1 不合格 / 2 台本に無いカード / 3 無効(H5)。64 は引数の誤り。
// 人の役の値は環境変数 SITATE_ROLE_*(test/acceptance/launch.md)で受ける。値はログにも記録にも出さない。
import { readFileSync } from "node:fs";
import { parseArgs } from "node:util";
import { resolve } from "node:path";
import { startBridge } from "./bridge.ts";
import { A2Loop } from "./a2-loop.ts";
import { appRecordSummary, coreVersion } from "./app-fields.ts";
import { runA6 } from "./a6-loop.ts";
import { makeFakeAgent } from "./fake-agent.ts";
import { launchApp, prepareState, disposeState, keychainItems, appRoot } from "./launch.ts";
import { startLlmEndpoint } from "./llm-endpoint.ts";
import { archiveExisting, Recorder } from "./record.ts";
import { answersFor, readJson, secretsOf } from "./script.ts";
import type { A2Script, InjectionScript } from "./script.ts";
import { addExtra, headerFields, hookWordFrom, loadCatalog, placeDoubles, questionEndings } from "./support.ts";
import type { Outcome } from "./human.ts";

const repo = resolve(appRoot, "..");
const DEFAULT_MAX_MINUTES = 60;
const sleep = (ms: number): Promise<void> => new Promise((r) => setTimeout(r, ms));

function usage(msg: string): never {
  console.error(`driver: ${msg}\n使い方: node app/test/e2e/cli.ts --script <JSON> --out <dir> [--variant add|lo|nocluster|base] [--mode real|fake] [--lang ja] [--name <NAME>] [--state-dir <dir> [--reset]] [--doubles <dir>] [--b2 --preflight(予行: 偽の LLM × 実物の B2。test/wiring/b2-preflight.sh)] [--upstream <url。試験用: 中継先を実物の LLM API から変える>] [--max-minutes N]`);
  process.exit(64);
}

async function main(): Promise<number> {
  const { values: v } = parseArgs({ options: { script: { type: "string" }, variant: { type: "string", default: "base" }, out: { type: "string" }, mode: { type: "string", default: "real" }, lang: { type: "string", default: "ja" },
    name: { type: "string" }, upstream: { type: "string" }, "state-dir": { type: "string" }, reset: { type: "boolean", default: false }, doubles: { type: "string" }, b2: { type: "boolean", default: false }, preflight: { type: "boolean", default: false }, "max-minutes": { type: "string", default: String(DEFAULT_MAX_MINUTES) } } });
  if (v.script === undefined || v.out === undefined) usage("--script と --out が要る");
  if (v.mode !== "real" && v.mode !== "fake") usage("--mode は real か fake");
  if (v.lang !== "ja" && v.lang !== "en") usage("--lang は ja か en");
  if (!["base", "add", "lo", "nocluster"].includes(v.variant!)) usage("--variant は base|add|lo|nocluster");
  if (v.b2 && v.mode !== "fake") usage("--b2 は --mode fake だけ(LLM は偽の口、保管先だけ実物の B2)");
  const name = v.name ?? process.env.SITATE_ROLE_NAME;
  if (name === undefined || name === "") usage("環境の名前が要る(--name か SITATE_ROLE_NAME)");
  const script = readJson<A2Script & InjectionScript>(resolve(v.script));
  const kind = "questions" in script ? "A2" : "A6";
  const answers = answersFor(v.mode, name, repo, process.env, kind === "A2", v.b2);
  const archived = archiveExisting(resolve(v.out));
  if (archived !== null) console.log(`driver: 前の回の記録を退避した: ${archived}`);
  const rec = new Recorder(resolve(v.out), secretsOf(answers));
  const catalog = loadCatalog(repo, v.lang);
  rec.write("header", { driver: kind, mode: v.mode, variant: v.variant, lang: v.lang, script_version: script.version, ...headerFields(repo) });
  const doubles = v.doubles === undefined ? async () => undefined : () => placeDoubles(resolve(v.doubles!));
  const llm = await startLlmEndpoint(v.mode === "fake"
    ? { mode: "fake", rec, expectKey: answers.llmKey, fake: makeFakeAgent(answers, doubles, kind === "A6" ? "ご依頼の操作に使える道具はありません。" : undefined, { adapter: v.b2 ? "b2" : "minio", extras: v.preflight }) }
    : { mode: "proxy", rec, expectKey: answers.llmKey, ...(v.upstream === undefined ? {} : { upstream: v.upstream }) });
  const bridge = await startBridge(answers.containers);
  const state = prepareState(v["state-dir"] === undefined ? undefined : resolve(v["state-dir"]), v.reset);
  // Ctrl-C・終了の合図でも、LLM キー入りの使い捨ての keychain とユーザーデータの置き場を消してから終わる(終了コード 130)。
  const onSignal = (): void => { try { disposeState(state); } finally { bridge.close(); process.exit(130); } };
  process.once("SIGINT", onSignal);
  process.once("SIGTERM", onSignal);
  const app = await launchApp({ state, llmBaseUrl: llm.url, hostMap: bridge.hostMap, lang: v.lang }).catch(async (e: unknown) => { bridge.close(); await llm.close(); disposeState(state); throw e; });
  const t0 = Date.now();
  let out: Outcome;
  let appLog = "";
  try {
    const deadlineMs = Number(v["max-minutes"]) * 60000;
    out = kind === "A2"
      ? await runA2(script, answers, rec, app.driver, { catalog, variant: v.variant!, name, deadlineMs, userData: state.userData, backup: answers.containers.backup })
      : await runA6({ port: app.driver, script, answers, rec, catalog, sleep, now: Date.now, deadlineMs });
  } catch (e) {
    out = { exit: 1, reason: "ドライバの内部エラー: " + String(e).slice(0, 300) };
  } finally {
    rec.write("keychain_items", { prefix: "jp.sitate-backup.app", items: keychainItems(state) });
    appLog = app.log();
    await app.stop(); bridge.close(); await llm.close(); disposeState(state);
    process.off("SIGINT", onSignal); process.off("SIGTERM", onSignal);
  }
  out = leakCheck(out, rec, secretsOf(answers), appLog, llm.keyMismatches());
  rec.write("summary", { exit: out.exit, reason: out.reason, elapsed_ms: Date.now() - t0, llm_requests: llm.requests(), secret_redactions: rec.redactions, counts: Object.fromEntries(rec.counts) });
  console.log(`driver: ${kind} 終了コード ${out.exit}: ${out.reason}(記録: ${rec.path})`);
  return out.exit;
}

/** 偽の(または実の)人の役の値が、記録のファイルとアプリのログに出ていないことを、書き終えた記録そのものから確かめる(そのままの文字列と base64)。 */
function leakCheck(out: Outcome, rec: Recorder, secrets: string[], appLog: string, keyMismatches: number): Outcome {
  const text = readFileSync(rec.path, "utf8");
  const forms = secrets.flatMap((s) => [s, JSON.stringify(s).slice(1, -1), Buffer.from(s).toString("base64")]);
  const inRecord = forms.filter((f) => text.includes(f)).length;
  const inLog = forms.filter((f) => appLog.includes(f)).length;
  rec.write("secret_check", { record_hits: inRecord, app_log_hits: inLog, redactions: rec.redactions, llm_key_mismatches: keyMismatches });
  if (out.exit !== 0) return out;
  if (inRecord + inLog + rec.redactions > 0) return { exit: 1, reason: "人の役の秘密の値が記録かアプリのログに出た(または出かけて伏せた)" };
  return keyMismatches > 0 ? { exit: 1, reason: "アプリが SDK へ渡した LLM API キーが、入力ボックスに入れた値と違う" } : out;
}

async function runA2(script: A2Script, answers: ReturnType<typeof answersFor>, rec: Recorder, port: import("./port.ts").Port, o: { catalog: Record<string, string>; variant: string; name: string; deadlineMs: number; userData: string; backup: string }): Promise<Outcome> {
  const loop = new A2Loop({ port, script, answers, rec, variant: o.variant, endings: questionEndings(repo), catalog: o.catalog,
    hookWord: (r) => hookWordFrom(answers.hookRecordsFile, r), envOp: async (k) => addExtra(repo, o.name, k), sleep, now: Date.now, deadlineMs: o.deadlineMs });
  const out = await loop.run();
  const core = coreVersion(o.backup); // 回の終わりの核の版(導入の途中で入るので、初めには取れない)。取れなくても判定は変えない
  const c = loop.counts;
  rec.write("a2_fields", { steps_passed: [...loop.stepsPassed].sort((a, b) => a - b), questions: c.questions, unscripted_questions: c.unscripted, continue_interventions: c.continues, turn_limit_inputs: c.turnLimit,
    human_work: [...loop.humanWork].sort(), human_work_count: loop.humanWork.size, tool_calls: c.toolCalls, withheld_replies: c.withheld, question_not_last: c.questionNotLast, multi_question: c.multiQuestion, repeated_requests: c.repeatedRequests, status: loop.status, self_recovery: loop.recovery(),
    core_version: core, app_record: appRecordSummary(o.userData) });
  return out;
}

main().then((code) => { process.exitCode = code; }, (e: unknown) => { console.error("driver: " + String(e)); process.exitCode = 1; });
