// 元サーバーの調査の結果の区画(完了条件 33): 区画に実名が出て、モデルへ返した内容に実名が出ない。エージェントの文の path_ref は画面で実名になる。
import assert from "node:assert/strict";
import { join } from "node:path";
import { test } from "node:test";
import { SoftwareApprovalKey } from "../../src/main/approval-sign.ts";
import { MemoryKeychain, serviceFor } from "../../src/main/keychain.ts";
import { PathRefTable } from "../../src/main/path-ref.ts";
import { Session } from "../../src/main/session.ts";
import type { ChatLine } from "../../src/main/session.ts";
import { buildSurveyView, sizeText } from "../../src/main/survey-view.ts";
import { ToolRegistry } from "../../src/main/tool-registry.ts";

// 区画の新しいキーは監督が足すまで keys.json に無い。キー名をそのまま返す(足りないキーの一覧は報告に書く)。
const t = (key: string): string => key;
const registry = ToolRegistry.fromDir(join(import.meta.dirname, "..", "..", "src", "tools"));
const RESULT = {
  host_key_fp: "SHA256:" + "A".repeat(43), sizes_complete: true, databases_complete: true, prereq_missing: [], clusters: [],
  dirs: [{ path: "/srv/shopdata-7731", label: "srv", bytes: 5 * 1024 * 1024, fs_type: "ext4", supported: true, db_data: false }, { path: "/home/odd-user-91", label: "home", bytes: null, fs_type: "other", supported: false, db_data: false }],
  mounts: [{ path: "/mnt/vol-4412", fs_type: "ext4", bytes: 1024, class: "candidate" }],
  databases: [{ name: "customer_db_5520", port: 5432, major: 16, bytes: 2048, large_objects: true }],
};
const REAL = ["/srv/shopdata-7731", "/home/odd-user-91", "/mnt/vol-4412", "customer_db_5520"];

test("調査の結果: 区画に実名・種別・大きさ・v1 の可否が並び、モデルへ返す区画には実名が出ない", async () => {
  const kc = new MemoryKeychain();
  await kc.set(serviceFor("r"), "src", "dummy-credential");
  const refs = new PathRefTable();
  const chat: ChatLine[] = [];
  let survey: Record<string, unknown> | null = null;
  const s = new Session({
    registry, refs, keychain: kc, service: () => serviceFor("r"), t, approvalKey: SoftwareApprovalKey.generate(), now: () => "2026-11-03T00:00:00Z",
    runner: { async run() { return { stdout: JSON.stringify({ schema: "sitate.out/v1", command: ["source", "survey"], ok: true, dry_run: false, run_id: null, warnings: [], result: RESULT }) + "\n", end: "complete" }; } },
    sendToAgent: () => undefined, onChat: (l) => chat.push(l), onCard: async () => ({ kind: "close" }), onPanel: async () => "done" as const, onSurvey: (r) => { survey = r; },
  });
  const toModel = await s.onToolCall("sitate_source_survey", { admin_ref: "src", secret_refs: ["src"] });
  for (const real of REAL) assert.ok(!toModel.includes(real), real);
  assert.ok(/pr:[0-9a-f]{16}/.test(toModel));
  assert.ok(!chat.some((l) => REAL.some((r) => l.text.includes(r))));
  const view = buildSurveyView(survey, t)!;
  assert.deepEqual(view.rows.map((r) => r.name), REAL);
  assert.deepEqual(view.rows.map((r) => r.v1), ["status.survey.v1.yes", "status.survey.v1.no", "status.survey.v1.yes", "status.survey.v1.no"]);
  assert.equal(view.rows[0]!.size, "5 MiB");
  assert.equal(view.rows[1]!.size, "status.survey.size.unknown");
  const ref = refs.refFor("/srv/shopdata-7731");
  s.onReply(`調べたのは ${ref} です`);
  assert.equal(chat.at(-1)!.text, "調べたのは /srv/shopdata-7731 です");
});

test("大きさの単位付きの表記: 2 進接頭辞で、数え切れなければ固定文", () => {
  assert.equal(sizeText(0, "?"), "0 B");
  assert.equal(sizeText(1536, "?"), "1.5 KiB");
  assert.equal(sizeText(null, "?"), "?");
  assert.equal(sizeText(-1, "?"), "?");
});

test("調査の結果の v1 の可否: DB のデータを含むディレクトリ・候補でないマウント・大きなオブジェクトを持つ DB は対象外。実名の制御文字は落とす", () => {
  const view = buildSurveyView({
    dirs: [{ path: "/srv/a", bytes: 1, supported: true, db_data: true }, { path: "/srv/b", bytes: 1, supported: true, db_data: false }],
    mounts: [{ path: "/mnt/x", bytes: 1, class: "other" }, { path: "/mnt/y", bytes: 1, class: "candidate" }],
    databases: [{ name: "db1", bytes: 1, large_objects: true }, { name: "db2", bytes: 1, large_objects: false }, { name: "evil\u001b[31m\nname", bytes: 1 }],
  }, t)!;
  assert.deepEqual(view.rows.map((r) => r.v1), ["status.survey.v1.no", "status.survey.v1.yes", "status.survey.v1.no", "status.survey.v1.yes", "status.survey.v1.no", "status.survey.v1.yes", "status.survey.v1.yes"]);
  const evil = view.rows[6]!.name;
  assert.ok(!/[\u0000-\u001f]/.test(evil), JSON.stringify(evil));
});

test("調査が失敗した封筒では区画を更新しない", async () => {
  const kc = new MemoryKeychain();
  await kc.set(serviceFor("r"), "src", "dummy-credential");
  let called = 0;
  const bad = JSON.stringify({ schema: "sitate.out/v1", command: ["source", "survey"], ok: false, dry_run: false, run_id: null, warnings: [], result: RESULT, error: { code: "SOURCE_UNREACHABLE", class: "input", retryable: false, detail: {}, next: [] } }) + "\n";
  const s = new Session({
    registry, refs: new PathRefTable(), keychain: kc, service: () => serviceFor("r"), t, approvalKey: SoftwareApprovalKey.generate(), now: () => "2026-11-03T00:00:00Z",
    runner: { async run() { return { stdout: bad, end: "complete" }; } },
    sendToAgent: () => undefined, onChat: () => undefined, onCard: async () => ({ kind: "close" }), onPanel: async () => "done" as const, onSurvey: () => { called++; },
  });
  await s.onToolCall("sitate_source_survey", { admin_ref: "src", secret_refs: ["src"] });
  assert.equal(called, 0);
});
