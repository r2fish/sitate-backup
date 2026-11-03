// 試験用のアプリへのドライバの取り付け(`make build-app-test` だけが束ねる。主プロセスの入口の `if (__TEST_BUILD__)` の枝から呼ばれる)。
// SITATE_TEST_DRIVER_SOCKET が無ければ何もしない。主プロセスの本体のコードは変えず、ここで次を取り付ける:
//  - 主プロセスの出来事(エージェントの準備、実行の始まりと終わり、道具の呼び出しと封筒)の記録 → ドライバの `events`
//  - LLM API の口の差し替え(SITATE_TEST_LLM_BASE_URL。エージェントの init の baseUrl。製品のビルドでは効かない)
//  - 受入試験の環境のコンテナへ届く口の写像(SITATE_TEST_HOST_MAP。管理用 SSH の接続先だけを読み替える)
import ssh2 from "ssh2";
import { AgentHost } from "../main/agent-host.ts";
import { startDriverServer } from "./driver-server.ts";
import type { TestEvent } from "./driver-server.ts";
import { parseHostMap } from "./host-map.ts";

type Rec = Record<string, unknown>;

function patchSsh(spec: string | undefined): void {
  const map = parseHostMap(spec);
  const proto = ssh2.Client.prototype as unknown as { connect(this: unknown, cfg: { host?: string; port?: number }): unknown };
  const orig = proto.connect;
  proto.connect = function (cfg) { return orig.call(this, { ...cfg, ...map(cfg.host ?? "", cfg.port ?? 22) }); };
}

function patchHost(emit: (e: Rec) => void, baseUrl: string | undefined): void {
  const proto = AgentHost.prototype;
  const start = proto.start;
  const send = proto.send;
  const wrapped = new WeakSet<object>();
  proto.start = function (apiKey, bundle, locale, date) {
    if (!wrapped.has(this)) { wrapped.add(this); wrapEvents(this.events as unknown as Record<string, (...a: never[]) => unknown>, emit); }
    return start.call(this, apiKey, bundle, locale, date, baseUrl);
  };
  proto.send = function (id, text) { emit({ kind: "run_start", id }); return send.call(this, id, text); };
}

function wrapEvents(ev: Record<string, (...a: never[]) => unknown>, emit: (e: Rec) => void): void {
  const { onReady, onReply, onFailure, onToolCall } = ev as unknown as { onReady: (m: string, f: boolean) => void; onReply: (i: string, t: string) => void; onFailure: (i: string | null, c: string) => void; onToolCall: (n: string, i: unknown) => Promise<string | { content: string; stepPassed: boolean }> };
  const host = ev as unknown as Record<string, unknown>;
  host.onReady = (model: string, first: boolean) => { emit({ kind: "agent_ready", model }); onReady(model, first); };
  host.onReply = (id: string, text: string) => { emit({ kind: "run_end", id, outcome: "reply", text }); onReply(id, text); };
  host.onFailure = (id: string | null, code: string) => { emit({ kind: "run_end", id, outcome: "failure", code }); onFailure(id, code); };
  host.onToolCall = async (name: string, input: unknown) => {
    const r = await onToolCall(name, input);
    emit({ kind: "tool_call", name, envelope: typeof r === "string" ? r : r.content, step_passed: typeof r === "string" ? false : r.stepPassed });
    return r;
  };
}

export function attachDriver(evalInPage: (script: string) => Promise<unknown>): void {
  const sock = process.env.SITATE_TEST_DRIVER_SOCKET;
  if (sock === undefined) return;
  const events: TestEvent[] = [];
  const emit = (e: Rec): void => { events.push({ at: new Date().toISOString(), ...e } as TestEvent); };
  const base = process.env.SITATE_TEST_LLM_BASE_URL;
  patchSsh(process.env.SITATE_TEST_HOST_MAP);
  patchHost(emit, base === undefined || base === "" ? undefined : base);
  startDriverServer(sock, { eval: evalInPage }, () => undefined, () => events.splice(0, events.length));
}
