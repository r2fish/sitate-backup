// [mp-a2] 参照名を secret_refs だけで渡しても(全道具の説明が勧める形)、道具から決めたラベルで入力ボックスを開く。
import assert from "node:assert/strict";
import { test } from "node:test";
import { collectSecrets } from "../../src/main/secret-flow.ts";
import { SECRET_LABEL } from "../../src/main/secret-box.ts";

const run = async (tool: string, input: Record<string, unknown>, refs: string[]): Promise<{ labels: string[]; modes: string[]; values: Record<string, unknown> | null }> => {
  const labels: string[] = [];
  const modes: string[] = [];
  const keychain = { get: async () => null } as never;
  const secretBox = { request: async (mode: string, _ref: string, label: string) => { modes.push(mode); labels.push(label); return { kind: "transient", value: { take: () => "line1\nline2" } }; } } as never;
  const values = await collectSecrets({ keychain, service: () => "svc", secretBox }, { tool, input, secretRefs: refs } as never);
  return { labels, modes, values };
};

test("[mp-a2] secret_refs だけの参照名: 保管先の管理用鍵・元サーバー・復元先・Webhook のラベルになり、事業者の鍵は保存しないモード", async () => {
  const a = await run("sitate_init_storage", { secret_refs: ["admin"] }, ["admin"]);
  assert.deepEqual(a.labels, [SECRET_LABEL.storageAdmin]);
  assert.deepEqual(a.modes, ["no-save"]);
  assert.deepEqual(a.values, { admin: { key_id: "line1", secret: "line2" } });
  assert.deepEqual((await run("sitate_source_enroll", { secret_refs: ["s"] }, ["s"])).labels, [SECRET_LABEL.sourceAdmin]);
  assert.deepEqual((await run("sitate_restore_dest_enroll", { secret_refs: ["d"] }, ["d"])).labels, [SECRET_LABEL.destAdmin]);
  assert.deepEqual((await run("sitate_notify_set", { secret_refs: ["w"] }, ["w"])).labels, [SECRET_LABEL.webhook]);
  assert.deepEqual((await run("sitate_status", { secret_refs: ["x"] }, ["x"])).labels, [SECRET_LABEL.generic], "決められない道具は一般の文");
});

test("[mp-fix F2] sitate_apply で secret_refs だけの参照名は、3 種の秘密のどれか決められず一般の文(保管先の管理用鍵と取り違えない)", async () => {
  const r = await run("sitate_apply", { secret_refs: ["a", "b"] }, ["a", "b"]);
  assert.deepEqual(r.labels, [SECRET_LABEL.generic, SECRET_LABEL.generic]);
  assert.deepEqual(r.modes, ["save", "save"], "事業者の鍵の保存しないモードにしない");
});

test("[mp-fix F2] sitate_init は key_ref を除いて参照名が 1 つだけのときに限り管理用鍵になる(key_ref の併記・複数なら一般の文)", async () => {
  assert.deepEqual((await run("sitate_init", { key_ref: "k", secret_refs: ["adm"] }, ["adm"])).labels, [SECRET_LABEL.storageAdmin]);
  const both = await run("sitate_init", { key_ref: "k", secret_refs: ["k", "adm"] }, ["k", "adm"]);
  assert.deepEqual(both.labels, [SECRET_LABEL.generic, SECRET_LABEL.storageAdmin], "key_ref は管理用鍵のラベルにしない");
  assert.deepEqual((await run("sitate_init", { key_ref: "k", secret_refs: ["x", "y"] }, ["x", "y"])).labels, [SECRET_LABEL.generic, SECRET_LABEL.generic]);
});
