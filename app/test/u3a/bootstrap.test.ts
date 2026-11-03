import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import ssh2 from "ssh2";
import { runBootstrap } from "../../src/main/bootstrap/bootstrap.ts";
import type { BootstrapDeps, BundleReader } from "../../src/main/bootstrap/bootstrap.ts";
import { fingerprintOf } from "../../src/main/ssh/fingerprint.ts";
import { TargetStore } from "../../src/main/ssh/target-store.ts";
import { parsePublicKey } from "../../src/main/minisign.ts";
import { newSigner } from "./minisign-signer.ts";
import { startServer } from "./ssh-server.ts";
import type { TestServer } from "./ssh-server.ts";

const { utils } = ssh2;
const VERSION = "1.0.0";
const ARCH = "linux-x86_64";

/** 相手のホストの代わり。コマンドを記録し、置かれたファイルの内容とユーザーの有無などの状態を持つ。 */
class FakeHost {
  files = new Map<string, Buffer>();
  log: string[] = [];
  userExists = false;
  binExists = false;
  authorizedKeys = "";
  failPackages = false;
  uid = "0";
  handler = (cmd: string, stdin: Buffer, st: ssh2.ServerChannel): void => {
    this.log.push(cmd);
    const out = (s: string, code = 0): void => { st.write(s); st.exit(code); st.end(); };
    if (cmd === "uname -m") return out("x86_64\n");
    if (cmd === "cat /etc/os-release") return out('ID=debian\nID_LIKE=""\n');
    if (cmd === "id -u") return out(this.uid + "\n");
    if (cmd === "id sitate") return out("", this.userExists ? 0 : 1);
    if (cmd.startsWith("test -x")) return out("", this.binExists ? 0 : 1);
    if (cmd === "command -v sudo >/dev/null 2>&1") return out("");
    if (cmd === "version") return out(JSON.stringify({ schema: "sitate.out/v1", command: ["version"], ok: true, result: { version: "0.1.0", signing_key_fp: "fp" } }) + "\n");
    const inner = /^(?:sudo -n )?sh -c '([\s\S]*)'$/.exec(cmd)?.[1]?.replace(/'\\''/g, "'");
    if (inner === undefined) return out("", 127);
    const put = /cat > '([^']+)'/.exec(inner);
    if (inner.includes("apt-get") && this.failPackages) return out("", 100);
    if (inner.startsWith("sha256sum")) { const p = /'([^']+)'/.exec(inner)![1]!; const f = this.files.get(p); return f ? out(`${createHash("sha256").update(f).digest("hex")}  ${p}\n`) : out("", 1); }
    if (inner.startsWith("cat '") && inner.includes("authorized_keys") && inner.includes("|| true")) return out(this.authorizedKeys);
    if (put) { this.files.set(put[1]!, stdin); if (put[1]!.endsWith("authorized_keys")) this.authorizedKeys = stdin.toString(); return out(""); }
    return out("");
  };
}

function setup(host: FakeHost, server: TestServer, dir: string, over: Partial<BootstrapDeps> = {}) {
  const signer = newSigner();
  const files = new Map<string, Buffer>();
  const sigs = new Map<string, string>();
  const add = (rel: string, body: string, arch = ARCH): void => { const b = Buffer.from(body); files.set(rel, b); sigs.set(rel, signer.signFile(b, rel, VERSION, arch)); };
  add(`${ARCH}/sitate`, "CORE");
  // アダプタの実行ファイルは署名せず、署名付きの adapter.json の sha256 で照らす(spec 3.2.1。tools/sign.sh)。
  const addAdapter = (arch: string, exeBody: string): void => {
    add(`${arch}/adapters/notify/mail/adapter.json`, JSON.stringify({ kind: "notify", name: "mail", exe: "run", sha256: createHash("sha256").update(exeBody).digest("hex") }), arch);
    files.set(`${arch}/adapters/notify/mail/run`, Buffer.from(exeBody));
  };
  addAdapter(ARCH, "ADAPTER");
  add("prices/prices.json", "{}", "any");
  // 動いているホストと別の arch の核とアダプタ、ソースのアーカイブ(phrase verify が保管先の bin/ へ写すために版ディレクトリに要る。spec 4.2。C1)。
  add("linux-arm64/sitate", "CORE-ARM", "linux-arm64");
  addAdapter("linux-arm64", "ADAPTER-ARM");
  add(`sitate-${VERSION}-src.tar.gz`, "SRC", "any");
  add(`sitate-${VERSION}.spdx.json`, "{}", "any");
  add("install.md", "HANDBOOK", "any"); // アプリの同梱物(配布物 bundle/<version>/ ではない)。サーバーへは置かない
  const bundle: BundleReader = { version: VERSION, list: (p) => [...files.keys()].filter((k) => k.startsWith(p)), read: (rel) => (files.has(rel) ? { bytes: files.get(rel)!, sig: sigs.get(rel) ?? "" } : null) };
  const state = { cleared: 0, savedKey: null as string | null };
  const targets = new TargetStore(join(dir, "ssh-target.json"));
  const deps: BootstrapDeps = {
    targets, bundle, keys: [parsePublicKey(signer.pub)],
    adminCredential: async () => (state.cleared > 0 ? null : server.clientPrivate), clearAdminCredential: async () => { state.cleared++; },
    loadAdminKey: async () => state.savedKey, saveAdminKey: async (k) => { state.savedKey = k; },
    typedFingerprint: () => null, connectTimeoutMs: 5000, ...over,
  };
  return { deps, state, targets, files, signer };
}
const input = (port: number, over: Record<string, unknown> = {}) => ({ host: "127.0.0.1", port, admin_user: "root", admin_ref: "admin", rekey: false, ...over }) as Parameters<typeof runBootstrap>[0];

