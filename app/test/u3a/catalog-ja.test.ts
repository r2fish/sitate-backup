// 日本語のカタログ(完了条件 15・16): 全キーが仮の文でない本物の文を持ち、擬似ロケールで 5 面の画面崩れが無い。描画器がカタログの文を描ける。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { isPlaceholder, layoutProblems } from "../../src/i18n/check.ts";
import { format, translator } from "../../src/i18n/format.ts";

const read = (p: string): Record<string, string> => JSON.parse(readFileSync(new URL(p, import.meta.url), "utf8")) as Record<string, string>;
const ja = read("../../src/i18n/ja/catalog.json");
const keys = read("../../src/i18n/keys.json");

test("日本語のカタログは骨格のキーすべてについて仮の文でない文を持つ(核のコード・why・5 面の固定文を含む)", () => {
  assert.deepEqual(Object.keys(ja).sort(), Object.keys(keys).sort());
  for (const [k, v] of Object.entries(ja)) {
    assert.ok(!isPlaceholder(v) && !v.includes("[仮]") && v.trim() !== "", k);
    if (v.replace(/\{[^}]*\}/g, "").replace(/[\s:：]/g, "") === "") continue; // 置換変数だけの書式(呼び出しの 1 行)
    assert.ok(/[぀-ヿ一-鿿]/.test(v), `${k} が日本語の文でない`);
  }
  assert.ok(Object.keys(ja).filter((k) => k.startsWith("core.why.")).length >= 80);
});

test("擬似ロケールで画面崩れが無い(置換変数が保たれ、欄に収まる)", () => {
  const problems = Object.entries(ja).flatMap(([k, v]) => layoutProblems(k, v).map((p) => `${k}: ${p}`));
  assert.deepEqual(problems, []);
});

test("描画器: 置換変数・数・日付を描き、欠けた引数とキーは例外(未置換の波括弧を出さない)", () => {
  const t = translator(ja, "ja");
  assert.equal(t("card.cost.delta", { delta_jpy: 12345 }), "月額の増減は 12,345 円です。");
  assert.match(t("card.timing.expires", { expires_at: new Date("2026-11-10T01:00:00Z") }), /承認の期限は .+ です。/);
  assert.throws(() => t("card.cost.delta"));
  assert.throws(() => t("no.such.key"));
  assert.throws(() => format("{n, number}", { n: "x" }));
});
