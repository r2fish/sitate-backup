// 秘密の入力ボックスの 3 モード(完了条件 4)と、Session の SSH 失敗の写像・run_id の付与・bootstrap の経路。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { SoftwareApprovalKey } from "../../src/main/approval-sign.ts";
import { MemoryKeychain, SERVICE_PRE_ADMIN, SERVICE_PRE_LLM, serviceFor } from "../../src/main/keychain.ts";
import { PathRefTable } from "../../src/main/path-ref.ts";
import { LLM_ACCOUNT, SecretBox, Transient } from "../../src/main/secret-box.ts";
import type { SecretRequest } from "../../src/main/secret-box.ts";
import { Session } from "../../src/main/session.ts";
import type { CoreRunner } from "../../src/main/session.ts";
import { SshError } from "../../src/main/ssh/connection.ts";
import { ToolRegistry } from "../../src/main/tool-registry.ts";

const keys = JSON.parse(readFileSync(new URL("../../src/i18n/keys.json", import.meta.url), "utf8")) as Record<string, unknown>;
const t = (key: string, args?: Record<string, unknown>): string => { if (!Object.hasOwn(keys, key)) throw new Error("no key " + key); return args === undefined ? key : key + JSON.stringify(args); };
const registry = ToolRegistry.fromDir(join(import.meta.dirname, "..", "..", "src", "tools"));
const ok = (command: string[], result: unknown = {}): string => JSON.stringify({ schema: "sitate.out/v1", command, ok: true, dry_run: false, run_id: null, warnings: [], result }) + "\n";
const ng = (command: string[], code: string, detail: unknown = {}): string => JSON.stringify({ schema: "sitate.out/v1", command, ok: false, dry_run: false, run_id: null, warnings: [], result: null, error: { code, class: "input", retryable: false, detail, next: [] } }) + "\n";

function rig(respond: (argv: readonly string[], stdin: string) => string | Error, answers: Record<string, string> = {}) {
  const kc = new MemoryKeychain();
  const calls: { argv: readonly string[]; stdin: string }[] = [];
  const shown: SecretRequest[] = [];
  let box!: SecretBox;
  box = new SecretBox({ keychain: kc, service: () => serviceFor("repoX"), show: (r) => { if (r !== null) { shown.push(r); const v = answers[r.ref]; queueMicrotask(() => { if (v === undefined) box.cancel(); else void box.submit(r.ref, v); }); } } });
  const runner: CoreRunner = { async run(argv, stdin) { calls.push({ argv, stdin }); const r = respond(argv, stdin); if (r instanceof Error) throw r; return { stdout: r, end: "complete" }; } };
  const bootstraps: Record<string, unknown>[] = [];
  const s = new Session({
    registry, refs: new PathRefTable(), keychain: kc, service: () => serviceFor("repoX"), runner, t, approvalKey: SoftwareApprovalKey.generate(),
    now: () => "2026-11-03T01:05:00Z", sendToAgent: () => undefined, onChat: () => undefined, onCard: async () => ({ kind: "close" }), onPanel: async () => "done" as const,
    secretBox: box, bootstrap: async (i) => { bootstraps.push(i); return { schema: "sitate.out/v1", command: ["bootstrap"], ok: true, dry_run: false, run_id: null, warnings: [], result: { admin_access_closed: true }, error: null }; },
  });
  return { s, kc, calls, shown, bootstraps };
}
const dump = (kc: MemoryKeychain): string => JSON.stringify([...kc.items.entries()]);

