// 状態と報告の表示(完了条件 10・23・32・35)、手順の合格の判定と往復の数え直し(30)、アプリの原因コードの detail と次の一手(34)。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { AgentFailure, createAgentRuntime } from "../../src/agent/runtime.ts";
import { buildSystemBlocks } from "../../src/agent/system-prompt.ts";
import { SoftwareApprovalKey } from "../../src/main/approval-sign.ts";
import { MemoryKeychain, serviceFor } from "../../src/main/keychain.ts";
import { PathRefTable } from "../../src/main/path-ref.ts";
import { Session } from "../../src/main/session.ts";
import type { ChatLine, CoreRunner } from "../../src/main/session.ts";
import { SshError } from "../../src/main/ssh/connection.ts";
import { StatusView } from "../../src/main/status-view.ts";
import { StepJudge, parseSteps } from "../../src/main/step-judge.ts";
import { ToolRegistry } from "../../src/main/tool-registry.ts";
import { fakeLlm } from "./fake-llm.ts";

const catalog = JSON.parse(readFileSync(new URL("../../src/i18n/ja/catalog.json", import.meta.url), "utf8")) as Record<string, string>;
const keys = JSON.parse(readFileSync(new URL("../../src/i18n/keys.json", import.meta.url), "utf8")) as Record<string, unknown>;
const t = (key: string, args?: Record<string, unknown>): string => { if (!Object.hasOwn(keys, key)) throw new Error("no key " + key); return catalog[key]!.replace(/\{([a-z_]+)[^}]*\}/g, (_m, n: string) => String(args?.[n] ?? "")); };
const registry = ToolRegistry.fromDir(join(import.meta.dirname, "..", "..", "src", "tools"));
const NOW = new Date("2026-11-03T12:00:00Z");
const status = (over: Record<string, unknown> = {}) => ({ schema: "sitate.out/v1", command: ["status"], ok: true, result: { last_backup: { at: "2026-11-03T02:00:00Z", ok: true, checks_count: 0 }, last_restore_test: { at: "2026-11-01T02:00:00Z", ok: true, days_since: 2 }, hold: { active: false }, warnings: [], uncovered_count: 0, ...over } });

function view(running = true) {
  const notes: string[] = [];
  let now = NOW;
  const v = new StatusView({ t, now: () => now, agentRunning: () => running, notify: (s) => notes.push(s) });
  return { v, notes, setNow: (d: Date) => { now = d; } };
}

test("status: AI-assessment operation appears per change kind, only on a change, with the fixed catalog text, and never calls the model unless pressed", () => {
  const cases: [string, Record<string, unknown>, string][] = [
    ["warning_new", { warnings: [{ code: "X_NEW" }] }, "status.ask_ai.input.warning_new"],
    ["hold_active", { hold: { active: true } }, "status.ask_ai.input.hold_active"],
    ["last_ok_false", { last_backup: { at: "2026-11-03T02:00:00Z", ok: false, checks_count: 1 } }, "status.ask_ai.input.last_ok_false"],
    ["last_ok_false", { last_restore_test: { at: "2026-11-01T02:00:00Z", ok: false, days_since: 2 } }, "status.ask_ai.input.last_ok_false"],
    ["uncovered_increase", { uncovered_count: 3 }, "status.ask_ai.input.uncovered_increase"],
  ];
  for (const [kind, over, key] of cases) {
    const { v } = view();
    v.ingest(status());
    assert.deepEqual(v.view().askAi, [], "最初の読みには操作を置かない");
    v.ingest(status());
    assert.deepEqual(v.view().askAi, [], "変化の無い読み直しでは出ない");
    v.ingest(status(over));
    assert.deepEqual(v.view().askAi.map((a) => a.kind), [kind]);
    assert.equal(v.press(kind), t(key));
    assert.equal(v.press(kind), null, "押した後は消える");
  }
  const held = view();
  held.v.ingest(status({ hold: { active: true } }));
  held.v.ingest(status({ hold: { active: true } }));
  assert.deepEqual(held.v.view().askAi, [], "すでに有効のまま読み直しても出さない(有効に変わったときだけ)");
  const { v } = view(false);
  v.ingest(status());
  v.ingest(status({ hold: { active: true } }));
  assert.deepEqual(v.view().askAi, [], "エージェントが動かない間は出さない");
  assert.equal(v.press("hold_active"), null);
});

