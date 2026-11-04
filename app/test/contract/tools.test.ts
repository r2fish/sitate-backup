import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { appRoot, contractSchemaDir, listFiles, readJson } from "./helpers.ts";
import { RESTORE_STEPS, argNames, bootstrapProblems, bootstrapRestoreProblems, commonProblems, definitionProblems, dryRunProblems, pathRefProblems, proposeProblems, secretRefsProblems, shapeProblems, stepProblems, typeProblems, type GateFlag } from "./tool-checks.ts";
import { checkKindArgs } from "../../src/main/tool-kind-args.ts";

// U3 の契約試験 1〜4(作業指示書 5.1。spec 3.7 の「道具の定義の写像」)。道具の定義は src/tools/<道具名>.json(U3a が書く)。
// 骨格では定義が無いので、定義を読む試験は NOT_IMPLEMENTED の印で失敗する。定義を読まない試験(名前の規則と一意性ほか)は骨格でも合格する。
type Sub = { command: string[]; public: string; later?: string; dry_run?: boolean };
const subs: Sub[] = readJson(join(contractSchemaDir, "cli/subcommands.json")).subcommands;
const gate = readJson(join(contractSchemaDir, "..", "gatedata", "allowlist.json"));
const toolFiles = listFiles(join(appRoot, "src/tools"), ".json");
const toolName = (c: string[]) => "sitate_" + c.join("_").replace(/-/g, "_");
const provided = subs.filter((s) => s.public === "tool" && !s.later);
/** 仕様 8.2 でイテレーション 3 へ送った --dry-run(D-0183): subcommands.json の dry_run の列が true でも、道具の定義には dry_run を出さない 9 サブコマンド。 */
const DRY_RUN_DEFERRED = ["backup", "hold set", "notify set", "extend", "prune", "init", "source enroll", "restore", "restore-test"];
const offersDryRun = (s: Sub) => s.dry_run === true && !DRY_RUN_DEFERRED.includes(s.command.join(" "));
const NOT_OFFERED = ["secrets-stdin", "phrase-stdin", "snapshot-key-stdin", "notify-proof-stdin", "secret-fd", "file", "invoked-by", "approval-pubkey"]; // approval-pubkey は init だけ(主プロセスが付ける。spec 3.7「道具に出さない選択肢」、D-0165)
const snake = (s: string) => s.replace(/-/g, "_");

function needTools() {
  assert.ok(toolFiles.length > 0, "NOT_IMPLEMENTED: src/tools に道具の定義が無い(U3a)");
}
const load = (name: string) => readJson(join(appRoot, "src/tools", name + ".json"));
const input = (name: string) => load(name).input_schema;
/** bootstrap-restore の place 以外の段の引数: 4.7 の復元の手順のサブコマンドの選択肢(道具に出さない選択肢を除く)と key_ref。 */
const restoreStepArgs = (): string[] => {
  const cmds = ["storage issue-read-key", "storage revoke-read-key", "journal verify", "list", "restore", "secrets set"];
  const names = gate.subcommands.filter((x: any) => cmds.includes(x.command.join(" "))).flatMap((x: any) => x.flags.map((f: any) => f.name)).filter((n: string) => !NOT_OFFERED.includes(n) && n !== "dry-run");
  return [...new Set<string>([...names.map(snake), "key_ref"])];
};
/**
 * secret_refs が要るサブコマンド: キーチェーンの値を `--secrets-stdin` で渡す参照名(--admin-ref・--source-admin-ref・--host-ref・--secret-ref)を取るもの、と propose(種別の提案の JSON が参照名を持つ)。
 * --key-ref は保管先の鍵の参照名で、値はホストの秘密ファイルから読む(spec 3.7: 書き込み鍵・読み取り鍵は秘密ファイルから)ので secret_refs を要さない(付けてもよい)。
 */
