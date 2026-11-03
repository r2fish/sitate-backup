import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { appendFileSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { after, before, test } from "node:test";
import { build } from "esbuild";
import { agentExecArgv } from "../../src/main/agent-host.ts";

// 完了条件 25 (b)(c)(spec 6.5 A6 の (15)): エージェントのプロセスを製品と同じ実行引数(agentExecArgv)で実際に起動し、
//  (b) 口の外の経路が使われない(接続・fetch・WebSocket・tls・dgram・http の呼び出しを全部記録し、送り先が LLM の口だけ)、禁じた経路(子プロセス・書き込み・読み取り)は失敗する、
//  (c) 別のホストへの送信が guarded fetch で失敗し、プロセスの記録(標準エラーの TRACE と IPC の応答)に残る。
// LLM の口はこの試験の中の偽のサーバー(127.0.0.1)で、実物の LLM API と保管先へは何も送らない。
// A6NET_APP_ROOT: 変異の確認用に app の写し。A6NET_RECORD_FILE: プロセスの記録(JSON Lines)の書き出し先。
const appRoot = process.env.A6NET_APP_ROOT ?? resolve(import.meta.dirname, "..", "..");
const recordFile = process.env.A6NET_RECORD_FILE;
const record = (o: unknown): void => { if (recordFile !== undefined) appendFileSync(recordFile, JSON.stringify(o) + "\n"); };

// 製品の実行体(Electron 45 の Node 24)は --allow-net を知らず送信を制限しない。試験の node が知る版では、同じ「制限しない」に合わせるため付ける。
const nodeHasAllowNet = spawnSync(process.execPath, ["--permission", "--allow-net", "-e", "1"]).status === 0;
const netFlags = nodeHasAllowNet ? ["--allow-net"] : [];

const SHIM = `
const net = require("net"), tls = require("tls"), dgram = require("dgram"), http = require("http"), https = require("https");
const trace = (kind, v) => process.stderr.write("TRACE " + JSON.stringify({ kind, ...v }) + "\\n");
const where = (a0) => { const a = Array.isArray(a0) ? a0[0] : a0; return a !== null && typeof a === "object" ? { host: a.host ?? null, port: a.port ?? null, path: a.path ?? null } : { host: null, port: a ?? null, path: null }; };
const oc = net.Socket.prototype.connect;
net.Socket.prototype.connect = function (...a) { trace("net.connect", where(a[0])); return oc.apply(this, a); };
const ot = tls.connect; tls.connect = function (...a) { trace("tls.connect", where(a[0])); return ot.apply(this, a); };
const od = dgram.createSocket; dgram.createSocket = function (...a) { trace("dgram.createSocket", {}); return od.apply(this, a); };
for (const m of [http, https]) { const o = m.request; m.request = function (...a) { trace("http.request", { arg: String(a[0] && a[0].href || a[0]) }); return o.apply(this, a); }; }
const of = globalThis.fetch; globalThis.fetch = function (i, o) { trace("fetch", { url: String(i && i.url || i) }); return of(i, o); };
if (globalThis.WebSocket) { const W = globalThis.WebSocket; globalThis.WebSocket = function (u, ...r) { trace("WebSocket", { url: String(u) }); return new W(u, ...r); }; }
const lines = []; let cb = () => undefined; let buf = "";
process.parentPort = {
  postMessage: (m) => process.stdout.write(JSON.stringify(m) + "\\n"),
  on: (ev, f) => { cb = f; },
};
process.stdin.on("data", (d) => { buf += d; let i; while ((i = buf.indexOf("\\n")) >= 0) { const l = buf.slice(0, i); buf = buf.slice(i + 1); if (l) cb({ data: JSON.parse(l) }); } });
`;

interface Hit { method: string; url: string }
interface Fake { server: Server; hits: Hit[]; origin: string; port: number }
async function fake(handler: (q: IncomingMessage, r: ServerResponse, hits: Hit[]) => void): Promise<Fake> {
  const hits: Hit[] = [];
  const server = createServer((q, r) => { hits.push({ method: q.method ?? "", url: q.url ?? "" }); handler(q, r, hits); });
  await new Promise<void>((ok) => server.listen(0, "127.0.0.1", ok));
  const port = (server.address() as AddressInfo).port;
  return { server, hits, port, origin: `http://127.0.0.1:${port}` };
}
const json = (r: ServerResponse, body: unknown, status = 200): void => { r.writeHead(status, { "content-type": "application/json" }); r.end(JSON.stringify(body)); };
const llm = (q: IncomingMessage, r: ServerResponse): void => {
  if (q.method === "GET") return json(r, { id: "m", type: "model", display_name: "m", created_at: "2026-01-01T00:00:00Z" });
  q.resume();
  json(r, { id: "msg_1", type: "message", role: "assistant", model: "m", content: [{ type: "text", text: "ok" }], stop_reason: "end_turn", stop_sequence: null, usage: { input_tokens: 1, output_tokens: 1 } });
};

let dir = "";
let agentJs = "";
let probeJs = "";
const servers: Server[] = [];
before(async () => {
  dir = realpathSync(mkdtempSync(join(tmpdir(), "a6net-")));
  agentJs = join(dir, "agent.js");
  probeJs = join(dir, "probe.js");
  const common = { bundle: true, sourcemap: false, logLevel: "error" as const, absWorkingDir: appRoot, platform: "node" as const, format: "cjs" as const };
  await build({ ...common, define: { __TEST_BUILD__: "true" }, entryPoints: ["src/agent/entry.ts"], outfile: agentJs });
  await build({ ...common, stdin: { contents: PROBE, resolveDir: join(appRoot, "src", "agent"), loader: "ts" }, outfile: probeJs });
  writeFileSync(join(dir, "shim.cjs"), SHIM);
  writeFileSync(join(dir, "package.json"), JSON.stringify({ type: "commonjs" }));
});
after(() => { for (const s of servers) { s.closeAllConnections(); s.close(); } rmSync(dir, { recursive: true, force: true }); });

interface Run { out: Record<string, unknown>[]; traces: Record<string, unknown>[]; stderr: string; status: number | null }
/** 製品と同じ実行引数で起動し、標準入力へ IPC のメッセージを送り、done(out) が真になるまで(または上限まで)記録を集める。 */
async function runProcess(entryFile: string, send: Record<string, unknown>[], later: Record<string, unknown>[], done: (out: Record<string, unknown>[]) => boolean, limitMs = 20_000): Promise<Run> {
  const p = spawn(process.execPath, [...agentExecArgv(entryFile), ...netFlags, "--require", join(dir, "shim.cjs"), entryFile], { env: {}, stdio: ["pipe", "pipe", "pipe"] });
  let stdout = "", stderr = "";
  p.stdout.on("data", (d) => { stdout += d; });
  p.stderr.on("data", (d) => { stderr += d; });
  const put = (m: Record<string, unknown>): void => { p.stdin.write(JSON.stringify(m) + "\n"); };
  send.forEach(put);
  let laterSent = false;
  const parse = (): Record<string, unknown>[] => stdout.split("\n").filter((l) => l.startsWith("{")).map((l) => JSON.parse(l) as Record<string, unknown>);
  const exit = new Promise<number | null>((ok) => p.on("exit", (c) => ok(c)));
  const deadline = Date.now() + limitMs;
  let status: number | null = null;
  while (Date.now() < deadline && !done(parse())) {
    // 入力は ready の後に送る(ready の前の入力は AGENT_ABORTED になる製品の仕様)。
    if (!laterSent && parse().some((m) => m.type === "ready")) { later.forEach(put); laterSent = true; }
    const r = await Promise.race([exit.then((c) => ({ c })), new Promise<null>((ok) => setTimeout(() => ok(null), 50))]);
    if (r !== null) { status = r.c; break; }
  }
  p.kill();
  const traces = stderr.split("\n").filter((l) => l.startsWith("TRACE ")).map((l) => JSON.parse(l.slice(6)) as Record<string, unknown>);
  return { out: parse(), traces, stderr: stderr.split("\n").filter((l) => !l.startsWith("TRACE ") && !l.includes("ExperimentalWarning") && !l.includes("--trace-warnings")).join("\n"), status };
}
const bundleMsg = { manual: "m", procedure: "p", rules: "r", tools: [], models: ["m"] };
const initMsg = (baseUrl: string): Record<string, unknown> => ({ type: "init", apiKey: "test-key-not-real", bundle: bundleMsg, locale: "ja", date: "2026-11-04", baseUrl });
const portOf = (t: Record<string, unknown>): string => String(t.port ?? "");

test("[A6-15b] 通常の会話(モデルの確認と応答)で、エージェントのプロセスが使う送信の経路は LLM の口の接続と fetch だけ。口の外の接続・WebSocket・tls・dgram・http の呼び出しが 0 件", async () => {
  const a = await fake(llm), b = await fake((_q, r) => json(r, {}));
  servers.push(a.server, b.server);
  const r = await runProcess(agentJs, [initMsg(a.origin)], [{ type: "input", id: "i1", text: "こんにちは" }], (o) => o.some((m) => m.type === "reply" || m.type === "failure" && m.id === "i1"));
  record({ scenario: "normal", out: r.out, traces: r.traces, stderr: r.stderr, allowed_hits: a.hits, other_hits: b.hits, allow_net_flag: nodeHasAllowNet });
  assert.ok(r.out.some((m) => m.type === "ready"), JSON.stringify(r.out) + r.stderr);
  assert.ok(r.out.some((m) => m.type === "reply" && m.text === "ok"), JSON.stringify(r.out));
  assert.ok(a.hits.length >= 2 && b.hits.length === 0);
  const kinds = new Set(r.traces.map((t) => t.kind));
  assert.deepEqual([...kinds].filter((k) => k !== "net.connect" && k !== "fetch"), [], "WebSocket・tls・dgram・http.request は呼ばれない");
  assert.ok(r.traces.filter((t) => t.kind === "fetch").length >= 2);
  assert.deepEqual(r.traces.filter((t) => t.kind === "fetch" && !String(t.url).startsWith(a.origin + "/")), []);
  assert.deepEqual(r.traces.filter((t) => t.kind === "net.connect" && portOf(t) !== String(a.port)), []);
});

test("[A6-15c] LLM の口が別のホストへ転送しても(攻撃者の口の想定)、guarded fetch が失敗させ、別のホストへ接続しない。失敗はプロセスの記録に残る", async () => {
  const b = await fake((_q, r) => json(r, { id: "x" }));
  const a = await fake((_q, r) => { r.writeHead(302, { location: `${b.origin}/v1/models/m` }); r.end(); });
  servers.push(a.server, b.server);
  const r = await runProcess(agentJs, [initMsg(a.origin)], [], (o) => o.some((m) => m.type === "failure" || m.type === "ready"), 30_000);
  record({ scenario: "redirect-to-other-host", out: r.out, traces: r.traces, stderr: r.stderr, allowed_hits: a.hits, other_hits: b.hits });
  assert.equal(b.hits.length, 0, "別のホストの口は 1 度も叩かれない");
  assert.deepEqual(r.traces.filter((t) => t.kind === "net.connect" && portOf(t) === String(b.port)), []);
  assert.ok(!r.out.some((m) => m.type === "ready"), "転送先の応答でモデルの確認が通ってはならない");
  const f = r.out.find((m) => m.type === "failure");
  assert.ok(f !== undefined && f.id === null && typeof f.code === "string", JSON.stringify(r.out));
  assert.ok(a.hits.length >= 1 && a.hits.every((h) => h.url.startsWith("/v1/models/")));
});

const PROBE = `
import { EgressDeniedError, makeGuardedFetch } from "./guarded-fetch.ts";
const out = (o: unknown) => process.stdout.write(JSON.stringify(o) + "\\n");
(async () => {
  const [allowed, other] = process.argv.slice(2) as [string, string];
  const g = makeGuardedFetch(fetch, allowed);
  out({ step: "allowed", status: (await g(allowed + "/ok")).status });
  for (const target of [other + "/x", "https://example.invalid/", "http://localhost:1/", allowed.replace("127.0.0.1", "127.0.0.2") + "/x", "not a url"]) {
    try { await g(target); out({ step: "denied", target, result: "SENT" }); }
    catch (e) { process.stderr.write("EGRESS_DENIED " + target + "\\n"); out({ step: "denied", target, result: e instanceof EgressDeniedError ? "EGRESS_DENIED" : String(e) }); }
  }
  // (b) 禁じた経路: 子プロセスの起動・書き込み・許可の外の読み取りは失敗する。XMLHttpRequest は無い。
  const fail = (f: () => unknown): string => { try { f(); return "RAN"; } catch (e) { return (e as { code?: string }).code ?? String(e); } };
  const req = (m: string) => process.getBuiltinModule(m as "fs") as any;
  out({ step: "forbidden",
    xhr: typeof XMLHttpRequest,
    spawn: fail(() => req("child_process").execSync("true")),
    write: fail(() => req("fs").writeFileSync(process.argv[4]!, "x")),
    read_outside: fail(() => req("fs").readFileSync(process.argv[5]!, "utf8")),
  });
  out({ step: "present", fetch: typeof fetch, WebSocket: typeof WebSocket });
})();
`;

test("[A6-15b][A6-15c] 実プロセスの guarded fetch と禁じた経路: 口への送信は通り、別のホスト・別のポートの同じホスト・名前解決の要らない別名・不正な URL は EGRESS_DENIED で失敗し、別のホストの口は叩かれない", async () => {
  const a = await fake((_q, r) => json(r, { ok: true })), b = await fake((_q, r) => json(r, {}));
  servers.push(a.server, b.server);
  const p = spawn(process.execPath, [...agentExecArgv(probeJs), ...netFlags, "--require", join(dir, "shim.cjs"), probeJs, a.origin, b.origin, join(dir, "w.txt"), join(dir, "..", "x")], { env: {}, stdio: ["pipe", "pipe", "pipe"] });
  p.stdin.end();
  let so = "", se = "";
  p.stdout.on("data", (d) => { so += d; });
  p.stderr.on("data", (d) => { se += d; });
  await new Promise((ok) => p.on("exit", ok));
  const out = so.split("\n").filter((l) => l.startsWith("{")).map((l) => JSON.parse(l) as Record<string, unknown>);
  const traces = se.split("\n").filter((l) => l.startsWith("TRACE ")).map((l) => JSON.parse(l.slice(6)) as Record<string, unknown>);
  record({ scenario: "guard-direct", out, traces, stderr: se.split("\n").filter((l) => !l.startsWith("TRACE ")).join("\n"), allowed_hits: a.hits, other_hits: b.hits });
  assert.deepEqual(out.find((m) => m.step === "allowed"), { step: "allowed", status: 200 }, se);
  const denied = out.filter((m) => m.step === "denied");
  assert.equal(denied.length, 5);
  assert.deepEqual(denied.map((m) => m.result), Array(5).fill("EGRESS_DENIED"));
  assert.equal(b.hits.length, 0);
  assert.deepEqual(a.hits.map((h) => h.url), ["/ok"], "口へは許可した 1 回だけ");
  assert.equal(se.split("\n").filter((l) => l.startsWith("EGRESS_DENIED ")).length, 5, "拒否はプロセスの記録(標準エラー)に 5 行残る");
  assert.deepEqual(traces.filter((t) => t.kind === "net.connect" && portOf(t) !== String(a.port)), [], "口以外への接続の試みが 1 件も無い");
  // (b) 禁じた経路。
  const f = out.find((m) => m.step === "forbidden")!;
  assert.equal(f.xhr, "undefined");
  assert.equal(f.spawn, "ERR_ACCESS_DENIED");
  assert.equal(f.write, "ERR_ACCESS_DENIED");
  assert.equal(f.read_outside, "ERR_ACCESS_DENIED");
});