test("status: silence is entered at the threshold, notified once per day while it continues, and released when back inside", () => {
  const { v, notes, setNow } = view();
  v.ingest(status());
  assert.equal(v.silent, false);
  setNow(new Date("2026-11-06T12:00:00Z"));
  v.ingest(status());
  assert.equal(v.silent, true);
  assert.deepEqual(v.view().askAi.map((a) => a.kind), ["silence"]);
  assert.equal(notes.length, 1);
  setNow(new Date("2026-11-06T18:00:00Z"));
  v.ingest(status());
  assert.equal(notes.length, 1, "続く間は 1 日に 1 回まで");
  setNow(new Date("2026-11-07T19:00:00Z"));
  v.ingest(status());
  assert.equal(notes.length, 2);
  v.ingest(status({ last_backup: { at: "2026-11-07T18:00:00Z", ok: true, checks_count: 0 } }));
  assert.equal(v.silent, false);
  const r = view();
  r.v.ingest(status({ last_restore_test: { at: "2026-09-01T00:00:00Z", ok: true, days_since: 36 } }));
  assert.equal(r.v.silent, true, "復元テストの経過日数が 35 日を超えたとき");
});

test("status: the fixed sentence for zero checks shows only when checks_count is 0; unreachable keeps the last value with a stale mark", () => {
  const { v } = view();
  v.ingest(status());
  assert.ok(v.view().rows.some((r) => r.label === t("status.row.checks_count_zero")));
  v.ingest(status({ last_backup: { at: "2026-11-03T02:00:00Z", ok: true, checks_count: 4 } }));
  assert.ok(!v.view().rows.some((r) => r.label === t("status.row.checks_count_zero")));
  v.ingest(null);
  const rows = v.view().rows;
  assert.equal(rows[0]!.label, t("status.unreachable.fixed"));
  assert.ok(rows.some((r) => r.label === t("status.row.last_backup") && r.value.includes(t("status.stale_mark"))));
  const m = view();
  m.v.ingest(status());
  m.v.setModel("claude-sonnet-5", false);
  assert.ok(m.v.view().appInfo.some((r) => r.label.includes("claude-sonnet-5") && r.value === t("status.model_not_first_mark")));
  m.v.setModel("claude-sonnet-5-5", true);
  assert.ok(m.v.view().appInfo.some((r) => r.label.includes("claude-sonnet-5-5") && r.value === ""));
  m.v.setModel(null, true);
  assert.ok(m.v.view().appInfo.some((r) => r.label === t("status.llm.model_missing")));
});

function sessionWith(respond: (argv: readonly string[]) => string | Error, running = true) {
  const kc = new MemoryKeychain();
  const lines: ChatLine[] = [];
  const sent: string[] = [];
  const calls: string[][] = [];
  const runner: CoreRunner = { async run(argv) { calls.push([...argv]); const r = respond(argv); if (r instanceof Error) throw r; return { stdout: r, end: "complete" }; } };
  const sv = new StatusView({ t, now: () => NOW, agentRunning: () => running, notify: () => undefined });
  const s = new Session({ registry, refs: new PathRefTable(), keychain: kc, service: () => serviceFor("r"), runner, t, approvalKey: SoftwareApprovalKey.generate(), now: () => NOW.toISOString(), sendToAgent: (_id, text) => sent.push(text), onChat: (l) => lines.push(l), onCard: async () => ({ kind: "close" }), onPanel: async () => "done" as const, status: sv });
  return { s, sv, lines, sent, calls };
}
const line = (o: unknown): string => JSON.stringify(o) + "\n";

