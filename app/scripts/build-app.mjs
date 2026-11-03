// アプリのビルド(`make build-app` / `make build-app-test`。作業指示書 3.1・3.5、5.14 の完了条件 12・13)。
//   node scripts/build-app.mjs --mode product|test [--pubkey-file <公開鍵の行のファイル>] [--bundle-dir <署名済みの同梱物の置き場>] [--out dist]
// 引数を省くと標準の置き場を使う(`make build-app` / `make build-app-test`): 同梱物は `make build[-test]` と `make sign[-test]` の成果物
// `../dist/<mode>/bundle/<版>/`(核・アダプタ・価格表)と `../dist/<mode>/app-assets/<版>/`(手順書・道具の定義・規則・カタログ・説明書)を 1 つの置き場
// `../dist/<mode>/app-bundle/<版>/` に合わせたもの。公開鍵は test が `../test/env/.run/keys/sitate-test.pub`、product は環境変数 PUBLIC_KEY_FILE。
// - 公開鍵は minisign の 1 行(最大 2 行)。product は配布用、test は使い捨ての鍵。両方を信頼するビルドは作らない(1 つのファイルだけ受ける)。
// - 同梱物はすべて `<名前>.sig` を伴うこと(署名は `make sign` / `make sign-test` が先に付ける)。無ければ失敗する。署名の鍵は扱わない。
// - 試験用ドライバは test のときだけ束ねる。product の成果物にドライバのモジュールが無いことを検査して、あれば失敗する。
import { build } from "esbuild";
import { createHash } from "node:crypto";
import { cpSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { parseArgs } from "node:util";

const { values } = parseArgs({ options: { mode: { type: "string" }, "pubkey-file": { type: "string" }, "bundle-dir": { type: "string" }, version: { type: "string" }, out: { type: "string", default: "dist" } } });
const fail = (msg) => { console.error("build-app: " + msg); process.exit(1); };
if (values.mode !== "product" && values.mode !== "test") fail("--mode は product か test");
const isTest = values.mode === "test";
const root = resolve(import.meta.dirname, "..");
const repo = resolve(root, "..");
const out = resolve(values.out);
const version = values.version ?? JSON.parse(readFileSync(join(root, "package.json"), "utf8")).version;
const pubkeyFile = values["pubkey-file"] ?? (isTest ? join(repo, "test/env/.run/keys/sitate-test.pub") : process.env.PUBLIC_KEY_FILE);
if (!pubkeyFile || !existsSync(pubkeyFile)) fail("公開鍵のファイルが無い(--pubkey-file" + (isTest ? "。test は先に make sign-test" : "。product は PUBLIC_KEY_FILE") + ")");
const keys = readFileSync(pubkeyFile, "utf8").split("\n").map((l) => l.trim()).filter((l) => l !== "" && !l.startsWith("untrusted comment"));
if (keys.length < 1 || keys.length > 2 || keys.some((k) => Buffer.from(k, "base64").length !== 42)) fail("公開鍵は minisign の 1 行を 1〜2 個");

const walk = (dir) => readdirSync(dir).flatMap((f) => (statSync(join(dir, f)).isDirectory() ? walk(join(dir, f)) : [join(dir, f)]));
const bundleDir = values["bundle-dir"] ? resolve(values["bundle-dir"]) : assemble();
function assemble() {
  const base = join(repo, "dist", values.mode);
  const parts = [join(base, "bundle", version), join(base, "app-assets", version)];
  for (const p of parts) if (!existsSync(p)) fail(`${p} が無い。先に make build${isTest ? "-test" : ""} と make sign${isTest ? "-test" : ""} を実行`);
  const merged = join(base, "app-bundle", version);
  rmSync(merged, { recursive: true, force: true });
  mkdirSync(merged, { recursive: true });
  for (const p of parts) cpSync(p, merged, { recursive: true });
  return merged;
}
const files = walk(bundleDir).filter((f) => !f.endsWith(".sig")).map((f) => relative(bundleDir, f));
// アダプタの実行ファイルは署名せず、署名付きの adapter.json の sha256 で照らす(spec 3.2.1、tools/sign.sh)。それ以外は署名を要る。
const declaredExe = (f) => {
  const dir = dirname(f);
  if (!/^.+\/adapters\/[^/]+\/[^/]+$/.test(dir)) return false;
  const decl = join(bundleDir, dir, "adapter.json");
  if (!existsSync(decl) || !existsSync(decl + ".sig")) return false;
  try {
    const d = JSON.parse(readFileSync(decl, "utf8"));
    return typeof d.exe === "string" && join(dir, d.exe) === f && d.sha256 === createHash("sha256").update(readFileSync(join(bundleDir, f))).digest("hex");
  } catch { return false; }
};
const unsigned = files.filter((f) => !existsSync(join(bundleDir, f + ".sig")) && !declaredExe(f));
if (unsigned.length > 0) fail("署名の無い同梱物: " + unsigned.join(", "));

rmSync(out, { recursive: true, force: true });
mkdirSync(join(out, "bundle"), { recursive: true });
const common = { bundle: true, sourcemap: false, logLevel: "error", define: { __TEST_BUILD__: String(isTest) }, absWorkingDir: root };
await build({ ...common, entryPoints: ["src/main/index.ts"], outfile: join(out, "main.js"), platform: "node", format: "cjs", external: ["electron", "cpu-features", "*.node"] });
await build({ ...common, entryPoints: ["src/main/preload.ts"], outfile: join(out, "preload.js"), platform: "node", format: "cjs", external: ["electron", "cpu-features", "*.node"] });
await build({ ...common, entryPoints: ["src/agent/entry.ts"], outfile: join(out, "agent.js"), platform: "node", format: "cjs" });
await build({ ...common, entryPoints: ["src/renderer/renderer.ts"], outfile: join(out, "renderer.js"), platform: "browser", format: "iife" });
cpSync(join(root, "src/renderer/index.html"), join(out, "index.html"));
cpSync(join(root, "src/renderer/style.css"), join(out, "style.css"));
cpSync(bundleDir, join(out, "bundle"), { recursive: true });
writeFileSync(join(out, "bundle", "manifest.json"), JSON.stringify({ version, files: files.sort() }, null, 2));
// app/package.json は "type": "module"。束ねた成果物(main・preload・agent)は CommonJS なので、dist の中だけ commonjs と宣言する。
writeFileSync(join(out, "package.json"), JSON.stringify({ type: "commonjs" }));
writeFileSync(join(out, "embedded-keys.json"), JSON.stringify({ kind: values.mode, keys }));

if (!isTest) {
  const leaked = walk(out).filter((f) => /\.(js|json)$/.test(f)).filter((f) => /startDriverServer|driver-server|SITATE_TEST_DRIVER_SOCKET|SITATE_TEST_KEYCHAIN/.test(readFileSync(f, "utf8")));
  if (leaked.length > 0) fail("製品の成果物に試験用ドライバが入っている: " + leaked.join(", "));
}
console.log(`build-app: ${values.mode} -> ${out}`);