test("bootstrap: the full sequence places verified files, registers the gate line, checks version and only then closes the access", async () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-bs-"));
  const host = new FakeHost();
  const server = await startServer({ acceptAnyKey: true, onExec: host.handler });
  try {
    const t = setup(host, server, dir);
    const env = await runBootstrap(input(server.port), t.deps) as { ok: boolean; command: string[]; result: Record<string, unknown> };
    assert.equal(env.ok, true);
    assert.deepEqual(env.command, ["bootstrap"]);
    assert.equal(env.result.host_key_fp, fingerprintOf(server.hostKeyBlob));
    assert.equal(env.result.host_key_pinned, true);
    assert.equal(env.result.core_version, "0.1.0");
    assert.equal(env.result.admin_key_type, "software");
    assert.equal(env.result.rekeyed, false);
    assert.equal(env.result.replaced_key_lines, 0);
    assert.equal(env.result.admin_access_closed, true);
    assert.equal(host.files.get("/usr/local/bin/sitate")?.toString(), "CORE");
    assert.equal(host.files.get("/usr/libexec/sitate/adapters/notify/mail/run")?.toString(), "ADAPTER");
    assert.equal(host.files.get("/usr/libexec/sitate/prices/prices.json")?.toString(), "{}");
    assert.ok(host.files.has(`/usr/libexec/sitate/bundle/${VERSION}/${ARCH}/sitate`));
    // C1: 版ディレクトリは配布物の全体(両方の arch、ソースのアーカイブ)。署名のあるものは .sig ごと、アダプタの実行ファイルは .sig なし。
    const vd = `/usr/libexec/sitate/bundle/${VERSION}`;
    assert.equal(host.files.get(`${vd}/linux-arm64/sitate`)?.toString(), "CORE-ARM");
    assert.ok(host.files.has(`${vd}/linux-arm64/sitate.sig`) && host.files.has(`${vd}/${ARCH}/sitate.sig`));
    assert.equal(host.files.get(`${vd}/linux-arm64/adapters/notify/mail/run`)?.toString(), "ADAPTER-ARM");
    assert.equal(host.files.get(`${vd}/${ARCH}/adapters/notify/mail/run`)?.toString(), "ADAPTER");
    assert.ok(host.files.has(`${vd}/sitate-${VERSION}-src.tar.gz`) && host.files.has(`${vd}/sitate-${VERSION}-src.tar.gz.sig`));
    assert.ok(host.files.has(`${vd}/sitate-${VERSION}.spdx.json`));
    assert.ok([...host.files.keys()].every((k) => !k.includes("install.md")), "F10: アプリの同梱物(手順書など)は置かない");
    // C2: アダプタの実行ファイルに .sig は無い(置かない)。動いているホストの arch のアダプタだけが置き場の adapters/ にある。
    assert.ok(!host.files.has(`${vd}/${ARCH}/adapters/notify/mail/run.sig`) && !host.files.has("/usr/libexec/sitate/adapters/notify/mail/run.sig"));
    assert.ok(host.files.has("/usr/libexec/sitate/adapters/notify/mail/adapter.json.sig"));
    assert.match(host.authorizedKeys, /^command="\/usr\/local\/bin\/sitate gate",restrict ssh-ed25519 \S+ sitate-admin\n$/);
    assert.equal(t.targets.read()?.hostKeyFp, fingerprintOf(server.hostKeyBlob));
    assert.equal(t.state.cleared, 1, "閉じた後に admin_ref を消す");
    const iVersion = host.log.indexOf("version");
    const iClose = host.log.findIndex((c) => c.includes("00-sitate.conf"));
    const iAttest = host.log.findIndex((c) => c.includes("attest --step admin_access_closed"));
    assert.ok(iVersion >= 0 && iClose > iVersion && iAttest > iClose, "version → 閉じる → attest の順");
    assert.ok(host.log.some((c) => c.includes("PasswordAuthentication no")) || [...host.files.values()].some((f) => f.toString() === "PasswordAuthentication no\nPermitRootLogin prohibit-password\n"));
    assert.ok(!JSON.stringify(env).includes("PRIVATE KEY"), "封筒に秘密を載せない");
  } finally { await server.close(); rmSync(dir, { recursive: true, force: true }); }
});