test("session: pressing the operation hands the fixed text to the agent as a human input and draws it in the chat; not pressing sends nothing", async () => {
  let n = 0;
  const x = sessionWith(() => line(status(n++ === 0 ? {} : { hold: { active: true } })));
  await x.s.refreshStatus();
  await x.s.refreshStatus();
  assert.deepEqual(x.sent, [], "押さなければモデルの呼び出し(入力の送信)が増えない");
  assert.deepEqual(x.sv.view().askAi.map((a) => a.kind), ["hold_active"]);
  x.s.askAi("hold_active");
  assert.deepEqual(x.sent, [t("status.ask_ai.input.hold_active")]);
  assert.deepEqual(x.lines.filter((l) => l.role === "human").map((l) => l.text), [t("status.ask_ai.input.hold_active")]);
  x.s.askAi("hold_active");
  assert.equal(x.sent.length, 1, "同じ操作を 2 度は渡さない");
  const y = sessionWith(() => new SshError({ code: "SSH_UNREACHABLE", reason: "timeout" }));
  await y.s.refreshStatus();
  assert.equal(y.sv.view().rows[0]!.label, t("status.unreachable.fixed"));
});

const PROC = `
| 手順 | 目的 | 実行するもの | 完了確認コマンド | 期待する値 | 失敗時 |
|---|---|---|---|---|---|
| 1 | a | \`bootstrap\` | \`sitate version\` と \`sitate doctor\` | 1 つ目: \`/ok\` = \`true\`、\`/result/signing_key_fp\` = \`{{DIST_KEY_FINGERPRINT}}\`。2 つ目: \`/result/passed\` = \`true\` | — |
| 2 | b | x | \`sitate init-storage --dry-run --endpoint ...\` | \`/result/plan/errors\` = \`[]\` | — |
| 3 | c | x | \`sitate status\` | \`/result/kit/confirmed\` = \`true\`、\`/result/schedule\` = \`<定義の schedule>\`、\`/warnings/<i>/code\` ⊆ \`{{WARNING_ALLOWLIST}}\` | — |
| 4 | d | x | \`sitate bucket check\` | \`/result/permissions/<i>/actual\` = \`/result/permissions/<i>/expected\` | — |
`;
const marks = { DIST_KEY_FINGERPRINT: "SHA256:abc", WARNING_ALLOWLIST: ["OK_WARN"] };

test("step judge: reads the table, judges each step once, needs both commands for a two-command step and honours --dry-run", () => {
  assert.deepEqual(parseSteps(PROC).map((s) => s.id), ["1", "2", "3", "4"]);
  const j = new StepJudge(PROC, marks);
  assert.deepEqual(j.judge({ command: ["version"], ok: true, result: { signing_key_fp: "SHA256:abc" } } as never), [], "2 つ目が未了");
  assert.deepEqual(j.judge({ command: ["doctor"], ok: true, result: { passed: true } } as never), ["1"]);
  assert.deepEqual(j.judge({ command: ["doctor"], ok: true, result: { passed: true } } as never), [], "同じ手順の合格を 2 度数えない");
  assert.deepEqual(j.judge({ command: ["init-storage"], dry_run: false, ok: true, result: { plan: { errors: [] } } } as never), [], "dry-run でない結果は照らさない");
  assert.deepEqual(j.judge({ command: ["init-storage"], dry_run: true, ok: true, result: { plan: { errors: [] } } } as never), ["2"]);
  assert.deepEqual(j.judge({ command: ["status"], ok: true, result: { kit: { confirmed: true }, schedule: "02:30" }, warnings: [{ code: "OK_WARN" }] } as never), ["3"]);
  assert.deepEqual(j.judge({ command: ["bucket", "check"], ok: true, result: { permissions: [{ actual: 1, expected: 1 }, { actual: 2, expected: 2 }] } } as never), ["4"]);
  j.reset();
  assert.deepEqual(j.judge({ command: ["bucket", "check"], ok: true, result: { permissions: [{ actual: 1, expected: 2 }] } } as never), [], "等式が外れれば合格しない");
  const k = new StepJudge(PROC, {});
  assert.deepEqual(k.judge({ command: ["status"], ok: true, result: { kit: { confirmed: true }, schedule: "02:30" }, warnings: [{ code: "OTHER" }] } as never), [], "印の値が無ければ許容の外の警告は合格しない");
});

