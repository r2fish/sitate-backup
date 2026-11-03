// A2 の回ごとの欄の追加分(app-fields.ts)の試験。判定には使わない欄なので、集計の形だけを確かめる。
import assert from "node:assert/strict";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { appRecordSummary, distribution } from "./app-fields.ts";

test("[M-20/M-22] アプリの記録を集計し、秘密になりうる欄(本文など)は取り出さない。無ければ null", () => {
  const dir = mkdtempSync(join(tmpdir(), "e2e-appf-"));
  try {
    assert.equal(appRecordSummary(dir), null);
    writeFileSync(join(dir, "app-record.json"), JSON.stringify({
      requests: [
        { at: "t", model: "m-a", input_tokens: 10, output_tokens: 2, cache_read_input_tokens: 5, elapsed_ms: 100, body: "漏れてはならない" },
        { at: "t", model: "m-a", input_tokens: 20, output_tokens: 3, cache_read_input_tokens: 15, elapsed_ms: 300 },
      ],
      runs: [{ at: "t", elapsed_ms: 1000 }, { at: "t", elapsed_ms: 500 }],
    }));
    const s = appRecordSummary(dir)!;
    assert.equal(s.runs, 2); assert.equal(s.runs_elapsed_ms_total, 1500);
    assert.equal(s.input_tokens, 30); assert.equal(s.output_tokens, 5); assert.equal(s.cache_read_input_tokens, 20);
    assert.deepEqual(s.models, ["m-a"]);
    assert.deepEqual(s.request_elapsed_ms, { count: 2, min: 100, median: 100, p90: 300, max: 300 });
    assert.ok(!JSON.stringify(s).includes("漏れて"));
    writeFileSync(join(dir, "app-record.json"), "{");
    assert.deepEqual(appRecordSummary(dir), { error: "app-record.json を読めない" });
  } finally { rmSync(dir, { recursive: true, force: true }); }
  assert.equal(distribution([]).count, 0);
});
