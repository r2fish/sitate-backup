// mp-app B2・B3・B6・B7: 道具の呼び出しの直列、入力ボックスのラベル、保存しないモード、`*_ref` の自動の差し込み。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { canonicalHash } from "../../src/main/approval-sign.ts";
import type { CardDecision } from "../../src/main/ipc-channels.ts";
import { serviceFor } from "../../src/main/keychain.ts";
import { env, harness } from "./mp-helpers.ts";

const sealRecord = (id: string): Record<string, unknown> => {
  const body = { change_id: id, op: "seal", args: { definition: { version: 1, notify: { to: ["hook"] } } }, target: null, precondition_hash: "sha256:" + "0".repeat(64), plan: { conflicts: [], reversible: true, errors: [] }, proposed_at: "2026-11-03T00:00:00Z", delay_hours: 0, expires_at: "2026-11-10T00:00:00Z" };
  return { ...body, canonical_hash: canonicalHash(body), state: "proposed", not_before: null };
};
const coreFor = (ids: string[]) => (argv: readonly string[]): string => {
  if (argv[0] === "propose") return env(["propose"], { change_id: ids.shift() ?? "c-x" });
  if (argv[0] === "pending") return env(["pending"], { changes: [sealRecord(argv[2]!)] });
  return env([argv[0]!], { state: "approved" });
};

test("[mp-app B2] 道具の呼び出しは並列に走らず、1 つ目のカードが解けるまで 2 つ目は核を呼ばない", async () => {
  let release: ((d: CardDecision) => void) | null = null;
  const h = harness(coreFor(["c-1", "c-2"]), () => new Promise<CardDecision>((r) => { release = r; }));
  const a = h.s.onToolCall("sitate_propose", { kind: "seal", definition: { version: 1 } });
  const b = h.s.onToolCall("sitate_propose", { kind: "seal", definition: { version: 1 } });
  await new Promise((r) => setTimeout(r, 30));
  assert.deepEqual(h.calls.map((c) => c.argv[0]), ["propose", "pending"], "2 つ目はまだ核を呼ばない");
  assert.equal(h.cards.length, 1);
  release!({ kind: "close" });
  await a;
  await new Promise((r) => setTimeout(r, 30));
  assert.equal(h.cards.length, 2, "1 つ目が解けてから 2 つ目のカード");
  release!({ kind: "close" });
  await b;
});

test("[mp-app B2] 承認が無効なカード(approveEnabled が偽)への approve は署名せず、approve を核へ送らない", async () => {
  const h = harness((argv) => {
    if (argv[0] === "propose") return env(["propose"], { change_id: "c-1" });
    if (argv[0] === "pending") return env(["pending"], { changes: [{ ...sealRecord("c-1"), canonical_hash: "sha256:" + "f".repeat(64) }] });
    return env([argv[0]!], {});
  }, { kind: "approve", proofs: {} });
  await h.s.onToolCall("sitate_propose", { kind: "seal", definition: { version: 1 } });
  assert.equal(h.cards[0]!.approveEnabled, false);
  assert.ok(!h.calls.some((c) => c.argv[0] === "approve"));
});

test("[mp-app B7,B3] notify set は secret_ref だけで入力ボックスを開き、値を標準入力の 1 行目に差し込む。ラベルは通知先の文", async () => {
  const h = harness(() => env(["notify", "set"], {}), { kind: "close" }, "https://hooks.example.test/x");
  await h.s.onToolCall("sitate_notify_set", { to: "hook1", secret_ref: "hook1" });
  assert.equal(h.boxes.length, 1);
  assert.equal(h.boxes[0]!.ref, "hook1");
  assert.equal(h.boxes[0]!.mode, "save");
  assert.equal(h.boxes[0]!.label, "chat.secret.label.webhook");
  const c = h.calls[0]!;
  assert.ok(c.argv.includes("--secrets-stdin"));
  assert.deepEqual(JSON.parse(c.stdin.split("\n")[0]!), { hook1: "https://hooks.example.test/x" });
});

test("[mp-app B6,B7,B3] init-storage の admin_ref は、キーチェーンに同名の項目があっても読まず、保存しないモードで開く", async () => {
  const h = harness(() => env(["init-storage"], { plan: { errors: [] } }), { kind: "close" }, "ADMIN-KEY-TYPED");
  await h.kc.set(serviceFor("repo"), "admin", "SOURCE-SSH-KEY-SAVED");
  await h.s.onToolCall("sitate_init_storage", { endpoint: "http://x", bucket: "b", admin_ref: "admin", dry_run: true });
  assert.equal(h.boxes.length, 1);
  assert.equal(h.boxes[0]!.mode, "no-save");
  assert.equal(h.boxes[0]!.label, "chat.secret.label.storage_admin");
  const stdin = h.calls[0]!.stdin;
  assert.ok(stdin.includes("ADMIN-KEY-TYPED") && !stdin.includes("SOURCE-SSH-KEY-SAVED"));
  assert.equal(await h.kc.get(serviceFor("repo"), "admin"), "SOURCE-SSH-KEY-SAVED", "元サーバーの鍵を消さない");
});

test("[mp-app B3] 元サーバーの管理用鍵と復元先の資格情報は、それぞれ別の文で求める", async () => {
  const h = harness(() => env(["x"], {}));
  await h.s.onToolCall("sitate_source_survey", { host: "ssh://u@h", admin_ref: "source-admin" });
  await h.s.onToolCall("sitate_restore_dest_enroll", { dest: "d", adapter: "ssh-host", admin_ref: "dest-admin", dry_run: true });
  assert.deepEqual(h.boxes.map((b) => b.label), ["chat.secret.label.source_admin", "chat.secret.label.dest_admin"]);
});

test("[mp-a2 D-0169] 「始める」: カタログの固定の指示を人の発話として描いてエージェントへ送る。押すまで何も送らない", () => {
  const sent: string[] = [];
  const h = harness(() => env(["status"], {}), { kind: "close" }, "x", { sendToAgent: (_id, text) => { sent.push(text); } });
  assert.deepEqual(sent, [], "押すまで送らない");
  h.s.start();
  assert.deepEqual(sent, ["chat.start.instruction"], "文はカタログのキーから引く(ここに固定文を持たない)");
  assert.deepEqual(h.lines, [{ role: "human", text: "chat.start.instruction" }]);
  const ja = JSON.parse(readFileSync(new URL("../../src/i18n/ja/catalog.json", import.meta.url), "utf8")) as Record<string, string>;
  assert.equal(ja["chat.start.instruction"], "導入手順書に従って、バックアップを導入してください。");
  assert.equal(ja["chat.start.label"], "始める");
  assert.match(ja["chat.start.guide"]!, /^バックアップの準備をお手伝いします。始めてよければ、下のボタンを押してください。分からないことは、そのまま聞いてください$/);
});
