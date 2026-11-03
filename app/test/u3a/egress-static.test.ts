import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { test } from "node:test";
import { build } from "esbuild";
import { collectGraph, stripComments } from "./import-graph.ts";

// 完了条件 25 (a)(spec 6.5 A6 の (15)): エージェントのプロセスの中で、fetch・WebSocket・XMLHttpRequest と
// http・https・net・tls・dgram・undici・ws の経路が、guarded fetch(src/agent/guarded-fetch.ts)の外から使えないことの静的検査。
// 対象は 3 つ: 自前のソース(入口 src/agent/entry.ts から import をたどった app/src の分)、依存の木(同じ木の node_modules 全体)、束ねた成果物(esbuild の出力 = 製品と試験用)。
// A6NET_APP_ROOT: 変異の確認用に app の写しを指す(既定は app/ 自身)。
const appRoot = process.env.A6NET_APP_ROOT ?? resolve(import.meta.dirname, "..", "..");
const entry = join(appRoot, "src", "agent", "entry.ts");
const graph = collectGraph([entry]);
const rel = (f: string): string => relative(appRoot, f);
const inNodeModules = (f: string): boolean => f.includes("/node_modules/"); // node_modules が写しへの symlink でも実体の場所で判定する
const sourceFiles = [...graph.files].filter((f) => /\.(ts|mjs|js|cjs)$/.test(f));
const sdk = (p: string): string => [...graph.files].find((f) => f.endsWith("/@anthropic-ai/sdk/" + p))!;
const text = (f: string): string => stripComments(readFileSync(f, "utf8"));

