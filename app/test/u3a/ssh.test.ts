import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { SshConnection, SshError } from "../../src/main/ssh/connection.ts";
import type { ConnectOptions } from "../../src/main/ssh/connection.ts";
import { SshCoreRunner, UnsendableArgvError, commandLine, newRunId } from "../../src/main/ssh/core-runner.ts";
import { decideHostKey, fingerprintOf, normalizeFingerprint } from "../../src/main/ssh/fingerprint.ts";
import { TargetStore } from "../../src/main/ssh/target-store.ts";
import { startServer } from "./ssh-server.ts";
import type { TestServer } from "./ssh-server.ts";

const base = (s: TestServer, over: Partial<ConnectOptions> = {}): ConnectOptions => ({
  host: "127.0.0.1", port: s.port, username: "sitate", auth: { privateKey: s.clientPrivate },
  pinnedFp: null, typedFp: null, allowFirstUse: true, connectTimeoutMs: 5000, ...over,
});
const failureOf = async (p: Promise<unknown>): Promise<unknown> => { try { await p; } catch (e) { if (e instanceof SshError) return e.failure; throw e; } return null; };

test("ssh: first use pins the Ed25519 host key, exec carries command and stdin, exit code returns", async () => {
  const s = await startServer({ onExec: (cmd, stdin, st) => { st.write(`${cmd}|${stdin.toString()}`); st.exit(3); st.end(); } });
  const c = new SshConnection();
  try {
    const r = await c.connect(base(s));
    assert.equal(r.hostKeyFp, fingerprintOf(s.hostKeyBlob));
    assert.equal(r.pinnedNow, true);
    const e = await c.exec("status --run-id x", { stdin: "in\n" });
    assert.equal(e.stdout.toString(), "status --run-id x|in\n");
    assert.equal(e.code, 3);
    assert.equal(e.disconnected, false);
    assert.deepEqual([...new Set(s.requests)], ["exec"], "exec 以外(シェル・転送・エージェント転送・pty)を要求しない");
  } finally { c.close(); await s.close(); }
});

test("ssh: a pinned fingerprint that differs refuses with SSH_HOSTKEY_MISMATCH; a matching pin connects without re-pinning", async () => {
  const s = await startServer();
  try {
    assert.deepEqual(await failureOf(new SshConnection().connect(base(s, { pinnedFp: "SHA256:" + "A".repeat(43) }))), { code: "SSH_HOSTKEY_MISMATCH" });
    const c = new SshConnection();
    const r = await c.connect(base(s, { pinnedFp: fingerprintOf(s.hostKeyBlob), allowFirstUse: false }));
    assert.equal(r.pinnedNow, false);
    c.close();
  } finally { await s.close(); }
});

test("ssh: with no pin, a typed fingerprint must match; with neither, first use is refused unless allowed", async () => {
  const s = await startServer();
  try {
    assert.deepEqual(await failureOf(new SshConnection().connect(base(s, { typedFp: "SHA256:" + "B".repeat(43), allowFirstUse: false }))), { code: "SSH_HOSTKEY_MISMATCH" });
    assert.deepEqual(await failureOf(new SshConnection().connect(base(s, { allowFirstUse: false }))), { code: "SSH_HOSTKEY_MISMATCH" });
    const c = new SshConnection();
    assert.equal((await c.connect(base(s, { typedFp: fingerprintOf(s.hostKeyBlob), allowFirstUse: false }))).pinnedNow, true);
    c.close();
  } finally { await s.close(); }
});

test("ssh: a server that shows no Ed25519 host key is SSH_HOSTKEY_MISMATCH", async () => {
  const s = await startServer({ keyType: "rsa" });
  try {
    assert.deepEqual(await failureOf(new SshConnection().connect(base(s))), { code: "SSH_HOSTKEY_MISMATCH" });
  } finally { await s.close(); }
});

test("ssh: a rejected key is SSH_AUTH_DENIED", async () => {
  const s = await startServer();
  const other = await startServer();
  try {
    assert.deepEqual(await failureOf(new SshConnection().connect(base(s, { auth: { privateKey: other.clientPrivate } }))), { code: "SSH_AUTH_DENIED" });
  } finally { await s.close(); await other.close(); }
});

test("ssh: refused port and unresolvable name are SSH_UNREACHABLE with reason refused / dns", async () => {
  const probe = createServer();
  await new Promise<void>((r) => probe.listen(0, "127.0.0.1", () => r()));
  const port = (probe.address() as { port: number }).port;
  await new Promise<void>((r) => probe.close(() => r()));
  const key = (await startServer());
  try {
    assert.deepEqual(await failureOf(new SshConnection().connect(base(key, { port }))), { code: "SSH_UNREACHABLE", reason: "refused" });
    assert.deepEqual(await failureOf(new SshConnection().connect(base(key, { host: "no-such-host.invalid" }))), { code: "SSH_UNREACHABLE", reason: "dns" });
  } finally { await key.close(); }
});