const takesSecretRefs = (s: Sub): boolean => s.command[0] === "propose" || gateOf(s).flags.some((x: any) => x.value === "ref" && x.name !== "key-ref");
const gateOf = (s: Sub) => gate.subcommands.find((x: any) => x.command.join(" ") === s.command.join(" "));
const change = readJson(join(contractSchemaDir, "data/change.json")).$defs;
const SIGNED_PATTERN: string = change.attest_step_signed.anyOf[1].pattern;
const UNSIGNED: string[] = change.attest_step_unsigned.enum;
test("tools: names follow the rule and no two subcommands share a name", () => {
  const names = subs.map((s) => toolName(s.command));
  assert.equal(new Set(names).size, names.length, "道具名の衝突");
  for (const n of names) assert.match(n, /^sitate_[a-z0-9_]{1,57}$/);
  assert.equal(toolName(["def", "validate"]), "sitate_def_validate");
  assert.equal(toolName(["restore-dest", "enroll"]), "sitate_restore_dest_enroll");
  assert.equal(toolName(["bootstrap-restore"]), "sitate_bootstrap_restore");
});

test("tools: every subcommand has exactly one public class mark; bootstrap and bootstrap-restore are tools", () => {
  for (const s of subs) assert.ok(["tool", "ui-only", "internal", "test-only"].includes(s.public), s.command.join(" "));
  for (const c of ["bootstrap", "bootstrap-restore"]) assert.equal(subs.find((s) => s.command.join(" ") === c)?.public, "tool");
  for (const c of ["approve", "phrase verify", "kit confirm", "restore fetch", "secrets set"]) {
    assert.equal(subs.find((s) => s.command.join(" ") === c)?.public, "ui-only", c);
  }
});

test("tools: the later-iteration subcommands are listed as not provided", () => {
  const later = subs.filter((s) => s.later).map((s) => s.command.join(" "));
  for (const c of ["restore fetch", "journal verify", "upgrade", "restore-dest teardown", "restore-dest release", "storage issue-read-key", "storage revoke-read-key"]) {
    assert.ok(later.includes(c), `${c} が「未提供」の印を持たない`);
  }
});

test("tools: public class tool rows and tool definitions are one-to-one, later ones have none", () => {
  needTools();
  const have = toolFiles.map((f) => f.replace(/\.json$/, "")).sort();
  assert.deepEqual(have, provided.map((s) => toolName(s.command)).sort());
  for (const s of subs.filter((x) => x.later)) assert.ok(!toolFiles.includes(toolName(s.command) + ".json"), s.command.join(" "));
  for (const f of have) assert.deepEqual(shapeProblems(f, load(f)), []);
});

test("tools: argument sets equal the CLI options minus the withheld ones plus kind/definition/secret_refs/run_id", () => {
  needTools();
  for (const s of provided) {
    const name = toolName(s.command);
    const got = argNames(input(name));
    assert.ok(got.has("run_id"), `${name}: run_id`);
    assert.deepEqual(commonProblems(name, input(name)), []);
    assert.deepEqual(definitionProblems(name, input(name)), []);
    if (s.command.join(" ") === "bootstrap") { assert.deepEqual(bootstrapProblems(input(name)), []); continue; } // 引数は spec 3.7 の bootstrap の入力の表
    if (s.command.join(" ") === "bootstrap-restore") { assert.deepEqual(bootstrapRestoreProblems(input(name), restoreStepArgs()), []); continue; } // 同 bootstrap-restore の入力の表
    const g = gateOf(s);
    const want = new Set<string>(["run_id"]);
    for (const f of g.flags.map((x: any) => x.name)) if (!NOT_OFFERED.includes(f)) want.add(snake(f));
    if (g.positional?.values) want.add("kind");
    if (g.flags.some((x: any) => x.name === "file")) want.add("definition");
    got.delete("secret_refs"); // secret_refs は --secrets-stdin を受けるどのサブコマンドにも付けてよい
    got.delete("dry_run"); // dry_run は subcommands.json の dry_run の列から別に照らす(dryRunProblems)
    assert.deepEqual(dryRunProblems(name, input(name), offersDryRun(s)), []);
    assert.deepEqual([...got].sort(), [...want].sort(), name);
    for (const n of NOT_OFFERED) assert.ok(!got.has(snake(n)), `${name}: 道具に出さない選択肢 ${n}`);
  }
});

