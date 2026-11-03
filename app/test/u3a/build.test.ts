// ビルドの成果物の検査(完了条件 12・13): 製品の成果物に試験用ドライバが無く、試験用にはある。署名の無い同梱物はビルドを止める。
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

const script = join(import.meta.dirname, "..", "..", "scripts", "build-app.mjs");

test("build-app: 製品に試験用ドライバが入らず、署名の無い同梱物では失敗し、公開鍵は 1 種類だけ埋め込む", () => {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), "u3a-build-")));
  try {
    mkdirSync(join(dir, "bd"));
    writeFileSync(join(dir, "bd", "manual.md"), "x");
    writeFileSync(join(dir, "bd", "manual.md.sig"), "x");
    writeFileSync(join(dir, "pk.txt"), Buffer.concat([Buffer.from("Ed"), Buffer.alloc(40, 7)]).toString("base64") + "\n");
    const run = (mode: string, out: string) => spawnSync(process.execPath, [script, "--mode", mode, "--pubkey-file", join(dir, "pk.txt"), "--bundle-dir", join(dir, "bd"), "--out", join(dir, out)], { encoding: "utf8" });
    assert.equal(run("product", "p").status, 0);
    assert.equal(run("test", "t").status, 0);
    assert.ok(!readFileSync(join(dir, "p", "main.js"), "utf8").includes("startDriverServer"));
    assert.ok(readFileSync(join(dir, "t", "main.js"), "utf8").includes("startDriverServer"));
    assert.equal((JSON.parse(readFileSync(join(dir, "p", "embedded-keys.json"), "utf8")) as { kind: string }).kind, "product");
    assert.equal((JSON.parse(readFileSync(join(dir, "t", "embedded-keys.json"), "utf8")) as { kind: string }).kind, "test");
    writeFileSync(join(dir, "bd", "extra.json"), "{}");
    const bad = run("product", "x");
    assert.notEqual(bad.status, 0);
    assert.match(bad.stderr, /署名の無い同梱物/);
  } finally { rmSync(dir, { recursive: true, force: true }); }
});

// C2(spec 3.2.1): アダプタの実行ファイルは署名を持たない。署名付きの adapter.json の sha256 と合えば通り、合わなければ「署名の無い同梱物」で止まる。
test("build-app: アダプタの実行ファイルは署名なしでも、署名付きの adapter.json の sha256 と合えば通る", () => {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), "u3a-build-exe-")));
  try {
    const ad = join(dir, "bd", "linux-x86_64", "adapters", "provision", "b2");
    mkdirSync(ad, { recursive: true });
    writeFileSync(join(dir, "pk.txt"), Buffer.concat([Buffer.from("Ed"), Buffer.alloc(40, 7)]).toString("base64") + "\n");
    const decl = (body: string) => JSON.stringify({ kind: "provision", name: "b2", exe: "b2", sha256: createHash("sha256").update(body).digest("hex") });
    writeFileSync(join(ad, "adapter.json"), decl("EXE"));
    writeFileSync(join(ad, "adapter.json.sig"), "x");
    writeFileSync(join(ad, "b2"), "EXE");
    const run = () => spawnSync(process.execPath, [script, "--mode", "test", "--pubkey-file", join(dir, "pk.txt"), "--bundle-dir", join(dir, "bd"), "--out", join(dir, "o")], { encoding: "utf8" });
    assert.equal(run().status, 0, run().stderr);
    writeFileSync(join(ad, "b2"), "TAMPERED");
    const bad = run();
    assert.notEqual(bad.status, 0);
    assert.match(bad.stderr, /署名の無い同梱物/);
  } finally { rmSync(dir, { recursive: true, force: true }); }
});
