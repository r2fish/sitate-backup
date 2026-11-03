// mp-app C3: 秘密の入力欄は複数行の値(OpenSSH 形式の秘密鍵)を保てる。読めない鍵は SSH_AUTH_DENIED(参照名の値を消す扱い)になる。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { SshConnection, SshError } from "../../src/main/ssh/connection.ts";
import { scriptFor } from "../../src/testing/driver-server.ts";
import { startServer } from "./ssh-server.ts";

const html = readFileSync(join(import.meta.dirname, "..", "..", "src", "renderer", "index.html"), "utf8");
const css = readFileSync(join(import.meta.dirname, "..", "..", "src", "renderer", "style.css"), "utf8");

test("[mp-app C3] 秘密の入力欄は複数行を保てる textarea で、文字を伏せ、提出に data-action がある", () => {
  assert.match(html, /<textarea id="secret-input"/);
  assert.doesNotMatch(html, /<input id="secret-input"/);
  assert.match(css, /#secret-input\s*\{[^}]*-webkit-text-security:\s*disc/);
  assert.match(html, /<button id="secret-submit" data-action="secret-submit"/);
});

test("[mp-app C3] 試験用ドライバの fill は textarea にも届く", () => {
  assert.match(scriptFor({ op: "fill", surface: "secret-box", target: "x", value: "a\nb" }), /textarea/);
});

test("[mp-app C3] 改行を失った(読めない)秘密鍵での接続は例外でなく SSH_AUTH_DENIED になる", async () => {
  const s = await startServer();
  try {
    const flat = s.clientPrivate.replace(/\n/g, "");
    const c = new SshConnection();
    await assert.rejects(c.connect({ host: "127.0.0.1", port: s.port, username: "u", auth: { privateKey: flat }, pinnedFp: null, typedFp: null, allowFirstUse: true, connectTimeoutMs: 3000 }), (e: unknown) => e instanceof SshError && e.failure.code === "SSH_AUTH_DENIED");
    // 原文(複数行)は通る。
    const ok = new SshConnection();
    await ok.connect({ host: "127.0.0.1", port: s.port, username: "u", auth: { privateKey: s.clientPrivate }, pinnedFp: null, typedFp: null, allowFirstUse: true, connectTimeoutMs: 3000 });
    ok.close();
  } finally { await s.close(); }
});

test("[mp-app C4] キーチェーンへの保存が失敗しても、入力ボックスの待ちは解ける", async () => {
  const { SecretBox } = await import("../../src/main/secret-box.ts");
  const { MemoryKeychain } = await import("../../src/main/keychain.ts");
  class Failing extends MemoryKeychain { override async set(): Promise<void> { throw new Error("KEYCHAIN_WRITE_FAILED"); } }
  const box = new SecretBox({ keychain: new Failing(), service: () => "svc", show: () => undefined });
  const p = box.request("save", "ref1", "l");
  await box.submit("ref1", "value");
  assert.deepEqual(await p, { kind: "cancelled" });
});