test("tools: argument types follow the gate value kinds (flag -> boolean, json, integer, enum, string)", () => {
  needTools();
  for (const s of provided) {
    if (["bootstrap", "bootstrap-restore"].includes(s.command.join(" "))) continue;
    const flags: GateFlag[] = gateOf(s).flags.filter((x: any) => !NOT_OFFERED.includes(x.name));
    assert.deepEqual(typeProblems(toolName(s.command), input(toolName(s.command)), flags, offersDryRun(s) ? ["dry-run"] : []), []);
  }
});

test("tools: secret_refs is a unique array of strings, required where a keychain reference name is taken; paths take path_ref, real paths and no non-string", () => {
  needTools();
  for (const s of provided) {
    if (["bootstrap", "bootstrap-restore"].includes(s.command.join(" "))) continue;
    const name = toolName(s.command);
    assert.deepEqual(secretRefsProblems(name, input(name), takesSecretRefs(s)), []);
    assert.deepEqual(pathRefProblems(name, input(name)), []);
  }
});

/** propose の和集合の引数(許可リストの選択肢から道具に出さないものを除き、file は definition)。 */
const proposeUnion = (g: any): string[] => g.flags.filter((x: any) => !NOT_OFFERED.includes(x.name) || x.name === "file").map((x: any) => (x.name === "file" ? "definition" : snake(x.name))).concat(["run_id"]);

test("tools: propose is one flat object (no top-level oneOf), kind enum = the allow-list kinds, properties = union of all kinds' arguments", () => {
  needTools();
  const g = gate.subcommands.find((x: any) => x.command[0] === "propose");
  assert.deepEqual(proposeProblems(input("sitate_propose"), g.positional.values, proposeUnion(g)), []);
});