test("bootstrap: a signature that does not verify places nothing and returns BOOTSTRAP_SIGNATURE_INVALID", async () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-bs-"));
  const host = new FakeHost();
  const server = await startServer({ acceptAnyKey: true, onExec: host.handler });
  try {
    const t = setup(host, server, dir);
    const evil: BundleReader = { version: VERSION, list: () => [], read: () => ({ bytes: Buffer.from("EVIL"), sig: t.deps.bundle.read(`${ARCH}/sitate`)!.sig }) };
    const env = await runBootstrap(input(server.port), { ...t.deps, bundle: evil }) as { ok: boolean; error: { code: string } };
    assert.equal(env.ok, false);
    assert.equal(env.error.code, "BOOTSTRAP_SIGNATURE_INVALID");
    assert.equal(host.files.size, 0);
    assert.equal(t.state.cleared, 0);
  } finally { await server.close(); rmSync(dir, { recursive: true, force: true }); }
});

test("bootstrap: a file that lands with a different SHA-256 is BOOTSTRAP_SIGNATURE_INVALID", async () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-bs-"));
  const host = new FakeHost();
  const server = await startServer({ acceptAnyKey: true, onExec: (c, i, s) => { host.handler(c, i, s); if (c.includes("/usr/local/bin/sitate") && c.includes("cat >")) host.files.set("/usr/local/bin/sitate", Buffer.from("TAMPERED")); } });
  try {
    const t = setup(host, server, dir);
    const env = await runBootstrap(input(server.port), t.deps) as { error: { code: string } };
    assert.equal(env.error.code, "BOOTSTRAP_SIGNATURE_INVALID");
  } finally { await server.close(); rmSync(dir, { recursive: true, force: true }); }
});

test("bootstrap: a package failure names the step; the pinned host key and prior steps stay, and a rerun can continue", async () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-bs-"));
  const host = new FakeHost();
  host.failPackages = true;
  const server = await startServer({ acceptAnyKey: true, onExec: host.handler });
  try {
    const t = setup(host, server, dir);
    const env = await runBootstrap(input(server.port), t.deps) as { ok: boolean; error: { code: string; detail: { step: string }; retryable: boolean } };
    assert.equal(env.error.code, "BOOTSTRAP_PACKAGE_FAILED");
    assert.equal(env.error.detail.step, "packages");
    assert.equal(env.error.retryable, true);
    assert.equal(t.state.cleared, 0, "閉じていない");
    host.failPackages = false;
    const again = await runBootstrap(input(server.port), t.deps) as { ok: boolean; result: { host_key_pinned: boolean; admin_access_closed: boolean } };
    assert.equal(again.ok, true);
    assert.equal(again.result.host_key_pinned, false, "固定済みの値と照らす");
  } finally { await server.close(); rmSync(dir, { recursive: true, force: true }); }
});

test("bootstrap: another admin key already registered is BOOTSTRAP_ALREADY_DONE without rekey; rekey replaces it and counts the lines", async () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-bs-"));
  const host = new FakeHost();
  host.userExists = true;
  host.binExists = true;
  const other = utils.parseKey(utils.generateKeyPairSync("ed25519").public);
  if (other instanceof Error) throw other;
  host.authorizedKeys = `command="/usr/local/bin/sitate gate",restrict ssh-ed25519 ${other.getPublicSSH().toString("base64")} old\n`;
  const server = await startServer({ acceptAnyKey: true, onExec: host.handler });
  try {
    const t = setup(host, server, dir);
    const env = await runBootstrap(input(server.port), t.deps) as { error: { code: string } };
    assert.equal(env.error.code, "BOOTSTRAP_ALREADY_DONE");
    t.targets.remove();
    const re = await runBootstrap(input(server.port, { rekey: true }), t.deps) as { error: { code: string } };
    assert.equal(re.error.code, "SSH_HOSTKEY_MISMATCH", "rekey の初回接続は人が入れた指紋が無ければ固定しない");
  } finally { await server.close(); rmSync(dir, { recursive: true, force: true }); }
});

