// 製品のビルドに、試験用の口(ドライバ、LLM の口の差し替え、接続先の写像)が残らないことの検査(作業指示書 5.14 の完了条件 12・GB-128)。
// 試験用のビルドにはあることも確かめる(検査が空振りでないこと)。成果物は同じ入力(署名済みの同梱物の代わりの小さな置き場)で作る。
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readdirSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { transformSync } from "esbuild";

const script = join(import.meta.dirname, "..", "..", "scripts", "build-app.mjs");
const TEST_ONLY = ["startDriverServer", "driver-server", "SITATE_TEST_DRIVER_SOCKET", "SITATE_TEST_KEYCHAIN", "SITATE_TEST_LLM_BASE_URL", "SITATE_TEST_HOST_MAP", "parseHostMap", "attachDriver", "patchHost"];

test("製品の成果物(main・preload・agent・renderer)に、試験用の口が 1 つも残らない。試験用の成果物には揃っている", () => {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), "e2e-product-")));
  try {
    mkdirSync(join(dir, "bd"));
    writeFileSync(join(dir, "bd", "manual.md"), "x");
    writeFileSync(join(dir, "bd", "manual.md.sig"), "x");
    writeFileSync(join(dir, "pk.txt"), Buffer.concat([Buffer.from("Ed"), Buffer.alloc(40, 7)]).toString("base64") + "\n");
    const run = (mode: string, out: string) => spawnSync(process.execPath, [script, "--mode", mode, "--pubkey-file", join(dir, "pk.txt"), "--bundle-dir", join(dir, "bd"), "--out", join(dir, out)], { encoding: "utf8" });
    assert.equal(run("product", "p").status, 0);
    assert.equal(run("test", "t").status, 0);
    const text = (out: string): string => readdirSync(join(dir, out)).filter((f) => /\.js$/.test(f)).map((f) => readFileSync(join(dir, out, f), "utf8")).join("\n");
    // 非圧縮の成果物は `false ? … : …` の畳まれる枝の字面を残す(束ね方は build-app.mjs の持ち物)。畳んだ後の字面で、生きた口が無いことを確かめる。
    const product = transformSync(text("p"), { minifySyntax: true, loader: "js" }).code;
    const test_ = text("t");
    for (const token of TEST_ONLY) assert.ok(!product.includes(token), `製品の成果物に ${token} が残っている`);
    assert.ok(!/\bmsg\.baseUrl\b/.test(product), "製品のエージェントは init の baseUrl を読まない");
    for (const token of ["startDriverServer", "SITATE_TEST_LLM_BASE_URL", "SITATE_TEST_HOST_MAP", "parseHostMap"]) assert.ok(test_.includes(token), `試験用の成果物に ${token} が無い(検査が空振り)`);
    assert.ok(product.includes("https://api.anthropic.com"), "製品の LLM API の送り先は既定のエンドポイント");
  } finally { rmSync(dir, { recursive: true, force: true }); }
});
