// 承認カード(完了条件 5): 区画 3 の対象と除外、固定文、AI の説明の別枠、未知の op の無効化、正規化ハッシュと署名の対象が核と一致すること。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { buildCard } from "../../src/main/approval-card.ts";
import { SoftwareApprovalKey, canonicalHash, signingInput, verifyApproval } from "../../src/main/approval-sign.ts";
import { jcs } from "../../src/main/jcs.ts";
import { createHash } from "node:crypto";
import { fileURLToPath } from "node:url";

const keys = JSON.parse(readFileSync(new URL("../../src/i18n/keys.json", import.meta.url), "utf8")) as Record<string, unknown>;
/** キーを値にする描画(キーが無ければ例外)。引数は後ろに JSON で付ける。 */
const t = (key: string, args?: Record<string, unknown>): string => {
  if (!Object.hasOwn(keys, key)) throw new Error("no key " + key);
  return args === undefined ? key : key + JSON.stringify(args, (_k, v: unknown) => (v instanceof Date ? v.toISOString() : v));
};
const allText = (c: ReturnType<typeof buildCard>, skip = 0): string =>
  c.sections.filter((s) => s.no !== skip).flatMap((s) => [s.title, ...s.lines, ...s.rows.flatMap((r) => [r.label, r.value])]).join("\n");

function sealRecord(extra: Record<string, unknown> = {}): Record<string, unknown> {
  const rec = {
    change_id: "c-20261103-0001", op: "seal", args: {}, target: null, precondition_hash: "sha256:" + "0".repeat(64),
    plan: {
      reversible: true, errors: [],
      definition: { source: { host: "app01.example.internal", targets: [{ id: "t1", files: "/srv/app/data" }, { id: "t3", postgresql: "appdb" }],
        declined: [{ id: "d1", mount: "/mnt/scratch" }, { id: "d2", postgresql_cluster: 5433 }] } },
      ...extra,
    },
    proposed_at: "2026-11-03T01:00:00Z", delay_hours: 24, expires_at: "2026-11-10T01:00:00Z",
  } as Record<string, unknown>;
  rec.canonical_hash = canonicalHash(rec);
  return rec;
}

test("JCS と正規化ハッシュ: 核と共有するベクトルに一致する", () => {
  const dir = fileURLToPath(new URL("../../../internal/contract/testdata/approval/canonical.json", import.meta.url));
  const v = JSON.parse(readFileSync(dir, "utf8")) as { cases: { input: string; canonical: string; sha256: string }[] };
  for (const c of v.cases) {
    assert.equal(jcs(JSON.parse(c.input)), c.canonical);
    assert.equal(createHash("sha256").update(jcs(JSON.parse(c.input))).digest("hex"), c.sha256);
  }
});

test("canonical_hash は canonical_hash 自身を除いた記録の JCS の SHA-256", () => {
  const rec = sealRecord();
  const { canonical_hash: _h, ...rest } = rec;
  assert.equal(canonicalHash(rec), "sha256:" + createHash("sha256").update(jcs(rest)).digest("hex"));
  assert.equal(canonicalHash({ ...rec, canonical_hash: "sha256:x" }), canonicalHash(rec));
});

test("署名の対象は `sitate-approval/v1\\n<hash>\\n<signed_at>`で、署名は検証に通り、記録が変われば署名しない", () => {
  const key = SoftwareApprovalKey.generate();
  const rec = sealRecord();
  const a = key.signRecord(rec, "2026-11-03T01:05:00Z");
  assert.equal(a.canonicalHash, rec.canonical_hash);
  assert.equal(signingInput(a.canonicalHash, a.signedAt).toString(), `sitate-approval/v1\n${String(rec.canonical_hash)}\n2026-11-03T01:05:00Z`);
  assert.ok(verifyApproval(key.publicKeyDer, a));
  assert.ok(!verifyApproval(key.publicKeyDer, { ...a, signedAt: "2026-11-03T01:06:00Z" }));
  assert.equal(Buffer.from(a.signature, "base64").length, 64);
  assert.throws(() => key.signRecord({ ...rec, delay_hours: 0 }, "2026-11-03T01:05:00Z"), /HASH_MISMATCH/);
  assert.ok(!JSON.stringify(key).includes(key.exportPkcs8().toString("base64")));
});

