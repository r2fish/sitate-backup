// まとめレビュー H1: 実物の propose の記録(Go の試験 internal/approval/plan_cards_test.go が書き出した固定ファイル)を buildCard に渡し、
// 承認が有効になり、宛先・指紋・差分が行に出ることを確かめる。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { buildCard } from "../../src/main/approval-card.ts";

const keys = JSON.parse(readFileSync(new URL("../../src/i18n/keys.json", import.meta.url), "utf8")) as Record<string, unknown>;
const t = (key: string, args?: Record<string, unknown>): string => {
  if (!Object.hasOwn(keys, key)) throw new Error("no key " + key);
  return args === undefined ? key : key + JSON.stringify(args, (_k, v: unknown) => (v instanceof Date ? v.toISOString() : v));
};
const records = JSON.parse(readFileSync(new URL("./fixtures/propose-records.json", import.meta.url), "utf8")) as Record<string, unknown>[];
const text = (c: ReturnType<typeof buildCard>): string => c.sections.flatMap((s) => [s.title, ...s.lines, ...s.rows.flatMap((r) => [r.label, r.value])]).join("\n");
const card = (op: string): ReturnType<typeof buildCard> => {
  const rec = records.find((r) => r.op === op);
  assert.ok(rec, op);
  return buildCard(rec as never, { t });
};
const FP = "SHA256:A1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q7r8S9t0U1v";

test("実物の propose の記録で、承認が有効になる(未知の欄で無効にならない)", () => {
  for (const r of records) {
    const c = buildCard(r as never, { t });
    assert.equal(c.approveEnabled, true, `${String(r.op)}: ${String(c.disabledReason)}`);
  }
});

test("seal: ホスト鍵の固定値・承認鍵の指紋・宛先が行に出る", () => {
  const s = text(card("seal"));
  assert.ok(s.includes(FP), "ホスト鍵の固定値");
  assert.ok(s.includes("app01.example.invalid"), "元サーバーの宛先");
  assert.ok(s.includes("SHA256:"), "承認鍵の指紋");
  assert.ok(s.includes("/approval_delay_hours"), "既定より弱い項目");
});

test("source-change・restore-change: 変更後の宛先と指紋が行に出る", () => {
  const sc = text(card("source-change"));
  assert.ok(sc.includes("attacker.example.invalid") && sc.includes(FP));
  const rc = text(card("restore-change"));
  assert.ok(rc.includes("other.example.invalid") && rc.includes(FP));
});

test("delay-change・def-change: 変更前後と差分が行に出る", () => {
  assert.ok(text(card("delay-change")).includes("48"));
  assert.ok(text(card("def-change")).includes("/cost_ceiling_yen"));
});
