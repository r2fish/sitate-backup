// 完了条件 22: 説明書に無い問いへの固定文は、カタログ・説明書・規則ファイルで同じ文であること。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const read = (rel: string): string => readFileSync(new URL(rel, import.meta.url), "utf8");
const catalog = JSON.parse(read("../../src/i18n/ja/catalog.json")) as Record<string, string>;

test("manual: the fixed 'not in the documents' sentence is the same in the catalog, the manual and the rules file", () => {
  const fixed = catalog["chat.manual.unanswerable"]!.replace(/。$/, "");
  assert.equal(fixed, "この製品の資料にはありません");
  assert.ok(read("../../../assets/manual/manual.md").includes(`「${fixed}」`), "説明書の固定文がカタログと違う");
  assert.ok(read("../../../assets/rules/data-section-rules.md").includes(`「${fixed}」`), "規則ファイルの固定文がカタログと違う");
});
