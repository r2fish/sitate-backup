// mp-app B9: エージェントのプロセスの実行引数は、Electron 45 の同梱の Node 24 が知る旗だけ。--allow-net は Node 24 で `bad option`(Node 24 の実行体で実測)。
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { test } from "node:test";
import { agentExecArgv } from "../../src/main/agent-host.ts";

test("[mp-app B9] agentExecArgv は --allow-net を含まず、権限モデルの旗は --permission と --allow-fs-read だけ", () => {
  const a = agentExecArgv("/res/dist/agent.js");
  assert.deepEqual(a, ["--permission", "--allow-fs-read=/res/dist"]);
  const r = spawnSync(process.execPath, [...a, "-e", "1"], { env: {} });
  assert.equal(r.status, 0, String(r.stderr));
});
