// mp-app C7: sitate_init は主プロセスが --approval-pubkey と --secret-fd 3 を付ける。エージェントは承認鍵の公開鍵を渡せない。
import assert from "node:assert/strict";
import { test } from "node:test";
import { createPublicKey } from "node:crypto";
import { env, harness, registry } from "./mp-helpers.ts";

test("[mp-app C7] 道具 sitate_init の定義に approval_pubkey が無く、渡すと TOOL_ARG_INVALID", async () => {
  const props = (registry.definitions().find((d) => d.name === "sitate_init")!.input_schema as { properties: Record<string, unknown> }).properties;
  assert.ok(!("approval_pubkey" in props));
  const h = harness(() => env(["init"], {}));
  const out = await h.s.onToolCall("sitate_init", { approval_pubkey: "QQ==", admin_ref: "admin" });
  assert.ok(out.includes("TOOL_ARG_INVALID"));
  assert.equal(h.calls.length, 0);
});

test("[mp-app C7] 主プロセスが承認鍵の公開鍵(ssh-ed25519 の base64 の部分)と --secret-fd 3 を付ける", async () => {
  const h = harness(() => env(["init"], { initialized: false }));
  await h.s.onToolCall("sitate_init", { admin_ref: "admin", endpoint: "http://x", bucket: "b" });
  const argv = h.calls[0]!.argv;
  assert.equal(argv[0], "init");
  const i = argv.indexOf("--approval-pubkey");
  assert.ok(i > 0);
  const blob = Buffer.from(argv[i + 1]!, "base64");
  // wire 形式: string "ssh-ed25519" + string 鍵(32 バイト) = 51 バイト
  assert.equal(blob.length, 51);
  assert.equal(blob.readUInt32BE(0), 11);
  assert.equal(blob.subarray(4, 15).toString(), "ssh-ed25519");
  const raw = createPublicKey({ key: h.key.publicKeyDer, format: "der", type: "spki" }).export({ type: "spki", format: "der" }).subarray(-32);
  assert.ok(blob.subarray(19).equals(raw));
  assert.equal(argv[argv.indexOf("--secret-fd") + 1], "3");
});
