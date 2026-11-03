// mp-app C8 / B1: 核の実物の形の `pending --id` の記録(state・not_before を足した形)で承認カードが開き、署名が通り、
// seal のカードに対象・除外・通知先が出る。記録の形は internal/approval/pending.go(views)と internal/approval/proof.go の読みに合わせる。
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { test } from "node:test";
import { SoftwareApprovalKey, canonicalHash, signedAtOf, verifyApproval } from "../../src/main/approval-sign.ts";
import { buildCard } from "../../src/main/approval-card.ts";
import { jcs } from "../../src/main/jcs.ts";
import { env, harness, t } from "./mp-helpers.ts";

const definition = {
  version: 1,
  source: { host: "src.example.test", targets: [{ id: "t1", files: "/srv/real-name-dir" }, { id: "t2", postgresql: "appdb", port: 5432 }], declined: [{ id: "d1", files: "/srv/not-this" }] },
  notify: { to: ["hook-main", "hook-sub"] },
  storage: [{ endpoint: "https://s3.example.test", bucket: "b", prefix: "p", key_ref: "storage" }],
  restore: { dest: { at: "dest.example.test", key_ref: "dest" } },
};
const CHANGE = "c-20261103-0001";
/** 核の buildRecord の本体(canonical_hash の入力)。def_hash_after は含めない(核の側は wp/mp-harden が直す)。 */
const body = {
  change_id: CHANGE, op: "seal", args: { definition }, target: null, precondition_hash: "sha256:" + "0".repeat(64),
  plan: { conflicts: [], reversible: true, errors: [] }, proposed_at: "2026-11-03T00:00:00Z", delay_hours: 0, expires_at: "2026-11-10T00:00:00Z",
};
const hashOf = (b: object): string => "sha256:" + createHash("sha256").update(jcs(b), "utf8").digest("hex");
/** `pending --id` が返す形: 本体 + canonical_hash + state + not_before。 */
const record = { ...body, canonical_hash: hashOf(body), state: "proposed", not_before: null };

test("[mp-app C8-1] canonical_hash は state と not_before を除いて計算する(核の views の形)", () => {
  assert.equal(canonicalHash(record), record.canonical_hash);
  const card = buildCard(record, { t });
  assert.equal(card.approveEnabled, true, String(card.disabledReason));
  assert.doesNotThrow(() => SoftwareApprovalKey.generate().signRecord(record, "2026-11-03T00:00:00Z"));
});

test("[mp-app C8-2] buildCard の戻り値に承認・取り消し・閉じるの文がある", () => {
  const c = buildCard(record, { t }) as unknown as { labels: Record<string, string> };
  assert.deepEqual(c.labels, { approve: "card.actions.approve", cancel: "card.actions.cancel", close: "card.actions.close" });
});

test("[mp-app C8-3] signedAt は秒までの UTC(YYYY-MM-DDTHH:MM:SSZ)", () => {
  assert.equal(signedAtOf(new Date("2026-11-03T01:02:03.456Z")), "2026-11-03T01:02:03Z");
});

test("[mp-app C8-4,6 / B1] seal のカードの確認語の宛先は args.definition.notify.to、区画 3 に対象・除外が実名で出る", () => {
  const c = buildCard(record, { t });
  assert.deepEqual(c.proofRecipients, ["hook-main", "hook-sub"]);
  const sec3 = JSON.stringify(c.sections.find((s) => s.no === 3));
  for (const must of ["/srv/real-name-dir", "appdb", "/srv/not-this"]) assert.ok(sec3.includes(must), must);
});

test("[mp-app C8] 承認の往復: 実物の形の記録で approve に有効な署名・秒までの signed-at・確認語が渡る", async () => {
  const proofs = { "hook-main": "ocean rapid tiger", "hook-sub": "plain fall night" };
  const h = harness((argv) => {
    if (argv[0] === "propose") return env(["propose"], { change_id: CHANGE, canonical_hash: record.canonical_hash, plan: body.plan });
    if (argv[0] === "pending") return env(["pending"], { changes: [record] });
    return env([argv[0]!], { state: "approved" });
  }, { kind: "approve", proofs });
  await h.s.onToolCall("sitate_propose", { kind: "seal", definition });
  assert.deepEqual(h.calls.map((c) => c.argv[0]), ["propose", "pending", "approve"]);
  const a = h.calls[2]!;
  assert.match(a.argv[a.argv.indexOf("--signed-at") + 1]!, /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$/);
  assert.ok(a.argv.includes("--notify-proof-stdin"));
  assert.deepEqual(JSON.parse(a.stdin), proofs);
  const sig = a.argv[a.argv.indexOf("--signature") + 1]!;
  assert.ok(verifyApproval(h.key.publicKeyDer, { changeId: CHANGE, canonicalHash: record.canonical_hash, signedAt: a.argv[a.argv.indexOf("--signed-at") + 1]!, signature: sig }));
});
