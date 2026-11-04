import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { Ajv2020 } from "ajv/dist/2020.js";
import { appRoot, contractSchemaDir, readJson } from "./helpers.ts";

// 送る欄の表のデータが U0 のスキーマに合う(作業指示書 5.1 の完了条件 15)。spec 3.7 の表との一致は監督の受け入れの照合。
test("send-fields data matches the U0 schema", () => {
  const ajv = new Ajv2020({ strict: false });
  ajv.addSchema(readJson(join(contractSchemaDir, "cli/codes.json")));
  const validate = ajv.compile(readJson(join(contractSchemaDir, "app/send-fields.json")));
  const data = readJson(join(appRoot, "src/data/send-fields.json"));
  assert.ok(validate(data), JSON.stringify(validate.errors));
  assert.equal(validate({ format: "sitate.send-fields/v1", rows: [{ where: "x", fields: [{ pointer: "bad" }] }] }), false);
});

// backup の対象の next の欄は封筒の next[] と同じ(argv・requires_human・requires_approval・why)。op は無い(D-0179)。
test("send-fields backup row carries target next fields without op", () => {
  const data = readJson(join(appRoot, "src/data/send-fields.json")) as { rows: { where: string; fields: { pointer: string }[] }[] };
  const ptrs = data.rows.filter((r) => r.where === "backup").flatMap((r) => r.fields.map((f) => f.pointer));
  for (const f of ["argv", "requires_human", "requires_approval", "why"]) {
    assert.ok(ptrs.includes(`/result/targets/*/next/*/${f}`), f);
  }
  assert.ok(!ptrs.some((p) => p.endsWith("/op")));
});