test("seal のカード: 区画 3 に対象と除外が描かれ、調査の結果が無ければ大きさの欄に固定文", () => {
  const c = buildCard(sealRecord(), { t });
  const s3 = c.sections.find((s) => s.no === 3)!;
  const text = s3.rows.map((r) => `${r.label}|${r.value}`).join("\n");
  for (const want of ["t1 (files)|/srv/app/data / card.source.size_note", "t3 (postgresql)|appdb / card.source.size_note", "d1 (mount)|/mnt/scratch", "d2 (postgresql_cluster)|5433"]) {
    assert.ok(text.includes(want), want + "\n" + text);
  }
  assert.ok(text.includes("card.source.targets_heading") && text.includes("card.source.excluded_heading"));
  assert.ok(c.approveEnabled);
  const sized = buildCard(sealRecord(), { t, surveySizes: { t1: "12 GB" } });
  assert.ok(sized.sections.find((s) => s.no === 3)!.rows.some((r) => r.value === "/srv/app/data / 12 GB"));
  assert.ok(sized.sections.find((s) => s.no === 3)!.rows.some((r) => r.value === "appdb / card.source.size_note"));
  const withUndeclared = buildCard(sealRecord(), { t, undeclaredCandidates: 3 });
  assert.ok(allText(withUndeclared).includes('card.source.undeclared_candidates{"count":3}'));
});

test("AI の説明は区画 9 の別枠だけに入り、見出しはカタログの固定文", () => {
  const c = buildCard(sealRecord(), { t, aiExplanation: "AI-NOTE-SENTINEL" });
  const s9 = c.sections.filter((s) => s.no === 9);
  assert.equal(s9.length, 1);
  assert.equal(s9[0]!.title, "card.ai_explanation.heading");
  assert.deepEqual(s9[0]!.lines, ["AI-NOTE-SENTINEL"]);
  assert.ok(!allText(c, 9).includes("AI-NOTE-SENTINEL"));
});

test("attest の本文と note は別枠", () => {
  const rec: Record<string, unknown> = { change_id: "c-20261103-0002", op: "attest", args: { step: "R7", note: "NOTE-X" }, target: null, plan: { secret_refs: ["old-token"], reversible: true, errors: [] }, delay_hours: 0, expires_at: "2026-11-10T01:00:00Z" };
  rec.canonical_hash = canonicalHash(rec);
  const c = buildCard(rec, { t });
  assert.ok(c.sections.find((s) => s.no === 7)!.lines.some((l) => l.startsWith("card.attest.r7.body")));
  assert.ok(!allText(c, 9).includes("NOTE-X"));
  assert.ok(c.sections.find((s) => s.no === 9)!.lines.includes("NOTE-X"));
});

// 実物の propose の記録(核が書き出した固定ファイル)の形で確かめる: seal は plan.definition.notify.to、notify-add は plan.proof_required[] と machine[]。
test("確認語の入力欄は宛先ごとに 1 つ: seal は定義の notify.to、notify-add は proof_required(machine には置かない)", () => {
  const seal = sealRecord();
  (seal.plan as Record<string, unknown>).definition = { notify: { to: ["a@example.test", "b@example.test"] }, source: { host: "app01.example.internal" } };
  seal.canonical_hash = canonicalHash(seal);
  assert.deepEqual(buildCard(seal, { t }).proofRecipients, ["a@example.test", "b@example.test"]);
  const rec = sealRecord({ proof_required: ["a@example.test"], machine: ["hook"] });
  rec.op = "notify-add";
  rec.canonical_hash = canonicalHash(rec);
  const c = buildCard(rec, { t });
  assert.deepEqual(c.proofRecipients, ["a@example.test"]);
  assert.ok(allText(c).includes("card.proof.machine_heading"));
});

