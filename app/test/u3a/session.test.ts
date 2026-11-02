// 会話の制御(完了条件 5・8・26・31): 承認の往復、取り消し、道具の呼び出しの 1 行、送信前の検出、表示しなかった応答の区画。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { SoftwareApprovalKey, canonicalHash, verifyApproval } from "../../src/main/approval-sign.ts";
import type { CardModel } from "../../src/main/approval-card.ts";
import { MemoryKeychain, serviceFor } from "../../src/main/keychain.ts";
import { PathRefTable } from "../../src/main/path-ref.ts";
import { Session } from "../../src/main/session.ts";
import type { ChatLine, CoreRunner } from "../../src/main/session.ts";
import { ToolRegistry } from "../../src/main/tool-registry.ts";
import type { CardDecision } from "../../src/main/ipc-channels.ts";

const keys = JSON.parse(readFileSync(new URL("../../src/i18n/keys.json", import.meta.url), "utf8")) as Record<string, unknown>;
const t = (key: string, args?: Record<string, unknown>): string => {
  if (!Object.hasOwn(keys, key)) throw new Error("no key " + key);
  return args === undefined ? key : key + JSON.stringify(args);
};
const registry = ToolRegistry.fromDir(join(import.meta.dirname, "..", "..", "src", "tools"));
const env = (command: string[], result: unknown): string => JSON.stringify({ schema: "sitate.out/v1", command, ok: true, dry_run: false, run_id: null, warnings: [], result }) + "\n";

function setup(decision: CardDecision) {
  const rec: Record<string, unknown> = {
    change_id: "c-20261103-0001", op: "seal", args: {}, target: null, precondition_hash: "sha256:" + "0".repeat(64),
    plan: { reversible: true, errors: [], definition: { source: { targets: [{ id: "t1", files: "/srv/real-name-dir" }] } } },
    proposed_at: "2026-11-03T01:00:00Z", delay_hours: 24, expires_at: "2026-11-10T01:00:00Z",
  };
  rec.canonical_hash = canonicalHash(rec);
  const calls: { argv: readonly string[]; stdin: string }[] = [];
  const runner: CoreRunner = {
    async run(argv, stdin) {
      calls.push({ argv, stdin });
      const head = argv[0]!;
      if (head === "propose") return { stdout: env(["propose"], { change_id: "c-20261103-0001", canonical_hash: rec.canonical_hash, plan: { reversible: true } }), end: "complete" };
      if (head === "pending") return { stdout: env(["pending"], { changes: [rec] }), end: "complete" };
      if (head === "approve") return { stdout: env(["approve"], { state: "approved", approved_at: "2026-11-03T01:05:00Z" }), end: "complete" };
      return { stdout: env([head], {}), end: "complete" };
    },
  };
  const key = SoftwareApprovalKey.generate();
  const lines: ChatLine[] = [];
  const sent: { id: string; text: string }[] = [];
  const cards: CardModel[] = [];
  const kc = new MemoryKeychain();
  const s = new Session({
    registry, refs: new PathRefTable(), keychain: kc, service: () => serviceFor("repoX"), runner, t, approvalKey: key,
    now: () => "2026-11-03T01:05:00Z", sendToAgent: (id, text) => sent.push({ id, text }), onChat: (l) => lines.push(l),
    onCard: async (c) => { cards.push(c); return decision; }, onPanel: async () => "done" as const,
  });
  return { s, calls, key, lines, sent, cards, rec, kc };
}
const proposeInput = { kind: "seal", definition: { version: 1, source: { host: "h", targets: [] } } };

test("propose の承認: カードは pending の記録から描かれ、署名は記録から計算した値に付き、モデルへは実名の無い区画だけが返る", async () => {
  const x = setup({ kind: "approve", proofs: {} });
  const out = await x.s.onToolCall("sitate_propose", proposeInput);
  assert.deepEqual(x.calls.map((c) => c.argv[0]), ["propose", "pending", "approve"]);
  const approve = x.calls[2]!.argv;
  const sig = approve[approve.indexOf("--signature") + 1]!;
  assert.ok(verifyApproval(x.key.publicKeyDer, { changeId: "c-20261103-0001", canonicalHash: x.rec.canonical_hash as string, signedAt: "2026-11-03T01:05:00Z", signature: sig }));
  assert.ok(JSON.stringify(x.cards[0]!.sections).includes("/srv/real-name-dir"));
  assert.ok(!out.includes("/srv/real-name-dir") && out.includes('command="approve"'));
  assert.equal(x.cards[0]!.canonicalHash, x.rec.canonical_hash);
});

test("取り消し: cancel を呼び、HUMAN_CANCELLED をモデルへ返す。閉じる: approve も cancel も呼ばない", async () => {
  const c = setup({ kind: "cancel" });
  const out = await c.s.onToolCall("sitate_propose", proposeInput);
  assert.deepEqual(c.calls.map((k) => k.argv[0]), ["propose", "pending", "cancel"]);
  assert.ok(out.includes("HUMAN_CANCELLED"));
  const z = setup({ kind: "close" });
  await z.s.onToolCall("sitate_propose", proposeInput);
  assert.deepEqual(z.calls.map((k) => k.argv[0]), ["propose", "pending"]);
});

test("道具の呼び出しの 1 行: 引数の値・参照名・パスが現れず、モデルへ送られない。拒否も同じ書式", async () => {
  const x = setup({ kind: "close" });
  await x.s.onToolCall("sitate_not_a_tool", { secret_refs: ["SENTINEL-REF"], path: "/srv/SENTINEL-PATH" });
  await x.s.onToolCall("sitate_version", {});
  const tool = x.lines.filter((l) => l.role === "tool").map((l) => l.text);
  assert.equal(tool.length, 2);
  assert.ok(tool[0]!.includes("app.tool.denied"));
  for (const l of tool) assert.ok(!l.includes("SENTINEL"));
  assert.equal(x.sent.length, 0);
});