test("secret box: save stores in the keychain and returns the ref only; no-save keeps no trace and gives the value once; app-open stores the LLM key without a ref", async () => {
  const kc = new MemoryKeychain();
  let box!: SecretBox;
  const reqs: SecretRequest[] = [];
  box = new SecretBox({ keychain: kc, service: () => serviceFor("r"), show: (r) => { if (r) reqs.push(r); } });
  const p1 = box.request("save", "notify-cred", "notify-cred");
  await box.submit("notify-cred", "VALUE-1");
  const o1 = await p1;
  assert.deepEqual(o1, { kind: "stored", ref: "notify-cred" });
  assert.equal(await kc.get(serviceFor("r"), "notify-cred"), "VALUE-1");
  assert.ok(!JSON.stringify(o1).includes("VALUE-1"));

  const before = dump(kc);
  const p2 = box.request("no-save", "admin-key", "admin-key");
  await box.submit("admin-key", "VALUE-2");
  const o2 = await p2;
  assert.equal(o2.kind, "transient");
  assert.equal(dump(kc), before, "キーチェーンに置かない");
  if (o2.kind === "transient") { assert.equal(o2.value.take(), "VALUE-2"); assert.equal(o2.value.take(), null, "取り出した後はメモリから消えている"); }

  const p3 = box.request("app-open", "llm", "llm");
  await box.submit("llm", "LLM-KEY");
  assert.deepEqual(await p3, { kind: "stored", ref: null });
  assert.equal(await kc.get(SERVICE_PRE_LLM, LLM_ACCOUNT), "LLM-KEY");

  const p4 = box.request("save", "x", "x");
  box.cancel();
  assert.deepEqual(await p4, { kind: "cancelled" });
  const tr = new Transient("zz");
  tr.discard();
  assert.equal(tr.take(), null);
});

test("session: a missing secret ref opens the box in save mode and the value reaches the core only through stdin; closing the box is HUMAN_CANCELLED", async () => {
  const x = rig((argv) => ok([argv[0]!]), { "notify-cred": "VALUE-A" });
  const out = await x.s.onToolCall("sitate_notify_set", { secret_refs: ["notify-cred"] });
  assert.equal(x.shown[0]!.mode, "save");
  assert.ok(x.calls[0]!.stdin.includes("VALUE-A"));
  assert.ok(!x.calls[0]!.argv.join(" ").includes("VALUE-A") && !out.includes("VALUE-A"));
  assert.equal(await x.kc.get(serviceFor("repoX"), "notify-cred"), "VALUE-A");
  const y = rig((argv) => ok([argv[0]!]));
  const cancelled = await y.s.onToolCall("sitate_notify_set", { secret_refs: ["notify-cred"] });
  assert.ok(cancelled.includes("HUMAN_CANCELLED"));
  assert.equal(y.calls.length, 0, "核を呼ばない");
});

test("session: the business admin key of init-storage opens no-save, is never stored, and a same-named keychain item is neither read nor dropped", async () => {
  const x = rig((argv) => ok([argv[0]!]), { adm: "BIZ-KEY" });
  await x.s.onToolCall("sitate_init_storage", { admin_ref: "adm", secret_refs: ["adm"], endpoint: "e", bucket: "b" });
  assert.equal(x.shown[0]!.mode, "no-save");
  assert.ok(x.calls[0]!.stdin.includes("BIZ-KEY"));
  assert.ok(!dump(x.kc).includes("BIZ-KEY"));
  const y = rig((argv) => ok([argv[0]!]), { adm: "TYPED-KEY" });
  await y.kc.set(serviceFor("repoX"), "adm", "BIZ-KEY");
  await y.s.onToolCall("sitate_init_storage", { admin_ref: "adm", secret_refs: ["adm"], endpoint: "e", bucket: "b" });
  assert.equal(y.shown[0]?.mode, "no-save", "同名の項目がキーチェーンに有っても読まず、保存しないモードで開く(mp-app B6)");
  assert.ok(y.calls[0]!.stdin.includes("TYPED-KEY") && !y.calls[0]!.stdin.includes("BIZ-KEY"));
  await y.s.onToolCall("sitate_bucket_check", { key_ref: "k" });
  assert.ok(dump(y.kc).includes("BIZ-KEY"), "同名の別の資格情報(元サーバーの鍵など)を消さない");
  const z = rig((argv) => ok([argv[0]!]), { adm: "TYPED-KEY" });
  await z.kc.set(serviceFor("repoX"), "adm", "BIZ-KEY");
  await z.s.onToolCall("sitate_init_storage", { admin_ref: "adm", secret_refs: ["adm"], endpoint: "e", bucket: "b" });
  await z.s.dispose();
});

