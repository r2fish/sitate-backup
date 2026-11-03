// 偽の LLM の定義の対象: 調査の結果から決める(変種の固定値を持たない)。
import assert from "node:assert/strict";
import { test } from "node:test";
import { candidatesFrom, sourceTargets, surveyFrom } from "./fake-definition.ts";

const dbs = (...x: [string, number, boolean][]) => x.map(([name, port, large_objects]) => ({ name, port, large_objects }));
const envelope = (result: unknown): unknown => ({ role: "user", content: [{ type: "tool_result", tool_use_id: "x", content: [{ type: "text", text: `<sitate-data command="source survey">\n${JSON.stringify({ ok: true, result })}\n</sitate-data>` }] }] });

test("base: 主のクラスタの DB をすべて対象にし、断りは無い", () => {
  const s = surveyFrom([envelope({ databases: dbs(["pr:a", 5432, false], ["pr:b", 5432, false]), clusters: [{ port: 5432 }] })]);
  const r = sourceTargets(s);
  assert.deepEqual(r.targets.map((t) => t.id), ["t1", "t2", "t3"]);
  assert.deepEqual(r.declined, []);
});
test("lo: ラージオブジェクトを持つ DB も対象にする(large_object の uncovered は核が出す)", () => {
  const r = sourceTargets(surveyFrom([envelope({ databases: dbs(["pr:a", 5432, false], ["pr:lo", 5432, true]), clusters: [{ port: 5432 }] })]));
  assert.deepEqual(r.targets.map((t) => t.postgresql), [undefined, "pr:a", "pr:lo"]);
});
test("nocluster: 主以外のクラスタを postgresql_cluster で断り、その DB は対象にしない", () => {
  const r = sourceTargets(surveyFrom([envelope({ databases: dbs(["pr:a", 5432, false], ["pr:x", 5433, false]), clusters: [{ port: 5433 }, { port: 5432 }] })]));
  assert.deepEqual(r.declined, [{ id: "d1", postgresql_cluster: 5433 }]);
  assert.equal(r.targets.some((t) => t.port === 5433), false);
});
test("調査の結果が履歴に無ければ、ファイルの対象だけ", () => {
  assert.deepEqual(sourceTargets(surveyFrom([{ role: "user", content: "x" }])), { targets: [{ id: "t1", files: "/srv/shopapp" }], declined: [] });
});

const check = (actionable: unknown[]): unknown => ({ role: "user", content: [{ type: "tool_result", tool_use_id: "y", content: `<sitate-data command="source check">\n${JSON.stringify({ ok: true, result: { actionable } })}\n</sitate-data>` }] });
test("add: source check の actionable の mount・db を定義へ足す。large_object などは足さない", () => {
  const added = candidatesFrom([check([{ kind: "mount", path: "pr:m" }, { kind: "db", path: "pr:n", port: 5432 }, { kind: "db_cluster", path: null, port: 5434 }])]);
  assert.equal(added.length, 2);
  const r = sourceTargets(undefined, added);
  assert.deepEqual(r.targets, [{ id: "t1", files: "/srv/shopapp" }, { id: "t2", files: "pr:m" }, { id: "t3", postgresql: "pr:n", port: 5432 }]);
  assert.deepEqual(candidatesFrom([check([])]), []);
});
