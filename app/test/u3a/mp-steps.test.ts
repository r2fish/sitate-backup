// mp-app C9: 実物の docs/install.md を読ませ、各手順の期待を満たす封筒で 1〜13 のすべてが合格する(手順書の文面の補足・裸の語・印を読めること)。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { StepJudge, parseSteps } from "../../src/main/step-judge.ts";

const md = readFileSync(join(import.meta.dirname, "..", "..", "..", "docs", "install.md"), "utf8");
const marks = { DIST_KEY_FINGERPRINT: "SHA256:testfp", WARNING_ALLOWLIST: [] };

type E = { command: string[]; dry_run?: boolean; ok: boolean; result: unknown; warnings?: unknown[] };
const good = (): E[] => [
  { command: ["version"], ok: true, result: { signing_key_fp: "SHA256:testfp" } },
  { command: ["doctor"], ok: true, result: { passed: true } },
  { command: ["init-storage"], dry_run: true, ok: true, result: { plan: { errors: [] } } },
  { command: ["restore-dest", "enroll"], dry_run: true, ok: true, result: { plan: { errors: [] } } },
  { command: ["def", "validate"], ok: true, result: {} },
  { command: ["notify", "test"], ok: true, result: { all_delivered: true } },
  { command: ["bucket", "check"], ok: true, result: { lock_mode: "COMPLIANCE", versioning: true, permissions: [{ actual: "x", expected: "x" }] } },
  { command: ["status"], ok: true, result: { initialized: true, phrase_confirmed: true, kit: { confirmed: true }, notify: { proof_confirmed: true }, last_backup: { ok: true }, last_restore_test: { ok: true }, schedule: "02:30" }, warnings: [] },
  { command: ["source", "check"], ok: true, result: { selftest: [{ ok: true }], actionable: [] } },
  { command: ["restore-dest", "check"], ok: true, result: { isolation: [{ ok: true }] } },
  { command: ["def", "show"], ok: true, result: { sealed: true } },
];
const passedBy = (envs: E[], m = marks): string[] => { const j = new StepJudge(md, m); return envs.flatMap((e) => j.judge(e as never)); };

test("[mp-app C9] 実物の手順書: 期待を全部満たす封筒で手順 1〜13 がすべて合格する", () => {
  assert.equal(parseSteps(md).length, 13);
  const got = passedBy(good());
  assert.deepEqual([...got].sort((a, b) => Number(a) - Number(b)), Array.from({ length: 13 }, (_, i) => String(i + 1)));
});

test("[mp-app C9] 期待を外す封筒では合格しない(値・欠けを見ている)", () => {
  const bad = (cmd: string[], patch: (e: E) => void, step: string): void => {
    const envs = good();
    patch(envs.find((e) => e.command.join(" ") === cmd.join(" ") && e.dry_run !== true)!);
    assert.ok(!passedBy(envs).includes(step), `手順 ${step}`);
  };
  bad(["version"], (e) => { e.result = { signing_key_fp: "SHA256:other" }; }, "1");
  bad(["bucket", "check"], (e) => { e.result = { lock_mode: "GOVERNANCE", versioning: true, permissions: [] }; }, "5");
  bad(["status"], (e) => { (e.result as { phrase_confirmed: boolean }).phrase_confirmed = false; }, "7");
  bad(["status"], (e) => { e.warnings = [{ code: "UNLISTED" }]; }, "13");
  // D-0168: 手順 13 は status.schedule が定義の schedule と同じ値のときだけ合格する(違う値・形で落ちる)
  bad(["status"], (e) => { (e.result as { schedule: unknown }).schedule = "03:00"; }, "13");
  bad(["status"], (e) => { (e.result as { schedule: unknown }).schedule = []; }, "13");
  bad(["status"], (e) => { (e.result as { schedule: unknown }).schedule = ["02:30"]; }, "13");
});

test("[D-0168] 手順 13: 定義に schedule を書いたときは、その値と同じときだけ合格する", () => {
  const withDef = (sched: string): E[] => good().map((e) => (e.command.join(" ") === "def validate" ? { ...e, result: { normalized: { schedule: "03:15" } } } : e.command[0] === "status" ? { ...e, result: { ...(e.result as object), schedule: sched } } : e));
  assert.ok(passedBy(withDef("03:15")).includes("13"));
  assert.ok(!passedBy(withDef("02:30")).includes("13"), "既定の値は、定義が別の値のとき合格しない");
});