test("未知の op・未知の欄・ハッシュの不一致は承認を無効にし、取り消しは残る", () => {
  const rehash = (r: Record<string, unknown>): Record<string, unknown> => ({ ...r, canonical_hash: canonicalHash(r) });
  const unknownOp = rehash({ ...sealRecord(), op: "no-such-op" });
  assert.equal(buildCard(unknownOp, { t }).disabledReason, "unknown_op");
  assert.equal(buildCard(unknownOp, { t }).updatePrompt, "card.unknown.update_prompt");
  const rec = rehash({ ...sealRecord(), op: "delay-change", plan: { before: 24, after: 48, applies_to: "later_proposals", mystery: 1, reversible: true, errors: [] } });
  const withField = buildCard(rec, { t });
  assert.equal(withField.approveEnabled, false);
  assert.equal(withField.disabledReason, "unknown_field");
  const tampered = { ...sealRecord(), delay_hours: 0 };
  assert.equal(buildCard(tampered, { t }).disabledReason, "hash_mismatch");
  assert.equal(buildCard(sealRecord(), { t }).canonicalHash, sealRecord().canonical_hash);
});

// --- 欄の見出し(card.field.<op>.<欄名>)と値の書式 ---
const { CARD_FIELDS } = await import("../../src/main/approval-card-fields.ts");
const catalogs = (["ja", "en"] as const).map((l) => JSON.parse(readFileSync(new URL(`../../src/i18n/${l}/catalog.json`, import.meta.url), "utf8")) as Record<string, string>);

function planFor(op: string): Record<string, unknown> {
  const spec = CARD_FIELDS[op]!;
  const plan: Record<string, unknown> = { reversible: true, errors: [] };
  for (const f of [...spec.target, ...spec.fields]) plan[f] = f === "diff" ? [{ pointer: "/schedule/time", before: "01:00", after: "02:00" }] : f === "backup" ? { at: "2026-11-03T01:00:00Z", health: "ok", stored_at_mismatch: true } : "VALUE-" + f;
  return plan;
}
function recFor(op: string, plan: Record<string, unknown>): Record<string, unknown> {
  const rec: Record<string, unknown> = { change_id: "c-20261103-0009", op, args: {}, target: null, plan, delay_hours: 0, expires_at: "2026-11-10T01:00:00Z" };
  rec.canonical_hash = canonicalHash(rec);
  return rec;
}

test("欄の見出し: 規則ファイルの全 op の全欄に、日英のカタログの固定文がある", () => {
  const rule = readFileSync(new URL("../../../assets/rules/approval-card-format.md", import.meta.url), "utf8");
  const json = JSON.parse(rule.slice(rule.indexOf("```json") + 7, rule.indexOf("```", rule.indexOf("```json") + 7))) as { ops: Record<string, { target_fields: string[]; fields: string[] }> };
  assert.deepEqual(Object.keys(json.ops).sort(), Object.keys(CARD_FIELDS).sort());
  for (const [op, spec] of Object.entries(json.ops)) {
    assert.deepEqual([...CARD_FIELDS[op]!.target], spec.target_fields, op);
    assert.deepEqual([...CARD_FIELDS[op]!.fields], spec.fields, op);
    for (const f of [...spec.target_fields, ...spec.fields]) {
      const key = `card.field.${op}.${f}`;
      assert.ok(Object.hasOwn(keys, key), key);
      for (const cat of catalogs) assert.ok(cat[key] !== undefined && cat[key] !== "" && !cat[key]!.startsWith("[仮]") && !cat[key]!.startsWith("[tmp]") && cat[key] !== f, key);
    }
  }
});