test("bootstrap --rekey: pins only a typed fingerprint, replaces the old line, and is BOOTSTRAP_NOT_DONE when the gate is missing", async () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-bs-"));
  const host = new FakeHost();
  host.userExists = true;
  const other = utils.parseKey(utils.generateKeyPairSync("ed25519").public);
  if (other instanceof Error) throw other;
  host.authorizedKeys = `command="/usr/local/bin/sitate gate",restrict ssh-ed25519 ${other.getPublicSSH().toString("base64")} old\nssh-ed25519 AAAAkeep keep\n`;
  const server = await startServer({ acceptAnyKey: true, onExec: host.handler });
  try {
    const t = setup(host, server, dir, { typedFingerprint: () => fingerprintOf(server.hostKeyBlob) });
    const missing = await runBootstrap(input(server.port, { rekey: true }), t.deps) as { error: { code: string } };
    assert.equal(missing.error.code, "BOOTSTRAP_NOT_DONE");
    assert.equal(t.targets.read()?.hostKeyFp, fingerprintOf(server.hostKeyBlob), "人が入れた指紋と一致したので固定した");
    host.binExists = true;
    const re = await runBootstrap(input(server.port, { rekey: true }), t.deps) as { ok: boolean; result: { rekeyed: boolean; replaced_key_lines: number; host_key_pinned: boolean } };
    assert.equal(re.ok, true);
    assert.equal(re.result.rekeyed, true);
    assert.equal(re.result.replaced_key_lines, 1);
    assert.ok(host.authorizedKeys.includes("ssh-ed25519 AAAAkeep keep"), "管理用でない行は残す");
    assert.ok(!host.authorizedKeys.includes(other.getPublicSSH().toString("base64")), "旧鍵の行を残さない");
    assert.ok(!host.log.some((c) => c.includes("apt-get")), "rekey は前提を整えない");
  } finally { await server.close(); rmSync(dir, { recursive: true, force: true }); }
});

test("bootstrap: a wrong host key against the pin is SSH_HOSTKEY_MISMATCH, a rejected credential is SSH_AUTH_DENIED and the stored credential is cleared", async () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-bs-"));
  const host = new FakeHost();
  const server = await startServer({ acceptAnyKey: false, onExec: host.handler });
  const other = await startServer({ onExec: host.handler });
  try {
    const t = setup(host, server, dir, { adminCredential: async () => other.clientPrivate });
    t.targets.write({ host: "127.0.0.1", port: server.port, user: "sitate", hostKeyFp: "SHA256:" + "A".repeat(43), adminPublicKey: null });
    const mm = await runBootstrap(input(server.port), t.deps) as { error: { code: string } };
    assert.equal(mm.error.code, "SSH_HOSTKEY_MISMATCH");
    assert.equal(t.targets.read()?.hostKeyFp, "SHA256:" + "A".repeat(43), "固定値を上書きしない");
    t.targets.remove();
    const denied = await runBootstrap(input(server.port), t.deps) as { error: { code: string } };
    assert.equal(denied.error.code, "SSH_AUTH_DENIED");
    assert.equal(t.state.cleared, 1);
  } finally { await server.close(); await other.close(); rmSync(dir, { recursive: true, force: true }); }
});

test("bootstrap: unsupported OS and missing root are reported as such", async () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-bs-"));
  const host = new FakeHost();
  const server = await startServer({ acceptAnyKey: true, onExec: (c, i, s) => (c === "cat /etc/os-release" ? (s.write("ID=freebsd\n"), s.exit(0), s.end()) : host.handler(c, i, s)) });
  try {
    const t = setup(host, server, dir);
    assert.equal(((await runBootstrap(input(server.port), t.deps)) as { error: { code: string } }).error.code, "BOOTSTRAP_PLATFORM_UNSUPPORTED");
  } finally { await server.close(); }
  const host2 = new FakeHost();
  host2.uid = "1000";
  const server2 = await startServer({ acceptAnyKey: true, onExec: (c, i, s) => (c.startsWith("sudo -n sh -c 'true'") ? (s.exit(1), s.end()) : host2.handler(c, i, s)) });
  try {
    const t = setup(host2, server2, dir);
    t.targets.remove();
    assert.equal(((await runBootstrap(input(server2.port), t.deps)) as { error: { code: string } }).error.code, "BOOTSTRAP_PRIVILEGE_REQUIRED");
  } finally { await server2.close(); rmSync(dir, { recursive: true, force: true }); }
});