/** 送信の経路になるモジュール(完了条件 25 の列挙。http2 は同じ種類として足す)。 */
const NET_MODULE = /^(?:node:)?(?:http|https|http2|net|tls|dgram|undici|ws)(?:\/.*)?$/;
/** require("x") の形の指定子(束ねた成果物の検査用)。 */
const NET_REQUIRE = /\b(?:require|import)\(?\s*["'](?:node:)?(?:http|https|http2|net|tls|dgram|undici|ws)(?:\/[^"']*)?["']/;
/** 大域の経路への直接の参照(束ねたコードにも依存の木にも 1 つも無いこと)。 */
const GLOBAL_PATH = /\b(?:WebSocket|XMLHttpRequest|EventSource)\b|\b(?:globalThis|global|self|window)\s*\.\s*fetch\b|\bglobalThis\s*\[\s*["'](?:fetch|WebSocket)["']\s*\]/;

/** 文字列リテラルを空にする(エラーの説明文の中の語は経路でない)。 */
const noStrings = (l: string): string => l.replace(/"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`/g, '""');
const hasGlobalPath = (l: string): boolean => GLOBAL_PATH.test(noStrings(l)) || /\b(?:globalThis|global)\s*\[\s*["'](?:fetch|WebSocket)["']/.test(l);

/** 依存の木で識別子 fetch を持つファイル。SDK のクライアントが受けた fetch を使う箇所と、受けていないときの大域の fetch への既定(shims.js)だけ。ここに無いファイルが fetch を持てば赤にして見直す。 */
const DEP_FETCH_FILES = new Set([
  "@anthropic-ai/sdk/client.js", "@anthropic-ai/sdk/internal/shims.js", "@anthropic-ai/sdk/internal/uploads.js",
  "@anthropic-ai/sdk/core/streaming.js", "@anthropic-ai/sdk/internal/decoders/jsonl.js",
  "@anthropic-ai/sdk/lib/credentials/credential-chain.js", "@anthropic-ai/sdk/lib/credentials/user-oauth.js", "@anthropic-ai/sdk/lib/credentials/oidc-federation.js",
  "@anthropic-ai/sdk/tools/agent-toolset/memories.js", "@anthropic-ai/sdk/tools/agent-toolset/skills.js",
]);

test("[A6-15a] 依存の木(自前 + node_modules)に http・https・net・tls・dgram・undici・ws の import が 1 つも無い", () => {
  const hits = [...graph.specifiers].filter(([s]) => NET_MODULE.test(s)).map(([s, f]) => `${s} <- ${f.map(rel).join(",")}`);
  assert.deepEqual(hits, []);
  assert.ok(graph.files.size > 100 && graph.specifiers.has("@anthropic-ai/sdk"), "依存の木をたどれている(SDK を含む)");
  assert.ok(sourceFiles.some((f) => inNodeModules(f)) && sourceFiles.some((f) => !inNodeModules(f)));
});

test("[A6-15a] 木の全体に WebSocket・XMLHttpRequest・EventSource・globalThis.fetch の参照が 1 つも無い", () => {
  const hits = sourceFiles.filter((f) => text(f).split("\n").some(hasGlobalPath)).map(rel);
  assert.deepEqual(hits, []);
});

test("[A6-15a] 自前のソースで識別子 fetch を持つのは入口の makeGuardedFetch(fetch ...) の 2 箇所(通常と試験用の口)だけ。guarded fetch の外に fetch を使う箇所が無い", () => {
  const own = sourceFiles.filter((f) => !inNodeModules(f));
  const found = new Map<string, string[]>();
  for (const f of own) {
    // 型の位置の typeof fetch と import の行は経路でない。
    const lines = text(f).split("\n").filter((l) => !/^\s*import\b/.test(l)).map((l) => l.replace(/typeof fetch/g, "").replace(/\bfetch:/g, "")); // 型の位置と、クライアントの引数の名前 fetch: は経路でない
    const hit = lines.filter((l) => /\bfetch\b/.test(l));
    if (hit.length > 0) found.set(rel(f), hit);
  }
  assert.deepEqual([...found.keys()], ["src/agent/entry.ts"]);
  const calls = found.get("src/agent/entry.ts")!.join("\n").match(/\bfetch\b/g) ?? [];
  assert.equal(calls.length, 2);
  assert.equal((found.get("src/agent/entry.ts")!.join("\n").match(/makeGuardedFetch\(fetch\b/g) ?? []).length, 2, "fetch は makeGuardedFetch の引数としてだけ現れる");
  // 口は SDK のクライアントへ明示して渡す。
  assert.equal((text(entry).match(/new Anthropic\(\{[^}]*\bfetch: withUsageReport\(makeGuardedFetch\(/g) ?? []).length, 2);
});

test("[A6-15a] 依存の木で識別子 fetch を持つファイルは SDK の既知の 10 件だけ(新しい経路が木に入れば赤)", () => {
  const files = sourceFiles.filter((f) => inNodeModules(f) && /\bfetch\b/.test(text(f))).map((f) => f.replace(/^.*\/node_modules\//, ""));
  const unknown = files.filter((f) => !DEP_FETCH_FILES.has(f));
  assert.deepEqual(unknown, []);
  // 大域の fetch への既定(受けた fetch が無いときだけ)は shims.js の 1 箇所で、client.js は受けた fetch を先に使う。
  const shims = text(sdk("internal/shims.js"));
  assert.match(shims, /typeof fetch !== 'undefined'/);
  assert.match(text(sdk("client.js")), /this\.fetch = options\.fetch \?\? Shims\.getDefaultFetch\(\)/);
});

for (const mode of ["product", "test"] as const) {
  test(`[A6-15a] 束ねた成果物(${mode})に送信の経路のモジュールの require が無く、大域の経路への参照も無い`, async () => {
    const r = await build({
      bundle: true, sourcemap: false, logLevel: "error", write: false, absWorkingDir: appRoot,
      define: { __TEST_BUILD__: String(mode === "test") },
      entryPoints: ["src/agent/entry.ts"], outfile: join(appRoot, "agent.out.js"), platform: "node", format: "cjs",
    });
    const code = r.outputFiles[0]!.text;
    assert.ok(code.length > 100_000 && /new Anthropic|Anthropic/.test(code), "SDK を含んで束ねられている");
    assert.deepEqual(code.split("\n").filter((l) => NET_REQUIRE.test(l)).map((l) => l.trim().slice(0, 80)), []);
    assert.deepEqual(code.split("\n").filter(hasGlobalPath).map((l) => l.trim().slice(0, 80)), []);
    // 束ねた中の組み込みモジュールの require は、送信の経路でないものだけ(fs・os・child_process は SDK の使われない枝で、boundary-runtime が評価されないことを確かめる)。
    const builtins = new Set([...code.matchAll(/\brequire\(["']((?:node:)?[a-z_/]+)["']\)/g)].map((m) => m[1]!.replace(/^node:/, "")));
    assert.deepEqual([...builtins].filter((m) => !["fs", "fs/promises", "os", "child_process", "path", "crypto", "stream", "util", "readline"].includes(m)), []);
  });
}

test("guarded-fetch.ts は fetch を 1 つの inner として受け、送り先の origin を検査する(口の実体)", () => {
  const g = text(join(dirname(entry), "guarded-fetch.ts"));
  assert.match(g, /new URL\(allowedBase\)\.origin/);
  assert.match(g, /redirect: "error"/);
});