test("session: apply returning HUMAN_SECRET_REQUIRED opens the no-save box and re-calls once with the value on stdin only", async () => {
  let n = 0;
  const x = rig((argv) => (argv[0] === "apply" && n++ === 0 ? ng(["apply"], "HUMAN_SECRET_REQUIRED", { change_id: "c", required_flags: ["--admin-ref"] }) : ok(["apply"])), { adm: "BIZ-KEY" });
  await x.s.onToolCall("sitate_apply", { admin_ref: "adm", id: "c" });
  assert.equal(x.calls.length, 2);
  assert.equal(x.shown[0]!.mode, "no-save");
  assert.ok(x.calls[1]!.argv.includes("--secrets-stdin") && x.calls[1]!.stdin.includes("BIZ-KEY"));
  assert.ok(!dump(x.kc).includes("BIZ-KEY"));
});

test("session: a source survey whose credential was rejected (connect/auth) removes that ref from the keychain", async () => {
  const x = rig((argv) => ng([argv[0]!, argv[1]!], "SOURCE_SURVEY_FAILED", { step: "connect", reason: "auth" }));
  await x.kc.set(serviceFor("repoX"), "src", "SRC-CRED");
  await x.s.onToolCall("sitate_source_survey", { admin_ref: "src", secret_refs: ["src"] });
  assert.equal(await x.kc.get(serviceFor("repoX"), "src"), null);
  const y = rig((argv) => ng([argv[0]!, argv[1]!], "SOURCE_SURVEY_FAILED", { step: "connect", reason: "timeout" }));
  await y.kc.set(serviceFor("repoX"), "src", "SRC-CRED");
  await y.s.onToolCall("sitate_source_survey", { admin_ref: "src", secret_refs: ["src"] });
  assert.equal(await y.kc.get(serviceFor("repoX"), "src"), "SRC-CRED", "認証以外の失敗では消さない");
});

test("session: SSH failures become app envelopes (reason in detail for SSH_UNREACHABLE), and state-changing tools get a --run-id", async () => {
  const x = rig(() => new SshError({ code: "SSH_UNREACHABLE", reason: "refused" }));
  const out = await x.s.onToolCall("sitate_status", {});
  assert.ok(out.includes("SSH_UNREACHABLE") && out.includes("refused"));
  const y = rig(() => new SshError({ code: "SSH_HOSTKEY_MISMATCH" }));
  assert.ok((await y.s.onToolCall("sitate_status", {})).includes("SSH_HOSTKEY_MISMATCH"));
  const z = rig((argv) => ok([argv[0]!]));
  await z.s.onToolCall("sitate_backup", {});
  assert.match(z.calls[0]!.argv.join(" "), /--run-id r-\d{8}T\d{6}Z-[0-9a-f]{4}$/);
  await z.s.onToolCall("sitate_backup", { run_id: "r-20261103T010000Z-abcd" });
  assert.ok(z.calls[1]!.argv.join(" ").endsWith("--run-id r-20261103T010000Z-abcd"));
});

test("session: bootstrap runs through the app (not the core runner); a missing admin_ref opens the save box first", async () => {
  const x = rig(() => ok(["x"]), { adm: "ADMIN-CRED" });
  await x.s.onToolCall("sitate_bootstrap", { host: "h", admin_user: "root", admin_ref: "adm", secret_refs: ["adm"] });
  assert.equal(x.calls.length, 0);
  assert.equal(x.bootstraps.length, 1);
  assert.equal(x.shown[0]!.mode, "save");
  assert.equal(await x.kc.get(SERVICE_PRE_ADMIN, "adm"), "ADMIN-CRED");
  const y = rig(() => ok(["x"]));
  const out = await y.s.onToolCall("sitate_bootstrap", { host: "h", admin_user: "root", admin_ref: "adm", secret_refs: ["adm"] });
  assert.ok(out.includes("HUMAN_CANCELLED") && y.bootstraps.length === 0);
});