test("bootstrap: a registered target without a pinned fingerprint is not overwritten by another host or port (H3), and nothing is connected or sent", async () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-bs-"));
  const host = new FakeHost();
  const server = await startServer({ acceptAnyKey: true, onExec: host.handler });
  try {
    const t = setup(host, server, dir);
    const registered = { host: "human-registered.example.test", port: 22, user: "sitate", hostKeyFp: null, adminPublicKey: null };
    t.targets.write(registered);
    const otherHost = await runBootstrap(input(server.port), t.deps) as { ok: boolean; error: { code: string } };
    assert.equal(otherHost.ok, false);
    assert.equal(otherHost.error.code, "SSH_HOSTKEY_MISMATCH");
    const otherPort = await runBootstrap(input(2222, { host: registered.host }), t.deps) as { error: { code: string } };
    assert.equal(otherPort.error.code, "SSH_HOSTKEY_MISMATCH");
    assert.deepEqual(t.targets.read(), registered, "人の登録を書き換えない");
    assert.deepEqual(host.log, [], "別のホストへは何も送らない");
  } finally { await server.close(); rmSync(dir, { recursive: true, force: true }); }
});

test("bootstrap: omitting port uses the default 22 so the target file stays readable (H4)", async () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-bs-"));
  const host = new FakeHost();
  const server = await startServer({ acceptAnyKey: true, onExec: host.handler });
  try {
    const t = setup(host, server, dir);
    // 22 番は待てないので、接続の前に止まる経路(接続先の host が違う)で、既定の 22 が照合に使われることを確かめる。
    t.targets.write({ host: "127.0.0.1", port: 22, user: "sitate", hostKeyFp: null, adminPublicKey: null });
    const { port: _omit, ...noPort } = input(server.port);
    const r = await runBootstrap(noPort as Parameters<typeof runBootstrap>[0], t.deps) as { error?: { code: string } };
    assert.notEqual(r.error?.code, "SSH_HOSTKEY_MISMATCH", "port 省略は 22 として登録の 22 と一致する");
    assert.equal(t.targets.read()?.port, 22);
    assert.ok(t.targets.read() !== null, "接続先ファイルが読める");
  } finally { await server.close(); rmSync(dir, { recursive: true, force: true }); }
});

// C2: 実行ファイルが署名付きの adapter.json の sha256 と合わなければ、何も信頼せず BOOTSTRAP_SIGNATURE_INVALID。
test("bootstrap: stage 5 enables linger and places the memlock limit for sitate (D-0163), the same on a repeat run", async () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-bs-"));
  const host = new FakeHost();
  const server = await startServer({ acceptAnyKey: true, onExec: host.handler });
  try {
    const t = setup(host, server, dir);
    await runBootstrap(input(server.port), t.deps);
    assert.ok(host.log.some((c) => c.includes("loginctl enable-linger sitate")), "sitate の linger を有効にする");
    const lim = "/etc/security/limits.d/sitate.conf";
    assert.equal(host.files.get(lim)?.toString(), "sitate - memlock unlimited\n");
    assert.ok(host.log.some((c) => c.includes(lim) && c.includes("chmod 0644")), "モードは 0644");
    // 繰り返しても同じ内容(冪等)。
    await runBootstrap(input(server.port), t.deps);
    assert.equal(host.files.get(lim)?.toString(), "sitate - memlock unlimited\n");
  } finally { await server.close(); rmSync(dir, { recursive: true, force: true }); }
});

test("bootstrap: an adapter executable that does not match the signed adapter.json sha256 is BOOTSTRAP_SIGNATURE_INVALID", async () => {
  const dir = mkdtempSync(join(tmpdir(), "u3a-bs-"));
  const host = new FakeHost();
  const server = await startServer({ acceptAnyKey: true, onExec: host.handler });
  try {
    const t = setup(host, server, dir);
    const reader = t.deps.bundle;
    const bad: BundleReader = { ...reader, read: (rel) => (rel === `${ARCH}/adapters/notify/mail/run` ? { bytes: Buffer.from("TAMPERED"), sig: "" } : reader.read(rel)) };
    const env = await runBootstrap(input(server.port), { ...t.deps, bundle: bad }) as { ok: boolean; error: { code: string } };
    assert.equal(env.ok, false);
    assert.equal(env.error.code, "BOOTSTRAP_SIGNATURE_INVALID");
  } finally { await server.close(); rmSync(dir, { recursive: true, force: true }); }
});
