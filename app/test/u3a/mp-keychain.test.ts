// mp-app C4: キーチェーンは複数行の値(SSH 秘密鍵)と長い値(RSA 鍵、約 3.4 KB)を往復できる。
// security -i の実物の挙動(-w は複数行や印字できない値を 16 進で返す。1 行が約 4 KB を超えると命令が読めない)を模した偽と、
// macOS では使い捨ての keychain での実物の往復の 2 つで確かめる。
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { MacKeychain } from "../../src/main/keychain.ts";
import type { SecurityExec } from "../../src/main/keychain.ts";

const LINE_LIMIT = 3500; // 実測: 3,000 文字は通り 4,000 文字は通らない(app/test の記録 mp-app)

const MULTILINE = "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW\nQyNTUxOQAAACD\n-----END OPENSSH PRIVATE KEY-----\n";
const LONG = Array.from({ length: 50 }, (_, i) => `line${i}-` + "QWxwaGFCZXRhR2FtbWE".repeat(4)).join("\n"); // 約 3.4 KB
const tok = (s: string): string[] => [...s.matchAll(/"((?:[^"\\]|\\.)*)"|(\S+)/g)].map((m) => (m[1] ?? m[2])!.replace(/\\(.)/g, "$1"));

function fakeSecurity(): SecurityExec {
  const store = new Map<string, Buffer>();
  return async (_args, stdin) => {
    const line = stdin.replace(/\n$/, "");
    if (line.includes("\n") || line.length > LINE_LIMIT) return { stdout: "", code: 1 };
    const t = tok(line);
    const opt = (f: string): string | undefined => t[t.indexOf(f) + 1];
    const key = `${opt("-s")}\u0000${opt("-a")}`;
    if (t[0] === "add-generic-password") {
      const v = t.includes("-X") ? Buffer.from(opt("-X")!, "hex") : Buffer.from(opt("-w")!, "utf8");
      store.set(key, v);
      return { stdout: "", code: 0 };
    }
    if (t[0] === "find-generic-password") {
      const v = store.get(key);
      if (v === undefined) return { stdout: "", code: 44 };
      const s = v.toString("utf8");
      return { stdout: (/^[\x20-\x7e]*$/.test(s) ? s : v.toString("hex")) + "\n", code: 0 };
    }
    if (t[0] === "delete-generic-password") return { stdout: "", code: store.delete(key) ? 0 : 44 };
    return { stdout: "", code: 1 };
  };
}

test("[mp-app C4] 複数行の値と約 3.4 KB の値が往復し、1 行の命令が上限を超えない", async () => {
  const kc = new MacKeychain(fakeSecurity());
  await kc.set("svc", "admin", MULTILINE);
  assert.equal(await kc.get("svc", "admin"), MULTILINE);
  await kc.set("svc", "long", LONG);
  assert.equal(await kc.get("svc", "long"), LONG);
  await kc.set("svc", "long", "short");
  assert.equal(await kc.get("svc", "long"), "short", "長い値を短い値で上書きしても、残りの断片を読まない");
  assert.equal(await kc.get("svc", "none"), null);
  assert.equal(await kc.delete("svc", "admin"), true);
  assert.equal(await kc.get("svc", "admin"), null);
});

test("[mp-app C4] 長い値の削除は断片も消す。値を引数に載せない", async () => {
  const stdins: string[] = [];
  const inner = fakeSecurity();
  const kc = new MacKeychain(async (a, s) => { stdins.push(s); assert.deepEqual(a, ["-i"]); return inner(a, s); });
  await kc.set("svc", "long", LONG);
  assert.ok(stdins.length >= 2);
  assert.equal(await kc.delete("svc", "long"), true);
  assert.equal(await kc.get("svc", "long"), null);
  assert.equal(await kc.get("svc", "long#c1"), null, "断片が残っていない");
});

const realTest = process.platform === "darwin" ? test : test.skip;
realTest("[mp-app C4] macOS の security で、使い捨ての keychain に往復する(利用者の項目に触れない)", async () => {
  const dir = mkdtempSync(join(tmpdir(), "mpapp-kc-"));
  const kcPath = join(dir, "t.keychain-db");
  const run = (args: string[]): void => { const r = spawnSync("/usr/bin/security", args); assert.equal(r.status, 0, args[0]); };
  run(["create-keychain", "-p", "tmp", kcPath]);
  run(["set-keychain-settings", kcPath]);
  run(["unlock-keychain", "-p", "tmp", kcPath]);
  // 実物の security -i を呼ぶが、命令の末尾に使い捨ての keychain を足して、既定の keychain に触れない。
  const exec: SecurityExec = (args, stdin) => new Promise((resolve, reject) => {
    const p = spawn("/usr/bin/security", [...args], { stdio: ["pipe", "pipe", "ignore"], env: {} });
    let out = "";
    p.stdout.on("data", (d: Buffer) => { out += d.toString("utf8"); });
    p.on("error", reject);
    p.on("close", (code) => resolve({ stdout: out, code: code ?? 1 }));
    p.stdin.end(stdin.replace(/\n$/, "") + ` "${kcPath}"\n`);
  });
  try {
    const kc = new MacKeychain(exec);
    await kc.set("svc", "ml", MULTILINE);
    assert.equal(await kc.get("svc", "ml"), MULTILINE);
    await kc.set("svc", "long", LONG);
    assert.equal(await kc.get("svc", "long"), LONG);
    assert.equal(await kc.delete("svc", "long"), true);
    assert.equal(await kc.get("svc", "long"), null);
  } finally {
    spawnSync("/usr/bin/security", ["delete-keychain", kcPath]);
    rmSync(dir, { recursive: true, force: true });
  }
});
