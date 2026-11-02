import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { join, relative } from "node:path";
import { collectGraph, specifiersOf } from "./import-graph.ts";

// プロセスの境界の静的検査(作業指示書 5.14 の完了条件 1・2。spec 6.5 A6 (15))。
const appRoot = join(import.meta.dirname, "..", "..");
const agentDir = join(appRoot, "src", "agent");
const agentFiles = readdirSync(agentDir).filter((f) => f.endsWith(".ts")).map((f) => join(agentDir, f));

/** エージェントのプロセスが import してはならないもの: キーチェーン・SSH・ファイル・設定ファイル・子プロセス・Electron の API、agent-toolset・memory、Claude Agent SDK、LLM API 以外のネットワーク。 */
const FORBIDDEN = [
  /^(node:)?child_process$/, /^(node:)?fs(\/promises)?$/, /^(node:)?(net|tls|http|https|http2|dgram|dns|worker_threads|cluster|vm)$/, /^(node:)?os$/,
  /^electron(\/.*)?$/, /^(keytar|ssh2|node-ssh|node-pty|conf|electron-store|electron-settings)$/, /^@anthropic-ai\/claude-agent-sdk/, /^@anthropic-ai\/sdk\/tools\//,
  /agent-toolset/, /(^|\/)tools\/memory/,
];
const forbidden = (spec: string) => FORBIDDEN.some((re) => re.test(spec));

test("boundary: the agent process's own sources import no keychain, SSH, file, settings, child process, Electron or toolset module", () => {
  assert.ok(agentFiles.length >= 5, "エージェントのソースがある");
  for (const f of agentFiles) {
    for (const spec of specifiersOf(readFileSync(f, "utf8"))) {
      assert.ok(!forbidden(spec), `${relative(appRoot, f)} が ${spec} を import している`);
      if (spec.startsWith(".")) assert.match(spec, /^(\.\/|\.\.\/constants\.ts$)/, `${relative(appRoot, f)}: 主プロセスのコードを import しない: ${spec}`);
    }
  }
});

test("boundary: nothing in the agent code reads the LLM API key from the environment or arguments", () => {
  for (const f of agentFiles) {
    const src = readFileSync(f, "utf8").replace(/\/\/.*$/gm, "");
    assert.ok(!/process\.(env|argv)/.test(src), `${relative(appRoot, f)} が環境変数・引数を読む`);
    assert.ok(!/ANTHROPIC_API_KEY/.test(src), `${relative(appRoot, f)} が ANTHROPIC_API_KEY の名前を使う`);
  }
});

test("boundary: the dependency tree reaches agent-toolset / child_process / fs only inside the SDK's own internals, and Claude Agent SDK is not a dependency", () => {
  const graph = collectGraph(agentFiles);
  const sdk = join(appRoot, "node_modules", "@anthropic-ai", "sdk") + "/";
  const reached = [...graph.specifiers].filter(([spec]) => forbidden(spec) && !spec.startsWith("."));
  const outside: string[] = [];
  for (const [spec, importers] of reached) for (const f of importers) {
    // 既知: SDK 1.3.0 のルートの import は internal/node(child_process・fs ほか)と tools/agent-toolset を推移的に引く(Anthropic クラスが environments の worker を持つため)。
    // 試験用の許可は SDK の 2 つの場所だけ。自前のソースと、SDK のほかの場所と、ほかの依存が引いたら赤。実行時の防御は完了条件 25 の実行の制限。
    const knownSdk = f.startsWith(sdk + "internal/node.") || f.startsWith(sdk + "tools/agent-toolset/") || f.startsWith(sdk + "tools/memory/");
    if (!knownSdk) outside.push(`${spec} <- ${f.replace(appRoot, "")}`);
  }
  assert.deepEqual(outside, []);
  const pkg = JSON.parse(readFileSync(join(appRoot, "package.json"), "utf8"));
  const deps = Object.keys({ ...pkg.dependencies, ...pkg.devDependencies });
  assert.ok(!deps.some((d) => /claude-agent-sdk/.test(d)), "Claude Agent SDK が依存にある");
  assert.ok(![...graph.files].some((f) => /claude-agent-sdk/.test(f)));
});

test("boundary: the renderer never talks to the agent process directly (only the main process sits between)", () => {
  const rendererDir = join(appRoot, "src", "renderer");
  let files: string[] = [];
  try { files = readdirSync(rendererDir).filter((f) => f.endsWith(".ts")).map((f) => join(rendererDir, f)); } catch { /* 画面の実装はこれから */ }
  for (const f of files) {
    for (const spec of specifiersOf(readFileSync(f, "utf8"))) assert.ok(!/agent\//.test(spec) && !/utilityProcess/.test(spec), `${relative(appRoot, f)} が ${spec} を import している`);
  }
});

test("boundary: the check itself finds a forbidden import (self-check)", () => {
  assert.deepEqual(specifiersOf('import fs from "node:fs";\nconst c = require("child_process"); // import x from "electron"\nimport("electron");').sort(), ["child_process", "electron", "node:fs"]);
  for (const bad of ["node:child_process", "fs", "node:fs/promises", "electron", "keytar", "ssh2", "@anthropic-ai/sdk/tools/agent-toolset/node", "@anthropic-ai/sdk/tools/memory/node", "@anthropic-ai/claude-agent-sdk"]) assert.ok(forbidden(bad), bad);
  for (const ok of ["@anthropic-ai/sdk", "@anthropic-ai/sdk/helpers/beta/json-schema", "node:crypto"]) assert.ok(!forbidden(ok), ok);
});