test("session: a passing step draws the passed line once and signals a round trip reset; a repeat does not", async () => {
  const judge = new StepJudge(PROC, marks);
  const kc = new MemoryKeychain();
  const lines: ChatLine[] = [];
  const respond = (argv: readonly string[]): string => line({ schema: "sitate.out/v1", command: argv.slice(0, 1), ok: true, dry_run: false, run_id: null, warnings: [], result: { passed: true, signing_key_fp: "SHA256:abc", version: "1" } });
  const runner: CoreRunner = { async run(argv) { return { stdout: respond(argv), end: "complete" }; } };
  const s = new Session({ registry, refs: new PathRefTable(), keychain: kc, service: () => serviceFor("r"), runner, t, approvalKey: SoftwareApprovalKey.generate(), now: () => NOW.toISOString(), sendToAgent: () => undefined, onChat: (l) => lines.push(l), onCard: async () => ({ kind: "close" }), onPanel: async () => "done" as const, judge });
  await s.onToolCall("sitate_version", {});
  assert.equal(s.consumeStepPassed(), false);
  await s.onToolCall("sitate_doctor", {});
  assert.equal(s.consumeStepPassed(), true);
  assert.equal(s.consumeStepPassed(), false, "読むと下りる");
  await s.onToolCall("sitate_doctor", {});
  assert.equal(s.consumeStepPassed(), false);
  assert.equal(lines.filter((l) => l.text === t("chat.tool.display.step_passed", { step: "1" })).length, 1);
});

test("runtime: the round trip count restarts on a step pass, a run of non-passing round trips reaches AGENT_TURN_LIMIT", async () => {
  const bundle = { manual: "M", procedure: "P", rules: "R", tools: registry.definitions(), models: ["m"] };
  const system = buildSystemBlocks(bundle, { locale: "ja", date: "2026-11-03", model: "m" });
  // 4 回の往復のうち 3 回目で合格(数え直し)。上限 3 でも合格を挟めば 6 回まで進める。
  const calls = { n: 0 };
  const llm = fakeLlm((n) => (n < 5 ? { toolUses: [{ name: "sitate_status", input: {} }] } : { text: "終わり" }));
  let rt!: ReturnType<typeof createAgentRuntime>;
  rt = createAgentRuntime({ client: llm.client, model: "m", system, tools: bundle.tools, roundtripMax: 3, onToolCall: async () => { calls.n += 1; if (calls.n % 3 === 0) rt.resetRoundtrips(); return "x"; } });
  assert.equal(await rt.run("go"), "終わり");
  assert.equal(calls.n, 5);
  const llm2 = fakeLlm(() => ({ toolUses: [{ name: "sitate_status", input: {} }] }));
  const rt2 = createAgentRuntime({ client: llm2.client, model: "m", system, tools: bundle.tools, roundtripMax: 3, onToolCall: async () => "x" });
  await assert.rejects(rt2.run("go"), (e: unknown) => e instanceof AgentFailure && e.code === "AGENT_TURN_LIMIT");
});