test("送信前の検出: 秘密の形は送らず、固定文だけを出す。キーチェーンの値も止める", async () => {
  const x = setup({ kind: "close" });
  await x.kc.set(serviceFor("repoX"), "smtp", "hunter2-password-value");
  await x.s.submit("鍵は sk-ant-api03-" + "z".repeat(30) + " です");
  await x.s.submit("パスワードは hunter2-password-value");
  assert.equal(x.sent.length, 0);
  assert.ok(x.lines.every((l) => l.role === "notice" && !l.text.includes("hunter2") && !l.text.includes("sk-ant")));
  await x.s.submit("状況を教えて");
  assert.equal(x.sent.length, 1);
});

test("表示しなかった応答: 次の入力の前に区画が 1 つだけ置かれ、モデルは呼び直さない", async () => {
  const x = setup({ kind: "close" });
  x.s.onReply("エラーは [[status:/result/x=1]] です。");
  x.s.onReply("[[status:/result/y=2]]");
  assert.equal(x.sent.length, 0);
  assert.ok(x.lines.every((l) => l.role === "notice"));
  await x.s.submit("続けて");
  assert.equal(x.sent.length, 1);
  assert.equal(x.sent[0]!.text.match(/<sitate-data command="app quote-check">/g)!.length, 1);
  assert.ok(x.sent[0]!.text.endsWith("\n続けて"));
  await x.s.submit("もう一度");
  assert.ok(!x.sent[1]!.text.includes("quote-check"));
});

// ---- H2: フレーズのパネルが済むまで道具の呼び出しは戻らない ----
const WORD_LIST = JSON.parse(readFileSync(new URL("../../src/data/bip39-english.json", import.meta.url), "utf8")) as string[];
const initStdout = (): string => {
  const secret = JSON.stringify({ schema: "sitate.secret-string/v1", kind: "recovery_phrase", words: WORD_LIST.slice(0, 24), keyring_version: 1 });
  const e = JSON.stringify({ schema: "sitate.out/v1", command: ["init"], ok: true, dry_run: false, run_id: null, warnings: [], phrase_emitted: true, result: { repo_id: "r1" } });
  return `${secret}\n${e}\n`;
};

function panelRig(opts: { runnerThrows?: Error; initOut?: string } = {}) {
  const panels: { resolve: (o: "done" | "cancelled") => void }[] = [];
  const calls: string[] = [];
  const lines: ChatLine[] = [];
  const runner: CoreRunner = {
    async run(argv) {
      calls.push(argv[0]!);
      if (opts.runnerThrows !== undefined) throw opts.runnerThrows;
      if (argv[0] === "init") return { stdout: opts.initOut ?? initStdout(), end: "complete" };
      return { stdout: env([argv[0]!], {}), end: "complete" };
    },
  };
  const s = new Session({
    registry, refs: new PathRefTable(), keychain: new MemoryKeychain(), service: () => serviceFor("repoX"), runner, t, approvalKey: SoftwareApprovalKey.generate(),
    now: () => "2026-11-03T01:05:00Z", sendToAgent: () => undefined, onChat: (l) => lines.push(l), onCard: async () => ({ kind: "close" }),
    onPanel: () => new Promise((resolve) => { panels.push({ resolve }); }),
  });
  return { s, panels, calls, lines };
}
const initInput = { admin_ref: "adm", secret_refs: ["adm"], bucket: "b" };
const settled = async (p: Promise<unknown>): Promise<boolean> => Promise.race([p.then(() => true, () => true), new Promise<boolean>((r) => setImmediate(() => r(false)))]);

test("H2: 秘密のレコードが来た道具の呼び出しは、パネルの完了まで戻らず、完了で通常の区画を返す", async () => {
  const x = panelRig();
  const call = x.s.onToolCall("sitate_init", { ...initInput, secret_refs: [] });
  assert.equal(await settled(call), false, "パネルが開いている間は戻らない");
  assert.equal(x.panels.length, 1);
  x.panels[0]!.resolve("done");
  const out = await call;
  assert.ok(out.includes('command="init"'), out);
  assert.ok(!out.includes("HUMAN_CANCELLED"));
});

test("H2: パネルの取り消しは HUMAN_CANCELLED をモデルへ返す。語は区画に入らない", async () => {
  const x = panelRig();
  const call = x.s.onToolCall("sitate_init", { ...initInput, secret_refs: [] });
  await settled(call);
  x.panels[0]!.resolve("cancelled");
  const out = await call;
  assert.ok(out.includes("HUMAN_CANCELLED"), out);
  assert.ok(!out.includes(WORD_LIST[0]! + " " + WORD_LIST[1]!));
});

test("M8: 想定外の例外・未知の原因コードでも道具の呼び出しは reject せず、固定文の封筒を返す", async () => {
  const boom = panelRig({ runnerThrows: new Error("core/app version mismatch") });
  const out = await boom.s.onToolCall("sitate_version", {});
  assert.ok(out.includes("ENVELOPE_INVALID"), out);
  const unknown = panelRig({ initOut: JSON.stringify({ schema: "sitate.out/v1", command: ["init"], ok: false, dry_run: false, run_id: null, warnings: [], result: null, error: { code: "SOME_FUTURE_CODE", class: "input", retryable: false, detail: {}, next: [] } }) + "\n" });
  const out2 = await unknown.s.onToolCall("sitate_init", { ...initInput, secret_refs: [] });
  assert.equal(typeof out2, "string");
  assert.ok(unknown.lines.some((l) => l.role === "tool"));
});