// kind ごとの引数の組(spec 3.7 の種別ごとの oneOf の意味。spec 3.1.2.9 の種別の表のうち、必須の引数が一意に決まる種別)。
// 共通の引数 run_id・dry_run・secret_refs はどの種別でも受ける。ベクトルは試験の中のデータで、検査の実装は src/main/tool-kind-args.ts(U3a)。
const COMMON = { run_id: "r-20261018T173000Z-3f9a", dry_run: true, secret_refs: ["ref-a"] };
const KIND_VECTORS: { name: string; kind: string; args: Record<string, unknown>; ok: boolean }[] = [
  { name: "restore-change with restore", kind: "restore-change", args: { restore: { adapter: "ssh-host" } }, ok: true },
  { name: "restore-change with common args", kind: "restore-change", args: { restore: {}, ...COMMON }, ok: true },
  { name: "restore-change without restore", kind: "restore-change", args: {}, ok: false },
  { name: "restore-change with another kind's source", kind: "restore-change", args: { restore: {}, source: {} }, ok: false },
  { name: "source-change with source", kind: "source-change", args: { source: {} }, ok: true },
  { name: "source-change with restore instead", kind: "source-change", args: { restore: {} }, ok: false },
  { name: "notify-add with notify", kind: "notify-add", args: { notify: {} }, ok: true },
  { name: "notify-change without notify", kind: "notify-change", args: { hours: 24 }, ok: false },
  { name: "seal with definition", kind: "seal", args: { definition: { version: 1 } }, ok: true },
  { name: "def-change without definition", kind: "def-change", args: {}, ok: false },
  { name: "checks-weaken with restore", kind: "checks-weaken", args: { definition: {}, restore: {} }, ok: false },
  { name: "delay-change with hours", kind: "delay-change", args: { hours: 48 }, ok: true },
  { name: "delay-change with to", kind: "delay-change", args: { to: "weekly" }, ok: false },
  { name: "approval-key-change with pubkey and key_type", kind: "approval-key-change", args: { pubkey: "AAAA", key_type: "software" }, ok: true },
  { name: "approval-key-change without pubkey", kind: "approval-key-change", args: { key_type: "software" }, ok: false },
  { name: "restore-test-reduce with to", kind: "restore-test-reduce", args: { to: "monthly" }, ok: true },
  { name: "adapter-add with dir", kind: "adapter-add", args: { dir: "x" }, ok: true },
  { name: "attest with step and note", kind: "attest", args: { step: "R3", note: "説明" }, ok: true },
  { name: "attest without step", kind: "attest", args: { note: "説明" }, ok: false },
  { name: "attest on a step that needs no signature (dest_disposed)", kind: "attest", args: { step: "dest_disposed" }, ok: false },
  { name: "attest on a step that needs no signature (admin_access_closed)", kind: "attest", args: { step: "admin_access_closed", note: "説明" }, ok: false },
  { name: "key-rotate with no arguments", kind: "key-rotate", args: {}, ok: true },
  { name: "key-rotate with common args", kind: "key-rotate", args: { ...COMMON }, ok: true },
  { name: "key-rotate with hours", kind: "key-rotate", args: { hours: 24 }, ok: false },
  { name: "key-rotate-waive with no arguments", kind: "key-rotate-waive", args: {}, ok: true },
  { name: "key-rotate-waive with hours", kind: "key-rotate-waive", args: { hours: 24 }, ok: false },
  { name: "restore-test-increase with to", kind: "restore-test-increase", args: { to: "weekly" }, ok: true },
  { name: "restore-test-increase without to", kind: "restore-test-increase", args: {}, ok: false },
  { name: "def-change-unlisted with definition", kind: "def-change-unlisted", args: { definition: {} }, ok: true },
  { name: "def-change-unlisted without definition", kind: "def-change-unlisted", args: {}, ok: false },
  { name: "read-key-issue with a definition", kind: "read-key-issue", args: { definition: {} }, ok: false },
  { name: "hold-release with definition", kind: "hold-release", args: { definition: {} }, ok: false },
  { name: "phrase-reissue with no arguments", kind: "phrase-reissue", args: {}, ok: true },
  { name: "kind outside the allow-list", kind: "no-such-kind", args: {}, ok: false },
  { name: "empty kind", kind: "", args: {}, ok: false },
  { name: "an argument that no kind takes", kind: "delay-change", args: { hours: 24, zzz: 1 }, ok: false },
];

test("tools: the vectors name only kinds of the allow-list (or an out-of-list kind on purpose)", () => {
  const kinds: string[] = gate.subcommands.find((x: any) => x.command[0] === "propose").positional.values;
  for (const v of KIND_VECTORS) assert.ok(kinds.includes(v.kind) || !v.ok, `${v.name}: 許可リストに無い kind が ok の期待`);
  assert.ok(KIND_VECTORS.some((v) => v.ok) && KIND_VECTORS.some((v) => !v.ok));
});

test("tools: the main process rejects arguments that do not fit the kind before running (propose)", () => {
  for (const v of KIND_VECTORS) {
    const got = checkKindArgs(v.kind, v.args);
    assert.equal(got.ok, v.ok, v.name);
    if (!got.ok) assert.equal(got.code, "TOOL_ARG_INVALID", v.name);
  }
});

test("tools: sitate_attest step accepts the signed steps and rejects the three unsigned ones (Ajv, any way of writing the range)", () => {
  needTools();
  assert.deepEqual(stepProblems(input("sitate_attest"), SIGNED_PATTERN, UNSIGNED), []);
});

// 検査そのものが誤った定義を落とし、正しい定義(書き方が違っても)を通すこと。定義が無い骨格でも走る。
const GOOD_STEP_ENUM = { type: "object", properties: { step: { anyOf: [{ enum: ["R3", "R7"] }, { type: "string", pattern: "^[A-Z][0-9]{1,2}$" }] } } };
const GOOD_STEP_PATTERN_ONLY = { type: "object", properties: { step: { type: "string", pattern: "^[A-Z][0-9]{1,2}$" } } };
const GOOD_STEP_ONEOF = { type: "object", properties: { step: { oneOf: [{ const: "R3" }, { const: "R7" }, { type: "string", pattern: "^V[0-9]{1,2}$" }] } } };
const BAD_STEP_LOOSE = { type: "object", properties: { step: { anyOf: [{ enum: ["R3", "R7"] }, { type: "string", pattern: "^.+$" }] } } };

