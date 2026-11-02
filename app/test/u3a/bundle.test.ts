// 同梱物の署名の検証・キーチェーンの命名(完了条件 13・14・27)。試験用の署名者は minisign の形式を自前で作る。
import assert from "node:assert/strict";
import { createHash, generateKeyPairSync, randomBytes, sign } from "node:crypto";
import { test } from "node:test";
import { BUNDLE_REFUSAL, verifyBundle } from "../../src/main/bundle-verify.ts";
import type { BundleFile } from "../../src/main/bundle-verify.ts";
import { KEYCHAIN_SERVICE_PREFIX, MacKeychain, MemoryKeychain, serviceFor } from "../../src/main/keychain.ts";
import { parsePublicKey } from "../../src/main/minisign.ts";
import { trustedKeysOf } from "../../src/trust/embedded.ts";

function signer(): { pub: string; signFile(bytes: Buffer, path: string, version?: string): string } {
  const { privateKey, publicKey } = generateKeyPairSync("ed25519");
  const keyId = randomBytes(8);
  const raw = publicKey.export({ type: "spki", format: "der" }).subarray(-32);
  return {
    pub: Buffer.concat([Buffer.from("Ed"), keyId, raw]).toString("base64"),
    signFile(bytes, path, version = "1.0.0") {
      const sig = sign(null, createHash("blake2b512").update(bytes).digest(), privateKey);
      const trusted = `version=${version} arch=any path=${path}`;
      const global = sign(null, Buffer.concat([sig, Buffer.from(trusted)]), privateKey);
      return `untrusted comment: test\n${Buffer.concat([Buffer.from("ED"), keyId, sig]).toString("base64")}\ntrusted comment: ${trusted}\n${global.toString("base64")}\n`;
    },
  };
}
const file = (s: ReturnType<typeof signer>, path: string, body: string): BundleFile => {
  const bytes = Buffer.from(body);
  return { path, bytes, sig: s.signFile(bytes, path) };
};

test("同梱物: 署名が通り、1 つの改変・置き場の違い・不可視文字・別の鍵(製品/試験の取り違え)で全体が拒否され、固定文だけが出る", () => {
  const test1 = signer();
  const product = signer();
  const keys = trustedKeysOf({ kind: "test", keys: [test1.pub] });
  const files = [file(test1, "manual.md", "説明書"), file(test1, "catalog.json", "{}")];
  assert.deepEqual(verifyBundle(files, keys, "1.0.0"), { ok: true });
  const tampered = [files[0]!, { ...files[1]!, bytes: Buffer.from("{ }") }];
  const r = verifyBundle(tampered, keys, "1.0.0");
  assert.equal(r.ok, false);
  assert.equal(!r.ok && r.path, "catalog.json");
  assert.equal(!r.ok && r.notice, BUNDLE_REFUSAL);
  assert.equal(verifyBundle(files, keys, "2.0.0").ok, false);
  const other = [file(product, "manual.md", "説明書")];
  const rr = verifyBundle(other, keys, "1.0.0");
  assert.equal(!rr.ok && rr.reason, "no_matching_key");
  assert.equal(verifyBundle([file(test1, "manual.md", "a​b")], keys, "1.0.0").ok, false);
  assert.equal(verifyBundle([file(test1, "manual.md", "ok\n")], trustedKeysOf({ kind: "product", keys: [product.pub] }), "1.0.0").ok, false);
  assert.throws(() => trustedKeysOf({ kind: "test", keys: [] }));
  assert.equal(parsePublicKey(test1.pub).publicKey.length, 32);
});

test("キーチェーン: 保存・取り出し・削除・接頭辞での列挙。repo_id で別のサーバーを区別する", async () => {
  const kc = new MemoryKeychain();
  await kc.set(serviceFor("repoA"), "storage", "v1");
  await kc.set(serviceFor("repoB"), "storage", "v2");
  assert.equal(await kc.get(serviceFor("repoA"), "storage"), "v1");
  assert.equal(await kc.get(serviceFor("repoB"), "storage"), "v2");
  assert.equal((await kc.list()).length, 2);
  assert.equal(await kc.delete(serviceFor("repoA"), "storage"), true);
  assert.equal(await kc.get(serviceFor("repoA"), "storage"), null);
  assert.ok((await kc.list()).every((i) => i.service.startsWith(KEYCHAIN_SERVICE_PREFIX)));
});

test("MacKeychain: 秘密の値は security の引数に載らず(標準入力の 16 進だけ)、列挙は dump の形から読む", async () => {
  const calls: { args: readonly string[]; stdin: string }[] = [];
  const kc = new MacKeychain(async (args, stdin) => {
    calls.push({ args, stdin });
    if (args[0] === "dump-keychain") return { stdout: 'keychain: "x"\n    "acct"<blob>="storage"\n    "svce"<blob>="' + serviceFor("r1") + '"\nkeychain: "x"\n    "acct"<blob>="o"\n    "svce"<blob>="other.app"\n', code: 0 };
    return { stdout: "value\n", code: 0 };
  });
  await kc.set(serviceFor("r1"), "storage", "TOP-SECRET-VALUE");
  assert.ok(calls.every((c) => c.args.every((a) => !a.includes("TOP-SECRET"))));
  assert.ok(!calls[0]!.stdin.includes("TOP-SECRET-VALUE") && calls[0]!.stdin.includes(Buffer.from("TOP-SECRET-VALUE").toString("hex")));
  assert.equal(await kc.get(serviceFor("r1"), "storage"), "value");
  assert.deepEqual(await kc.list(), [{ service: serviceFor("r1"), account: "storage" }]);
});