test("ssh: a connection dropped during the output is reported as disconnected (no exit status)", async () => {
  const s = await startServer({ onExec: (_c, _i, st) => { st.write("partial"); st.destroy(); } });
  const c = new SshConnection();
  try {
    await c.connect(base(s));
    const e = await c.exec("status");
    assert.equal(e.disconnected, true);
    assert.equal(e.code, null);
  } finally { c.close(); await s.close(); }
});

test("core runner: sends the argv as one space-joined command through the pinned connection and never re-pins", async () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-ssh-"));
  const s = await startServer({ onExec: (cmd, stdin, st) => { st.write(JSON.stringify({ cmd, stdin: stdin.toString() })); st.exit(0); st.end(); } });
  try {
    const targets = new TargetStore(join(dir, "ssh-target.json"));
    targets.write({ host: "127.0.0.1", port: s.port, user: "sitate", hostKeyFp: fingerprintOf(s.hostKeyBlob), adminPublicKey: null });
    const runner = new SshCoreRunner({ targets, adminKey: async () => s.clientPrivate, connectTimeoutMs: 5000 });
    const out = await runner.run(["def", "validate", "--file", "-"], "{}\n");
    assert.deepEqual(JSON.parse(out.stdout), { cmd: "def validate --file -", stdin: "{}\n" });
    assert.equal(out.end, "complete");
    // 固定値と違うサーバーには送らず、固定値を書き換えない。
    const other = await startServer();
    try {
      targets.write({ host: "127.0.0.1", port: other.port, user: "sitate", hostKeyFp: fingerprintOf(s.hostKeyBlob), adminPublicKey: null });
      await assert.rejects(runner.run(["status"], ""), (e: unknown) => e instanceof SshError && e.failure.code === "SSH_HOSTKEY_MISMATCH");
      assert.equal(targets.read()?.hostKeyFp, fingerprintOf(s.hostKeyBlob));
    } finally { await other.close(); }
    await assert.rejects(runner.run(["status", "a b"], ""), UnsendableArgvError);
  } finally { await s.close(); rmSync(dir, { recursive: true, force: true }); }
});

test("command line: words with whitespace, control characters or empty words are unsendable (spec 3.1.5 rules 2 and 6)", () => {
  assert.equal(commandLine(["propose", "seal", "--file", "-"]), "propose seal --file -");
  for (const bad of [[""], ["a b"], ["a\tb"], ["a\nb"], ["a\u007fb"], ["x", ""]]) assert.equal(commandLine(bad), null);
  assert.equal(commandLine([]), null);
  assert.match(newRunId(new Date("2026-11-03T04:05:06.789Z")), /^r-20261103T040506Z-[0-9a-f]{4}$/);
});

test("host key decision: Ed25519 only; pinned wins; typed fingerprint pins; shape of typed fingerprint", () => {
  const blob = Buffer.concat([Buffer.from([0, 0, 0, 11]), Buffer.from("ssh-ed25519"), Buffer.alloc(36, 7)]);
  const fp = fingerprintOf(blob);
  assert.match(fp, /^SHA256:[A-Za-z0-9+/]{43}$/);
  assert.deepEqual(decideHostKey(blob, fp, null, false), { ok: true, pin: false });
  assert.deepEqual(decideHostKey(blob, null, fp, false), { ok: true, pin: true });
  assert.deepEqual(decideHostKey(blob, null, null, false), { ok: false });
  assert.deepEqual(decideHostKey(blob, "SHA256:" + "C".repeat(43), fp, true), { ok: false });
  const rsa = Buffer.concat([Buffer.from([0, 0, 0, 7]), Buffer.from("ssh-rsa"), Buffer.alloc(20)]);
  assert.deepEqual(decideHostKey(rsa, null, null, true), { ok: false });
  assert.equal(normalizeFingerprint(` ${fp}\n`), fp);
  assert.equal(normalizeFingerprint("SHA256:short"), null);
});

test("target store: a pin is written once and a different value never overwrites it", () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-ssh-"));
  try {
    const st = new TargetStore(join(dir, "x", "t.json"));
    assert.equal(st.pin("SHA256:" + "A".repeat(43)), false, "接続先が無ければ固定しない");
    st.write({ host: "h", port: 22, user: "sitate", hostKeyFp: null, adminPublicKey: null });
    assert.equal(st.pin("SHA256:" + "A".repeat(43)), true);
    assert.equal(st.pin("SHA256:" + "B".repeat(43)), false);
    assert.equal(st.read()?.hostKeyFp, "SHA256:" + "A".repeat(43));
    st.remove();
    assert.equal(st.read(), null);
  } finally { rmSync(dir, { recursive: true, force: true }); }
});
