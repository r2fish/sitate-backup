// 画面写しの口(5.14c の 7): 置き場の制限、フレーズの語の欄の守り、明暗の切り替え、製品のビルドに入らないこと。
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { checkShotPath, handleShot } from "../../src/testing/shots.ts";
import type { ShotControl } from "../../src/testing/shots.ts";

const U3C = "/work/test/records/U3c";

function ctl(over: { guard?: boolean; wait?: boolean } = {}): ShotControl & { themes: string[]; pngs: number } {
  const c = {
    themes: [] as string[], pngs: 0,
    eval: async (s: string) => (s.includes("chat-wait") ? over.wait ?? true : s.includes("phrase-panel") && s.includes("open") ? over.guard ?? true : true),
    setTheme: (t: string) => { c.themes.push(t); },
    png: async () => { c.pngs++; return new Uint8Array([137, 80, 78, 71]); },
  };
  return c;
}

test("置き場は test/records/U3c/ の直下の .png だけ", () => {
  assert.equal(checkShotPath(`${U3C}/a-light.png`), `${U3C}/a-light.png`);
  for (const bad of ["a.png", `${U3C}/../x.png`, `${U3C}/sub/a.png`, `${U3C}/a.txt`, "/work/test/records/other/a.png", "/work/test/records/U3c/.png", `${U3C}/a.png/../b.png`]) assert.throws(() => checkShotPath(bad), bad);
});

test("capture は PNG を書き、theme を先に当てる。置き場の外は書かない", async () => {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), "u3c-shot-")));
  try {
    const p = join(dir, "test", "records", "U3c", "x-dark.png");
    const c = ctl();
    assert.equal(await handleShot({ op: "capture", path: p, theme: "dark" }, c), true);
    assert.deepEqual(c.themes, ["dark"]);
    assert.deepEqual([...readFileSync(p)], [137, 80, 78, 71]);
    await assert.rejects(handleShot({ op: "capture", path: join(dir, "out.png") }, c));
    assert.equal(existsSync(join(dir, "out.png")), false);
  } finally { rmSync(dir, { recursive: true, force: true }); }
});

test("フレーズのパネルの語の欄が隠れていなければ capture は何も書かずに失敗する", async () => {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), "u3c-shot-")));
  try {
    const p = join(dir, "test", "records", "U3c", "p.png");
    const c = ctl({ guard: false });
    await assert.rejects(handleShot({ op: "capture", path: p }, c), /語の欄/);
    assert.equal(c.pngs, 0);
    assert.equal(existsSync(join(dir, "test")), false);
  } finally { rmSync(dir, { recursive: true, force: true }); }
});

test("require=wait は応答の待ちが出ていないと書かない", async () => {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), "u3c-shot-")));
  try {
    const p = join(dir, "test", "records", "U3c", "w.png");
    assert.equal(await handleShot({ op: "capture", path: p, require: "wait" }, ctl({ wait: false })), false);
    assert.equal(existsSync(p), false);
    assert.equal(await handleShot({ op: "capture", path: p, require: "wait" }, ctl({ wait: true })), true);
  } finally { rmSync(dir, { recursive: true, force: true }); }
});

test("製品のビルドの成果物に画面写しの口が入らず、試験用には入る", () => {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), "u3c-build-")));
  try {
    mkdirSync(join(dir, "bd"));
    writeFileSync(join(dir, "bd", "manual.md"), "x");
    writeFileSync(join(dir, "bd", "manual.md.sig"), "x");
    writeFileSync(join(dir, "pk.txt"), Buffer.concat([Buffer.from("Ed"), Buffer.alloc(40, 7)]).toString("base64") + "\n");
    const run = (mode: string, out: string) => spawnSync(process.execPath, [join(import.meta.dirname, "..", "..", "scripts", "build-app.mjs"), "--mode", mode, "--pubkey-file", join(dir, "pk.txt"), "--bundle-dir", join(dir, "bd"), "--out", join(dir, out)], { encoding: "utf8" });
    assert.equal(run("product", "p").status, 0);
    assert.equal(run("test", "t").status, 0);
    const product = readFileSync(join(dir, "p", "main.js"), "utf8");
    const test_ = readFileSync(join(dir, "t", "main.js"), "utf8");
    for (const w of ["capturePage", "test/records/U3c", "themeSource"]) { assert.ok(!product.includes(w), w); assert.ok(test_.includes(w), w); }
  } finally { rmSync(dir, { recursive: true, force: true }); }
});