test("tools: the step check rejects a loose range and accepts every correct way of writing it", () => {
  for (const ok of [GOOD_STEP_ENUM, GOOD_STEP_PATTERN_ONLY, GOOD_STEP_ONEOF]) assert.deepEqual(stepProblems(ok, SIGNED_PATTERN, UNSIGNED), []);
  assert.ok(stepProblems(BAD_STEP_LOOSE, SIGNED_PATTERN, UNSIGNED).length >= 3);
});

test("tools: the propose shape check accepts only the flat form", () => {
  const g = gate.subcommands.find((x: any) => x.command[0] === "propose");
  const kinds: string[] = g.positional.values;
  const union = proposeUnion(g);
  const good = () => ({ type: "object", additionalProperties: false, required: ["kind"], properties: { kind: { enum: [...kinds] }, ...Object.fromEntries(union.map((a) => [a, {}])), dry_run: { type: "boolean" } } });
  assert.deepEqual(proposeProblems(good(), kinds, union), []);
  const bad: Record<string, any> = {
    oneOf: { ...good(), oneOf: [{ properties: { kind: { const: "seal" } } }] },
    anyOf: { ...good(), anyOf: [{}] },
    requiredExtra: { ...good(), required: ["kind", "restore"] },
    openAdditional: { ...good(), additionalProperties: true },
    missingArg: (() => { const d = good(); delete (d.properties as any).restore; return d; })(),
    noDryRun: (() => { const d = good(); delete (d.properties as any).dry_run; return d; })(),
    missingKind: (() => { const d = good(); d.properties.kind.enum.pop(); return d; })(),
  };
  for (const [name, def] of Object.entries(bad)) assert.ok(proposeProblems(def, kinds, union).length > 0, name);
});

test("tools: dry_run follows the dry_run column of subcommands.json (every subcommand but bootstrap and bootstrap-restore has the column)", () => {
  for (const s of subs) {
    if (["bootstrap", "bootstrap-restore"].includes(s.command.join(" "))) assert.equal(s.dry_run, undefined, s.command.join(" "));
    else assert.equal(typeof s.dry_run, "boolean", s.command.join(" "));
  }
  assert.ok(subs.some((s) => s.dry_run === true) && subs.some((s) => s.dry_run === false));
});

test("tools: the 9 subcommands deferred to iteration 3 (spec 8.2, D-0183) have no dry_run although their column is true; the other true tools have it", () => {
  needTools();
  for (const w of DRY_RUN_DEFERRED) {
    const s = subs.find((x) => x.command.join(" ") === w);
    assert.ok(s && s.dry_run === true, `${w}: 列が true の例外`);
    assert.equal(argNames(input(toolName(s.command))).has("dry_run"), false, `${w}: 道具に dry_run がある`);
  }
  const kept = provided.filter((s) => s.dry_run === true && !DRY_RUN_DEFERRED.includes(s.command.join(" ")));
  for (const n of ["init-storage", "restore-dest enroll", "propose"]) assert.ok(kept.some((s) => s.command.join(" ") === n), n);
  for (const s of kept) assert.equal(argNames(input(toolName(s.command))).has("dry_run"), true, `${s.command.join(" ")}: dry_run が無い`);
});

