import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { appRoot, contractSchemaDir, readJson } from "./helpers.ts";
import type { KitBlock, KitLayout, KitRenderResult, RenderKit } from "../../src/kit/types.ts";

// フレーズのパネルとキットの描画部の間の型のファイルが置かれ、欄が kit-fields.json と揃っている(作業指示書 5.1 の完了条件 9)。[u3b]
test("[u3b] kit types file exists and its field names equal the kit-fields schema", () => {
  const path = join(appRoot, "src/kit/types.ts");
  assert.ok(existsSync(path));
  const src = readFileSync(path, "utf8");
  const fields = readJson(join(contractSchemaDir, "data/kit-fields.json")).$defs.fields;
  const block = src.slice(src.indexOf("export interface KitFields"), src.indexOf("/** 24 語"));
  const top = [...block.matchAll(/^  readonly (\w+):/gm)].map((m) => m[1]).sort();
  assert.deepEqual(top, Object.keys(fields.properties).sort());
  for (const sub of ["storage", "core_sha256", "provider_hints"]) {
    for (const k of Object.keys(fields.properties[sub].properties)) assert.ok(new RegExp(`\\b${k}\\b`).test(block), `${sub}.${k}`);
  }
  for (const id of fields.properties.steps.items.properties.id.enum) assert.ok(src.includes(`"${id}"`), id);
});

test("[u3b] KitRenderInput carries fields, 24 words and the language", () => {
  const src = readFileSync(join(appRoot, "src/kit/types.ts"), "utf8");
  assert.match(src, /readonly fields: KitFields;/);
  assert.match(src, /readonly phrase: PhraseWords;/);
  assert.match(src, /readonly lang: Lang;/);
  assert.match(src, /length: 24/);
});

// 配置の構造と失敗の戻り値(作業指示書 5.14b の完了条件 2)。型の形は tsc(npm run typecheck)が照らし、ここでは実行時の約束を照らす。
const sampleLayout: KitLayout = {
  paper: "A4-portrait",
  blocks: [
    { kind: "title", page: "front", order: 0, box: { x_mm: 10, y_mm: 10, w_mm: 190, h_mm: 15 } },
    { kind: "restore_step", page: "front", order: 1, step_id: "2-no-app", box: { x_mm: 10, y_mm: 30, w_mm: 190, h_mm: 20 } },
    { kind: "phrase_entry", page: "front", order: 2, box: { x_mm: 10, y_mm: 60, w_mm: 190, h_mm: 40 } },
    { kind: "provider_notes", page: "back", order: 0, box: { x_mm: 10, y_mm: 10, w_mm: 190, h_mm: 40 } },
  ],
};

/** 配置が約束を満たすか(試験の入口。U3b の試験も使える)。問題の一覧を返す。 */
function layoutProblems(l: KitLayout): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const b of l.blocks as readonly KitBlock[]) {
    const key = `${b.page}:${b.order}`;
    if (seen.has(key)) out.push(`order が重複: ${key}`);
    seen.add(key);
    if ((b.kind === "restore_step") !== (b.step_id !== undefined)) out.push(`step_id は restore_step だけが持つ: ${key}`);
    if (b.box.x_mm < 0 || b.box.y_mm < 0 || b.box.x_mm + b.box.w_mm > 210 || b.box.y_mm + b.box.h_mm > 297) out.push(`A4 縦からはみ出る: ${key}`);
  }
  return out;
}

test("[u3b] layout structure: kinds, order and position are typed, and a sample satisfies the contract", () => {
  assert.deepEqual(layoutProblems(sampleLayout), []);
  const bad: KitLayout = { paper: "A4-portrait", blocks: [{ kind: "storage", page: "front", order: 0, step_id: "1", box: { x_mm: 200, y_mm: 0, w_mm: 20, h_mm: 5 } }] };
  assert.equal(layoutProblems(bad).length, 2);
  const src = readFileSync(join(appRoot, "src/kit/types.ts"), "utf8");
  for (const kind of ["title", "product", "installer", "signing_keys", "storage", "restore_step", "phrase_entry", "host_key", "custody", "provider_notes", "production_restore_notes"]) {
    assert.ok(src.includes(`| "${kind}"`), kind);
  }
});

test("[u3b] RenderKit returns a success with html and layout, or a failure that prints nothing", () => {
  const ok: KitRenderResult = { ok: true, printable: { html: "<html></html>", layout: sampleLayout } };
  const ng: KitRenderResult = { ok: false, reason: "does_not_fit", field: "distribution_url" };
  const stub: RenderKit = (input) => (input.fields.distribution_url.length > 300 ? ng : ok);
  assert.equal(stub({ fields: { distribution_url: "x".repeat(301) } as never, phrase: [] as never, lang: "ja" }).ok, false);
  const src = readFileSync(join(appRoot, "src/kit/types.ts"), "utf8");
  assert.match(src, /export type RenderKit = \(input: KitRenderInput\) => KitRenderResult;/);
  assert.match(src, /readonly layout: KitLayout;/);
  assert.match(src, /readonly ok: false;/);
  assert.match(src, /readonly ok: true;/);
});
