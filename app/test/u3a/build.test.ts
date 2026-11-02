// ビルドの成果物の検査(完了条件 12・13): 製品の成果物に試験用ドライバが無く、試験用にはある。署名の無い同梱物はビルドを止める。
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
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