test("tools: the dry_run check rejects a missing definition where the column is true and a present one where it is false", () => {
  const withDry = { type: "object", properties: { dry_run: { type: "boolean" }, run_id: { type: "string" } } };
  const withoutDry = { type: "object", properties: { run_id: { type: "string" } } };
  assert.deepEqual(dryRunProblems("t", withDry, true), []);
  assert.deepEqual(dryRunProblems("t", withoutDry, false), []);
  assert.equal(dryRunProblems("t", withoutDry, true).length, 1); // true の道具で欠いた定義
  assert.equal(dryRunProblems("t", withDry, false).length, 1); // false の道具で持たせた定義
  assert.ok(dryRunProblems("t", { type: "object", properties: { dry_run: { type: "string" } } }, true).length >= 1);
  assert.ok(dryRunProblems("t", { type: "object", properties: { dry_run: {} } }, true).length >= 1);
  assert.equal(dryRunProblems("t", { type: "object", oneOf: [{ properties: { dry_run: { type: "boolean" } } }] }, false).length, 1);
});

test("tools: the type, secret_refs and path_ref checks reject wrong definitions and the wrap shape is fixed", () => {
  const good = { type: "object", properties: { dry_run: { type: "boolean" }, hours: { type: "integer", minimum: 1 }, kind: { enum: ["a"] }, note: { type: "string" }, restore: { type: "object" }, paths: { type: "array", items: { type: "string" } } } };
  const flags: GateFlag[] = [{ name: "hours", value: "int" }, { name: "note", value: "json" }, { name: "restore", value: "json" }, { name: "paths", value: "json" }, { name: "mode", value: "enum", enum: ["a", "b"] }];
  assert.deepEqual(typeProblems("t", { ...good, properties: { ...good.properties, mode: { enum: ["a", "b"] } } }, flags, ["dry-run"]), []);
  assert.ok(typeProblems("t", { type: "object", properties: { dry_run: { type: "string" }, hours: {}, note: { type: "string" } } }, flags, ["dry-run"]).length >= 2);
  // JSON の引数の型: note は文字列(object を拒む)、restore はオブジェクト(文字列・配列を拒む)、paths は配列(文字列・オブジェクトを拒む)
  const wrong = (prop: string, schema: any) => typeProblems("t", { ...good, properties: { ...good.properties, mode: { enum: ["a", "b"] }, [prop]: schema } }, flags, ["dry-run"]);
  assert.equal(wrong("note", { type: "object" }).length > 0, true); // note をオブジェクトにした誤った定義(核が ARG_INVALID で拒む)
  assert.equal(wrong("note", {}).length > 0, true);
  assert.equal(wrong("restore", { type: "string" }).length > 0, true);
  assert.equal(wrong("restore", {}).length > 0, true);
  assert.equal(wrong("paths", { type: "string" }).length > 0, true);
  assert.equal(wrong("paths", { type: "object" }).length > 0, true);
  assert.equal(secretRefsProblems("t", { properties: {} }, true).length, 1);
  assert.deepEqual(secretRefsProblems("t", { properties: { secret_refs: { type: "array", uniqueItems: true, items: { type: "string" } } } }, true), []);
  assert.ok(secretRefsProblems("t", { properties: { secret_refs: { type: "array", items: { type: "string" } } } }, true).length >= 1);
  // paths: path_ref・実値のパス・pr:…/sub を受け、文字列でない要素を拒む。pr: の pattern だけの定義は誤り
  assert.deepEqual(pathRefProblems("t", { properties: { paths: { type: "array", items: { type: "string" } } } }), []);
  assert.equal(pathRefProblems("t", { properties: { paths: { type: "array", items: { type: "string", pattern: "^pr:[0-9a-f]{16}$" } } } }).length, 2); // /srv/data と pr:…/sub を拒む
  assert.equal(pathRefProblems("t", { properties: { paths: { type: "array", items: {} } } }).length, 3); // 数・null・オブジェクトを受ける
  assert.equal(shapeProblems("sitate_x", { name: "sitate_x", description: "d", input_schema: { type: "object" } }).length, 0);
  assert.ok(shapeProblems("sitate_x", { name: "sitate_x", input_schema: { type: "object" } }).length > 0);
  assert.ok(shapeProblems("sitate_x", { type: "object", properties: {} }).length > 0);
});

