import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { ToolRegistry } from "../../src/main/tool-registry.ts";
import { authorizeToolCall, buildStdin } from "../../src/main/tool-gate.ts";
import { PathRefTable } from "../../src/main/path-ref.ts";
import { checkKindArgs } from "../../src/main/tool-kind-args.ts";

// 主プロセスの許可リストと引数の検査(作業指示書 5.14 の完了条件 2・3・26)。
const registry = ToolRegistry.fromDir(join(import.meta.dirname, "..", "..", "src", "tools"));
const call = (name: string, input: unknown) => ({ id: "t1", name, input });

test("gate: a tool name outside the definitions is denied as TOOL_DENIED and nothing is planned", () => {
  for (const name of ["sitate_gate", "sitate_approve", "sitate_phrase_verify", "sitate_restore_fetch", "sitate_secrets_set", "bash", "", "sitate_test_tick"]) {
    const r = authorizeToolCall(registry, call(name, {}), new PathRefTable());
    assert.equal(r.ok, false, name);
    if (!r.ok) {
      assert.equal(r.envelope.error.code, "TOOL_DENIED");
      assert.equal(r.envelope.ok, false);
    }
  }
});

test("gate: an argument that breaks the definition's type is TOOL_ARG_INVALID", () => {
  const bad: [string, unknown][] = [
    ["sitate_status", { zzz: 1 }], ["sitate_restore", { paths: "x" }], ["sitate_propose", {}], ["sitate_propose", { kind: "nope" }],
    ["sitate_def_validate", { definition: "text" }], ["sitate_attest", { step: "dest_disposed" }], ["sitate_attest", { step: "admin_access_closed" }],
    ["sitate_backup", "string"], ["sitate_backup", null], ["sitate_apply", { dry_run: "yes" }],
  ];
  for (const [name, input] of bad) {
    const r = authorizeToolCall(registry, call(name, input), new PathRefTable());
    assert.ok(!r.ok && r.envelope.error.code === "TOOL_ARG_INVALID", `${name} ${JSON.stringify(input)}`);
  }
});

test("gate: a propose whose kind and arguments do not fit is rejected before running", () => {
  const r = authorizeToolCall(registry, call("sitate_propose", { kind: "delay-change", source: {} }), new PathRefTable());
  assert.ok(!r.ok && r.envelope.error.code === "TOOL_ARG_INVALID");
  const ok = authorizeToolCall(registry, call("sitate_propose", { kind: "delay-change", hours: 48, dry_run: true }), new PathRefTable());
  assert.ok(ok.ok);
  if (ok.ok) assert.deepEqual(ok.plan.argv, ["propose", "delay-change", "--hours", "48", "--dry-run"]);
});

test("gate: the five kinds the vectors leave out accept their rows and reject other kinds' arguments", () => {
  const good: [string, Record<string, unknown>][] = [
    ["storage-init", { endpoint: "e", bucket: "b", prefix: "p", adapter: "b2", reissue: true }], ["storage-init", {}],
    ["restore-elsewhere", { backup: "latest", to: "ssh://h:/d", paths: ["/a"], checks: "all" }],
    ["restore-session-extend", { session: "s1", hold_hours: 48 }], ["restore-session-extend", { backup: "latest", to: "restore-dest:/d", hold_hours: 48 }],
    ["restore-dest-release", { session: "s1", authorized_key: "ssh-ed25519 AAAA", allow_from: ["1.2.3.4"] }],
    ["storage-read-extra", { backup: "latest", to: "/d" }], ["storage-read-extra", { force: true }],
  ];
  const bad: [string, Record<string, unknown>][] = [
    ["storage-init", { hours: 1 }], ["storage-init", { restore: {} }], ["restore-elsewhere", { backup: "latest" }], ["restore-elsewhere", { backup: "latest", to: "/d", force: true }],
    ["restore-session-extend", { session: "s1" }], ["restore-session-extend", { session: "s1", hold_hours: 48, backup: "latest" }], ["restore-session-extend", { hold_hours: 48 }],
    ["restore-dest-release", { authorized_key: "k" }], ["restore-dest-release", { session: "s1", definition: {} }],
    ["storage-read-extra", { force: true, backup: "latest", to: "/d" }], ["storage-read-extra", {}], ["storage-read-extra", { source: {} }],
  ];
  for (const [k, a] of good) assert.ok(checkKindArgs(k, a).ok, `${k} ${JSON.stringify(a)}`);
  for (const [k, a] of bad) assert.ok(!checkKindArgs(k, a).ok, `${k} ${JSON.stringify(a)}`);
});

test("gate: path_ref returns to the real value at any depth, an unknown or partial pr: value is PATH_REF_UNKNOWN", () => {
  const table = new PathRefTable();
  const ref = table.refFor("/srv/app/data");
  assert.match(ref, /^pr:[0-9a-f]{16}$/);
  assert.equal(table.refFor("/srv/app/data"), ref, "同じ値に同じ参照名");
  assert.notEqual(table.refFor("/srv/other"), ref);
  const ok = authorizeToolCall(registry, call("sitate_restore", { backup: "latest", to: "/restore", paths: [ref] }), table);
  assert.ok(ok.ok);
  if (ok.ok) assert.ok(ok.plan.argv.includes(JSON.stringify(["/srv/app/data"])));
  const def = authorizeToolCall(registry, call("sitate_def_validate", { definition: { source: { targets: [{ root: ref }] }, storage: [{ key_ref: "key-a" }] } }), table);
  assert.ok(def.ok);
  if (def.ok) assert.deepEqual(def.plan.definition, { source: { targets: [{ root: "/srv/app/data" }] }, storage: [{ key_ref: "key-a" }] }, "key_ref は値に置き換えない");
  for (const bad of ["pr:0123456789abcdef", ref + "/sub", "pr:ZZ"]) {
    const r = authorizeToolCall(registry, call("sitate_restore", { backup: "latest", to: "/r", paths: [bad] }), table);
    assert.ok(!r.ok && r.envelope.error.code === "PATH_REF_UNKNOWN", bad);
  }
  const fresh = new PathRefTable();
  assert.notEqual(fresh.refFor("/srv/app/data"), ref, "再起動の後は別の参照名");
  const old = authorizeToolCall(registry, call("sitate_restore", { backup: "latest", to: "/r", paths: [ref] }), fresh);
  assert.ok(!old.ok && old.envelope.error.code === "PATH_REF_UNKNOWN");
  const human = authorizeToolCall(registry, call("sitate_restore", { backup: "latest", to: "/r", paths: ["/home/user/docs"] }), fresh);
  assert.ok(human.ok, "人がチャットで与えたパスは実値のまま");
});

test("gate: secret_refs stay names in the plan; values enter only through buildStdin, secrets-stdin first then the definition", () => {
  const r = authorizeToolCall(registry, call("sitate_propose", { kind: "seal", definition: { version: 1 }, secret_refs: ["admin"] }), new PathRefTable());
  assert.ok(r.ok);
  if (!r.ok) return;
  assert.deepEqual(r.plan.secretRefs, ["admin"]);
  assert.ok(r.plan.argv.includes("--secrets-stdin") && r.plan.argv.includes("--file"));
  assert.ok(!JSON.stringify(r.plan).includes("SECRET-VALUE"));
  assert.equal(buildStdin(r.plan, { admin: { key_id: "k", secret: "SECRET-VALUE" } }), '{"admin":{"key_id":"k","secret":"SECRET-VALUE"}}\n{"version":1}\n');
});