test("app causes: detail.reason / detail.step and the next steps follow the table, and only the send-field entries reach the model", async () => {
  const cases: [SshError, string, (a: { argv: string[]; requires_human: boolean }[]) => void][] = [
    [new SshError({ code: "SSH_UNREACHABLE", reason: "dns" }), "dns", (n) => { assert.equal(n[0]!.requires_human, true); assert.deepEqual(n[1]!.argv, ["sitate", "status"]); }],
    [new SshError({ code: "SSH_UNREACHABLE", reason: "refused" }), "refused", (n) => assert.equal(n[0]!.requires_human, true)],
    [new SshError({ code: "SSH_UNREACHABLE", reason: "timeout" }), "timeout", (n) => assert.equal(n[0]!.requires_human, true)],
  ];
  for (const [err, reason, check] of cases) {
    const x = sessionWith(() => err);
    const out = await x.s.onToolCall("sitate_status", {});
    const body = JSON.parse(out.split("\n")[1]!) as { error: { code: string; detail: Record<string, unknown>; next: { argv: string[]; requires_human: boolean }[] } };
    assert.equal(body.error.code, "SSH_UNREACHABLE");
    assert.deepEqual(body.error.detail, { reason });
    check(body.error.next);
  }
  const auth = sessionWith(() => new SshError({ code: "SSH_AUTH_DENIED" }));
  const o = JSON.parse((await auth.s.onToolCall("sitate_status", {})).split("\n")[1]!) as { error: { next: { argv: string[]; requires_human: boolean }[] } };
  assert.deepEqual(o.error.next, [{ argv: ["bootstrap", "--rekey"], requires_human: true, requires_approval: false, why: null }]);
});