test("tools: the bootstrap, bootstrap-restore, common and definition checks accept the table's shape and reject the others", () => {
  const goodBoot = () => ({ type: "object", required: ["host", "admin_user", "admin_ref"], properties: { host: { type: "string" }, port: { type: "integer" }, admin_user: { type: "string" }, admin_ref: { type: "string" }, rekey: { type: "boolean" }, run_id: { type: "string" }, secret_refs: { type: "array", items: { type: "string" } } } });
  assert.deepEqual(bootstrapProblems(goodBoot()), []);
  const addProp = (d: any, k: string, v: any) => ({ ...d, properties: { ...d.properties, [k]: v } });
  assert.ok(bootstrapProblems(addProp(goodBoot(), "admin_password", { type: "string" })).length > 0); // 秘密の値を受ける引数
  assert.ok(bootstrapProblems(addProp(goodBoot(), "port", { type: "string" })).length > 0);
  assert.ok(bootstrapProblems(addProp(goodBoot(), "rekey", { type: "string" })).length > 0);
  assert.ok(bootstrapProblems({ ...goodBoot(), required: ["host", "run_id"] }).length > 0);
  const noRekey = goodBoot(); delete (noRekey.properties as any).rekey;
  assert.ok(bootstrapProblems(noRekey).length > 0);
  const goodRestore = () => ({ type: "object", required: ["step"], properties: { step: { enum: [...RESTORE_STEPS] }, run_id: { type: "string" }, host: { type: "string" }, port: { type: "integer" }, initial_user: { type: "string" }, initial_ref: { type: "string" }, backup: { type: "string" } } });
  assert.deepEqual(bootstrapRestoreProblems(goodRestore(), ["backup"]), []);
  assert.ok(bootstrapRestoreProblems(addProp(goodRestore(), "step", { enum: RESTORE_STEPS.slice(1) }), ["backup"]).length > 0); // 段が足りない
  assert.ok(bootstrapRestoreProblems(addProp(goodRestore(), "step", { type: "string" }), ["backup"]).length > 0); // 段が開いている
  assert.ok(bootstrapRestoreProblems(addProp(goodRestore(), "initial_password", { type: "string" }), ["backup"]).length > 0);
  assert.ok(bootstrapRestoreProblems({ ...goodRestore(), required: [] }, ["backup"]).length > 0);
  // 任意の引数を required に入れる・run_id が整数・definition が文字列
  assert.ok(commonProblems("t", { required: ["run_id"], properties: { run_id: { type: "string" } } }).length > 0);
  assert.ok(commonProblems("t", { required: ["dry_run"], properties: { dry_run: { type: "boolean" } } }).length > 0);
  assert.ok(commonProblems("t", { properties: { run_id: { type: "integer" } } }).length > 0);
  assert.ok(commonProblems("t", { properties: { run_id: {} } }).length > 0);
  assert.deepEqual(commonProblems("t", { required: ["kind"], properties: { run_id: { type: "string" } } }), []);
  assert.ok(definitionProblems("t", { properties: { definition: { type: "string" } } }).length > 0);
  assert.ok(definitionProblems("t", { properties: { definition: {} } }).length > 0);
  assert.deepEqual(definitionProblems("t", { properties: { definition: { type: "object" } } }), []);
});

test("attest: the step classes in the schema match the three unsigned steps (ui-only equivalent)", () => {
  const defs = readJson(join(contractSchemaDir, "data/change.json")).$defs;
  assert.deepEqual([...defs.attest_step_unsigned.enum].sort(), ["admin_access_closed", "admin_access_opened", "dest_disposed"]);
  assert.deepEqual(defs.attest_step_signed.anyOf[0].enum, ["R3", "R7"]);
  for (const u of defs.attest_step_unsigned.enum) assert.doesNotMatch(u, new RegExp(defs.attest_step_signed.anyOf[1].pattern));
});

test("tools: [5.14] sitate_init_storage mentions --adapter b2 in its top-level description", () => {
  needTools();
  assert.ok(String(load("sitate_init_storage").description).includes("`--adapter b2`"));
});
