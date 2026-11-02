// 参照名の字種(H1): 改行や引用符を含む参照名は、どの入口でもキーチェーンの命令に届かない。
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { REF_NAME_PATTERN } from "../../src/constants.ts";
import { MacKeychain, MemoryKeychain, serviceFor } from "../../src/main/keychain.ts";
import { PathRefTable } from "../../src/main/path-ref.ts";
import { Session } from "../../src/main/session.ts";
import { SoftwareApprovalKey } from "../../src/main/approval-sign.ts";
import { authorizeToolCall } from "../../src/main/tool-gate.ts";
import { ToolRegistry } from "../../src/main/tool-registry.ts";

const toolsDir = join(import.meta.dirname, "..", "..", "src", "tools");
const registry = ToolRegistry.fromDir(toolsDir);
const EVIL = 'x"\ndelete-generic-password -s jp.sitate-backup.app.app -a approval-key\ny';
const BAD = [EVIL, "a b", 'a"b', "a\\b", "-x", "", "a b", "a".repeat(65), "a;b", "日本語"];

class SpyKeychain extends MemoryKeychain {
  touched = 0;
  override async get(s: string, a: string): Promise<string | null> { this.touched++; return super.get(s, a); }
  override async list(prefix?: string) { this.touched++; return super.list(prefix); }
}

test("ref: gate rejects a bad reference name in secret_refs, *_ref keys and nested definitions", () => {
  for (const bad of BAD) {
    const inputs: [string, unknown][] = [
      ["sitate_source_survey", { admin_ref: "ok", secret_refs: [bad] }],
      ["sitate_bootstrap", { host: "h", admin_user: "u", admin_ref: bad }],
      ["sitate_init", { admin_ref: bad, secret_refs: ["ok"] }],
      ["sitate_bucket_check", { key_ref: bad }],
      ["sitate_notify_set", { secret_ref: bad }],
    ];
    for (const [name, input] of inputs) {
      const r = authorizeToolCall(registry, { id: "t", name, input }, new PathRefTable());
      assert.ok(!r.ok && r.envelope.error.code === "TOOL_ARG_INVALID", `${name} ${JSON.stringify(bad)}`);
    }
  }
  const nested = authorizeToolCall(registry, { id: "t", name: "sitate_def_validate", input: { definition: { a: [{ admin_ref: EVIL }] } } }, new PathRefTable());
  assert.ok(!nested.ok && nested.envelope.error.code === "TOOL_ARG_INVALID", "入れ子の定義の中の参照名");
  const good = authorizeToolCall(registry, { id: "t", name: "sitate_source_survey", input: { admin_ref: "adm.1_x-y", secret_refs: ["adm.1_x-y", "SENTINEL-REF"] } }, new PathRefTable());
  assert.ok(good.ok);
});

test("ref: the tool definitions carry the same pattern as the constant, on every *_ref and secret_refs item", () => {
  let n = 0;
  const walk = (name: string, o: unknown): void => {
    if (Array.isArray(o)) return o.forEach((x) => walk(name, x));
    if (typeof o !== "object" || o === null) return;
    const props = (o as { properties?: Record<string, { type?: string; pattern?: string; items?: { pattern?: string } }> }).properties;
    for (const [k, v] of Object.entries(props ?? {})) {
      if (k.endsWith("_ref") && v.type === "string") { assert.equal(v.pattern, REF_NAME_PATTERN, `${name}.${k}`); n++; }
      if (k === "secret_refs") { assert.equal(v.items?.pattern, REF_NAME_PATTERN, `${name}.secret_refs`); n++; }
    }
    Object.values(o).forEach((x) => walk(name, x));
  };
  for (const f of readdirSync(toolsDir).filter((x) => x.endsWith(".json"))) walk(f, JSON.parse(readFileSync(join(toolsDir, f), "utf8")));
  assert.ok(n >= 25, `検査した欄が少ない ${n}`);
});

test("ref: a hijacked reference name never reaches the keychain through Session", async () => {
  const kc = new SpyKeychain();
  const s = new Session({
    registry, refs: new PathRefTable(), keychain: kc, service: () => serviceFor("r"), runner: { async run() { throw new Error("核は呼ばれない"); } },
    t: (k) => k, approvalKey: SoftwareApprovalKey.generate(), now: () => "2026-11-03T00:00:00Z", sendToAgent: () => undefined, onChat: () => undefined,
    onCard: async () => ({ kind: "close" }), onPanel: async () => "done" as const,
  });
  const out = await s.onToolCall("sitate_source_survey", { admin_ref: "ok", secret_refs: [EVIL] });
  assert.ok(out.includes("TOOL_ARG_INVALID"));
  assert.equal(kc.touched, 0);
});

test("ref: MacKeychain refuses control characters before anything is written to security -i", async () => {
  const sent: string[] = [];
  const kc = new MacKeychain(async (_a, stdin) => { sent.push(stdin); return { stdout: "", code: 0 }; });
  for (const bad of [EVIL, "a\nb", "a\rb", "a\u0000b", "a b"]) {
    await assert.rejects(kc.get("svc", bad));
    await assert.rejects(kc.set("svc", bad, "v"));
    await assert.rejects(kc.delete(bad, "acct"));
  }
  assert.deepEqual(sent, []);
  await kc.get("svc", 'a"b');
  assert.equal(sent[0], 'find-generic-password -s "svc" -a "a\\"b" -w\n');
});