// --- 完了条件 18・19・24 ---
import { mkdtempSync, readFileSync as readF, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { AppRecorder } from "../../src/main/app-record.ts";
import { reportFailure } from "../../src/main/failure-report.ts";
import { withUsageReport } from "../../src/agent/usage-fetch.ts";
import { AgentHost } from "../../src/main/agent-host.ts";
import type { ForkedAgent } from "../../src/main/agent-host.ts";

test("failures: each LLM / agent failure shows its fixed catalog sentence on the status area; API failures and a stopped agent also reach the OS notification", () => {
  const table: [string, string, boolean][] = [
    ["LLM_KEY_INVALID", "app.llm.key_invalid", true], ["LLM_BILLING", "app.llm.billing", true], ["LLM_RATE_LIMITED", "app.llm.rate_limited", true],
    ["LLM_PROVIDER_ERROR", "app.llm.provider_error", true], ["LLM_MODEL_NOT_FOUND", "app.llm.model_not_found", true], ["LLM_NO_RESPONSE", "app.llm.no_response", true],
    ["AGENT_EXITED", "app.agent.exited", true], ["AGENT_TURN_LIMIT", "app.agent.turn_limit", false], ["AGENT_OUTPUT_TRUNCATED", "app.agent.output_truncated", false],
  ];
  for (const [code, key, notified] of table) {
    const shown: string[] = [], chat: string[] = [], os: string[] = [];
    let replaced = 0;
    reportFailure(code, { t, showNotice: (x) => shown.push(x), chat: (x) => chat.push(x), osNotify: (x) => os.push(x), replaceKey: () => { replaced += 1; } });
    assert.deepEqual(shown, [t(key)], code);
    assert.deepEqual(chat, [t(key)], code);
    assert.deepEqual(os, notified ? [t(key)] : [], code);
    assert.equal(replaced, code === "LLM_KEY_INVALID" ? 1 : 0, code);
  }
  const quiet: string[] = [];
  reportFailure("AGENT_ABORTED", { t, showNotice: (x) => quiet.push(x), chat: (x) => quiet.push(x), osNotify: (x) => quiet.push(x) });
  assert.deepEqual(quiet, [], "人の中断は出さない");
});

test("P7: with no agent process the host reports AGENT_EXITED on input, while the status view and a propose approval card still work", async () => {
  const fails: (string | null)[][] = [];
  const host = new AgentHost((() => ({}) as ForkedAgent) as never, "/x/agent.js", { onReady: () => undefined, onReply: () => undefined, onFailure: (id, c) => fails.push([id, c]), onUsage: () => undefined, onToolCall: async () => "" });
  host.send("i0", "hello");
  assert.deepEqual(fails, [["i0", "AGENT_EXITED"]]);
  const { v } = view(false);
  v.ingest(status());
  assert.ok(v.view().rows.length > 1, "エージェントが無くても状態の表示は読める");
  const cards: unknown[] = [];
  const reply = (cmd: string[], result: unknown): string => line({ schema: "sitate.out/v1", command: cmd, ok: true, dry_run: false, run_id: null, warnings: [], result });
  const rec = { change_id: "c-1", op: "seal", args: {}, target: null, precondition_hash: "sha256:" + "0".repeat(64), plan: { reversible: true, errors: [], definition: {} }, proposed_at: "2026-11-03T01:00:00Z", delay_hours: 24, expires_at: "2026-11-10T01:00:00Z", canonical_hash: "sha256:" + "1".repeat(64) };
  const runner: CoreRunner = { async run(argv) { return { stdout: argv[0] === "pending" ? reply(["pending"], { changes: [rec] }) : reply([argv[0]!], { change_id: "c-1" }), end: "complete" }; } };
  const s = new Session({ registry, refs: new PathRefTable(), keychain: new MemoryKeychain(), service: () => serviceFor("r"), runner, t, approvalKey: SoftwareApprovalKey.generate(), now: () => NOW.toISOString(), sendToAgent: () => undefined, onChat: () => undefined, onCard: async (c) => { cards.push(c); return { kind: "close" }; }, onPanel: async () => "done" as const });
  await s.onToolCall("sitate_propose", { kind: "seal", definition: { version: 1 } });
  assert.equal(cards.length, 1, "承認カードは出る");
});

test("app record: requests and runs are written as JSON with numbers and the model name only", () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-rec-"));
  try {
    let tick = 0;
    const rec = new AppRecorder(join(dir, "data", "app-record.json"), () => new Date(Date.UTC(2026, 10, 3, 0, 0, 0) + tick * 1000));
    rec.runStart("i0");
    tick = 4;
    rec.request({ input_tokens: 100, output_tokens: 20, cache_read_input_tokens: 70, model: "claude-sonnet-5-5", elapsed_ms: 1500, ...{ text: "SECRET-CHAT" } } as never);
    rec.runEnd("i0");
    const f = JSON.parse(readF(join(dir, "data", "app-record.json"), "utf8")) as { requests: Record<string, unknown>[]; runs: Record<string, unknown>[] };
    assert.deepEqual(Object.keys(f.requests[0]!).sort(), ["at", "cache_read_input_tokens", "elapsed_ms", "input_tokens", "model", "output_tokens"]);
    assert.equal(f.requests[0]!.cache_read_input_tokens, 70);
    assert.equal(f.runs[0]!.elapsed_ms, 4000);
    assert.ok(!JSON.stringify(f).includes("SECRET-CHAT"));
  } finally { rmSync(dir, { recursive: true, force: true }); }
});

test("usage fetch: reports usage, model and send-to-complete time for message requests only, and passes the response through", async () => {
  const got: unknown[] = [];
  let clock = 1000;
  const base = (async (url: string) => { clock += 250; return new Response(JSON.stringify(String(url).endsWith("/v1/models/x") ? { id: "x" } : { model: "m1", usage: { input_tokens: 7, output_tokens: 3, cache_read_input_tokens: 2 } }), { status: 200, headers: { "content-type": "application/json" } }); }) as unknown as typeof fetch;
  const f = withUsageReport(base, (u) => got.push(u), () => clock);
  const res = await f("https://api.example/v1/messages", { method: "POST", body: "{}" });
  assert.equal((await res.json() as { model: string }).model, "m1");
  await f("https://api.example/v1/models/x");
  assert.deepEqual(got, [{ input_tokens: 7, output_tokens: 3, cache_read_input_tokens: 2, model: "m1", elapsed_ms: 250 }]);
});
