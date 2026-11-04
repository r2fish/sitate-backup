// 完了条件 10(復元テストの行の表示)と 23(アプリの情報の鍵の種別)を直接確かめる。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { StatusView } from "../../src/main/status-view.ts";

const catalog = JSON.parse(readFileSync(new URL("../../src/i18n/ja/catalog.json", import.meta.url), "utf8")) as Record<string, string>;
const t = (key: string): string => catalog[key]!;
const status = (restore: Record<string, unknown> | null) => ({ schema: "sitate.out/v1", command: ["status"], ok: true, result: { last_backup: { at: "2026-11-03T02:00:00Z", ok: true, checks_count: 0 }, last_restore_test: restore, hold: { active: false }, warnings: [], uncovered_count: 0 } });
const mk = () => new StatusView({ t, now: () => new Date("2026-11-03T12:00:00Z"), agentRunning: () => true, notify: () => {} });

test("status rows: the last restore test row shows its date with a pass mark, a fail mark, and a dash when never run", () => {
  const label = t("status.row.last_restore_test");
  const rowOf = (v: StatusView) => v.view().rows.find((r) => r.label === label)!;
  const ok = mk(); ok.ingest(status({ at: "2026-11-01T02:00:00Z", ok: true, days_since: 2 }));
  assert.match(rowOf(ok).value, /✓$/);
  assert.match(rowOf(ok).value, /11/);
  const ng = mk(); ng.ingest(status({ at: "2026-11-01T02:00:00Z", ok: false, days_since: 2 }));
  assert.match(rowOf(ng).value, /✗$/);
  assert.notEqual(rowOf(ng).value, rowOf(ok).value);
  const none = mk(); none.ingest(status({ at: null, days_since: null }));
  assert.equal(rowOf(none).value, "—");
});

test("status rows: the app-info block names the admin SSH key and approval key kinds (software only in iteration 1)", () => {
  const v = mk(); v.ingest(status(null));
  const info = v.view().appInfo;
  assert.equal(info[0]!.label, t("status.app_info.heading"));
  const ssh = info.find((r) => r.label === "SSH")!;
  const appr = info.find((r) => r.label === "approval")!;
  assert.match(ssh.value, /software/);
  assert.match(appr.value, /software/);
  assert.ok(!/fido|hardware|sk-/i.test(ssh.value + appr.value));
});
