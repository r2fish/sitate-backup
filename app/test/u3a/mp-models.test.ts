// mp-app C1: モデルの候補は作業指示書 5.14(完了条件 18)の 3 つを順に持つ。空だとエージェントが起動しない。
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { repoRoot } from "../contract/helpers.ts";

test("[mp-app C1] models.json は claude-sonnet-5-5, claude-sonnet-5, claude-sonnet-4-6 をこの順に持つ", () => {
  const j = JSON.parse(readFileSync(join(repoRoot, "assets/models/models.json"), "utf8")) as { format: string; models: string[] };
  assert.equal(j.format, "sitate.models/v1");
  assert.deepEqual(j.models, ["claude-sonnet-5-5", "claude-sonnet-5", "claude-sonnet-4-6"]);
});