test("欄の見出し: どの op のカードも、生の欄名を行の見出しに出さず、承認は有効のまま", () => {
  for (const op of Object.keys(CARD_FIELDS)) {
    const plan = planFor(op);
    const c = buildCard(recFor(op, plan), { t });
    assert.equal(c.disabledReason, null, op);
    const labels = c.sections.flatMap((s) => s.rows.map((r) => r.label));
    for (const f of Object.keys(plan)) assert.ok(!labels.includes(f), `${op}: 生の欄名 ${f} が見出しに出た`);
    for (const l of labels) assert.ok(/^card\.|^\/|^[a-z0-9]+ \(/.test(l), `${op}: 見出し ${l}`);
    if (op !== "attest" && op !== "seal") for (const f of [...CARD_FIELDS[op]!.target, ...CARD_FIELDS[op]!.fields]) {
      if (op === "source-change" && (f === "before" || f === "after")) continue;
      if (op === "hold-release" || f !== "diff") assert.ok(labels.includes(`card.field.${op}.${f}`), `${op}.${f} の見出し`);
    }
  }
});

test("欄の見出し: カタログに見出しの無い欄は未知の欄として承認を無効にし、行に出さない", () => {
  const noKey = (key: string, args?: Record<string, unknown>): string => {
    if (key === "card.field.delay-change.applies_to") throw new Error("no key");
    return t(key, args);
  };
  const c = buildCard(recFor("delay-change", { before: 24, after: 48, applies_to: "later", reversible: true, errors: [] }), { t: noKey });
  assert.equal(c.approveEnabled, false);
  assert.equal(c.disabledReason, "unknown_field");
  assert.ok(!allText(c).includes("applies_to"));
});

test("値の書式: 日付は現地時刻の日時、大きさは単位付き、金額は円、真偽値は固定文、null は固定文", () => {
  const plan = { reversible: true, errors: [], read_bytes: 2_500_000_000, month_read_bytes: 1500, reserved_bytes: 0, planned_bytes: 12_000, backup: "g1", host_key_fp: "SHA256:abc", to: "ssh://h", };
  const c = buildCard(recFor("restore-elsewhere", plan), { t });
  const rows = c.sections.flatMap((s) => s.rows);
  assert.ok(rows.some((r) => r.label === "card.field.restore-elsewhere.read_bytes" && r.value === "2.5 GB"));
  const d = buildCard(recFor("def-change", { reversible: true, errors: [], diff: [], def_hash_after: "sha256:x", estimate_after_jpy: 1234, cost_ceiling_yen: 3000, over_ceiling: null }), { t });
  const dr = d.sections.flatMap((s) => s.rows);
  assert.ok(dr.some((r) => r.label === "card.field.def-change.estimate_after_jpy" && r.value === 'card.value.yen{"amount":1234}'));
  assert.ok(dr.some((r) => r.label === "card.field.def-change.over_ceiling" && r.value === "card.value.none"));
  const h = buildCard(recFor("hold-release", { reversible: true, errors: [], causes: ["a"], since: "2026-11-01T00:00:00Z", days_since_last_ok: 3 }), { t });
  const s7 = h.sections.find((s) => s.no === 7)!;
  assert.ok(s7.rows.some((r) => r.label === "card.field.hold-release.since" && r.value === 'card.value.date{"at":"2026-11-01T00:00:00.000Z"}'));
  assert.equal(h.sections.find((s) => s.no === 3)!.rows.length, 0, "停止の原因は区画 7 だけ");
  const k = buildCard(recFor("restore-change", { reversible: true, errors: [], host_key_fp: "SHA256:abc", before: { a: 1 }, after: { a: 2 }, needs_secret: true }), { t });
  assert.ok(k.sections.flatMap((s) => s.rows).some((r) => r.label === "card.field.restore-change.needs_secret" && r.value === "card.value.true"));
});

test("世代(backup)は区画 2 に at・stored_at・health などを描き、食い違いに印を付ける", () => {
  const c = buildCard(recFor("storage-read-extra", { reversible: true, errors: [], backup: { at: "2026-11-03T01:00:00Z", health: "ok", targets_ok: true, restore_verified: false, stored_at_mismatch: true }, read_bytes: 1 }), { t });
  const labels = c.sections.find((s) => s.no === 2)!.rows.map((r) => r.label);
  for (const want of ["card.backup.at", "card.backup.health", "card.backup.targets_ok", "card.backup.restore_verified", "card.backup.stored_at_mismatch"]) assert.ok(labels.includes(want), want);
});
