import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { appRoot, contractSchemaDir, readJson } from "./helpers.ts";
import { PLACEHOLDER_MARK, isPlaceholder, layoutProblems, parseArgs, pseudo } from "../../src/i18n/check.ts";

// 作業指示書 5.1 の完了条件 9(メッセージカタログの骨格。GB-124)。
const ja: Record<string, string> = readJson(join(appRoot, "src/i18n/ja/catalog.json"));
const en: Record<string, string> = readJson(join(appRoot, "src/i18n/en/catalog.json"));
const keys: Record<string, { args: Record<string, string> }> = readJson(join(appRoot, "src/i18n/keys.json"));
const env = readJson(join(contractSchemaDir, "cli/envelope.json"));
const codes: string[] = [...new Set<string>([...env.$defs.error.properties.code.enum, ...env.$defs.warning.properties.code.enum])].sort();
const whys: string[] = env.$defs.next.properties.why.oneOf.find((x: any) => x.enum).enum;
const ops: string[] = readJson(join(contractSchemaDir, "data/change.json")).$defs.op.enum;
const subs: { command: string[]; public: string; later?: string }[] = readJson(join(contractSchemaDir, "cli/subcommands.json")).subcommands;
const toolNames = subs.filter((s) => s.public === "tool" && !s.later).map((s) => "sitate_" + s.command.join("_").replace(/-/g, "_"));
// アプリの原因コードの列挙(spec 3.7 の表。核の原因コードとは別の名前空間)。
const APP_CODES = [
  "tool.denied", "tool.arg_invalid", "tool.path_ref_unknown", "tool.human_cancelled", "tool.quote_rejected",
  "ssh.unreachable", "ssh.auth_denied", "ssh.hostkey_mismatch", "ssh.disconnected",
  "envelope.invalid", "envelope.secret_mismatch", "envelope.too_many_records", "envelope.exit_unknown",
  "bootstrap.signature_invalid", "bootstrap.privilege_required", "bootstrap.platform_unsupported",
  "bootstrap.package_failed", "bootstrap.already_done", "bootstrap.not_done", "bootstrap.restore_order",
  "llm.key_invalid", "llm.billing", "llm.rate_limited", "llm.provider_error", "llm.model_not_found", "llm.no_response",
  "agent.exited", "agent.turn_limit", "agent.output_truncated",
];

const family = (prefix: string) => Object.keys(keys).filter((k) => k.startsWith(prefix)).map((k) => k.slice(prefix.length)).sort();

test("catalog: ja and en keys equal the key list (keys.json) and each other", () => {
  assert.deepEqual(Object.keys(ja).sort(), Object.keys(keys).sort());
  assert.deepEqual(Object.keys(en).sort(), Object.keys(keys).sort());
});

test("catalog: the key families match their sources (codes, why, app codes, ops, tools)", () => {
  assert.deepEqual(family("core.code."), codes);
  assert.deepEqual(family("core.why."), [...whys].sort());
  assert.deepEqual(family("app."), APP_CODES.slice().sort());
  assert.deepEqual(family("card.op.").map((k) => k.replace(/\.title$/, "")).sort(), [...ops].sort());
  assert.deepEqual(family("chat.tool.display.name."), [...toolNames].sort());
  assert.deepEqual(family("core.reason."), ["ARG_INVALID.unsupported_port", "RESTORE_ENROLL_MISMATCH.unsupported_platform"]);
  for (const k of Object.keys(keys)) assert.match(k, /^(core\.(code|why|reason)|app|card|chat|status|phrase)\.[a-zA-Z0-9_.-]+$/, k);
});

test("catalog: no empty values, placeholders carry the mark", () => {
  for (const [lang, c] of [["ja", ja], ["en", en]] as const) {
    for (const [k, v] of Object.entries(c)) {
      assert.ok(typeof v === "string" && v.length > 0, `${lang}:${k} is empty`);
      assert.ok(!isPlaceholder(v) || v.startsWith(PLACEHOLDER_MARK[lang]), `${lang}:${k} mark`);
    }
  }
});

test("catalog: ICU placeholders (name and type) match keys.json in ja and en", () => {
  for (const [lang, c] of [["ja", ja], ["en", en]] as const) {
    for (const [k, v] of Object.entries(c)) assert.deepEqual(parseArgs(v), keys[k].args, `${lang}:${k}`);
  }
});

test("catalog: pseudo-locale keeps placeholders and the layout judge passes for the five surfaces", () => {
  for (const [lang, c] of [["ja", ja], ["en", en]] as const) {
    for (const [k, v] of Object.entries(c)) {
      assert.deepEqual(parseArgs(pseudo(v)), parseArgs(v), `${lang}:${k}`);
      assert.deepEqual(layoutProblems(k, v), [], `${lang}:${k}`);
    }
  }
});

test("catalog: the layout judge catches an overflow and a broken placeholder", () => {
  assert.ok(layoutProblems("card.section.cost", "あ".repeat(41)).length > 0);
  assert.ok(layoutProblems("card.actions.approve", "承認\n承認").length > 0);
  assert.throws(() => parseArgs("{count"));
  // 原因コードの説明と why の文は長い欄(次の行動まで述べる文を置ける)。
  for (const k of ["core.code.STORAGE_DENIED", "core.why.hold"]) {
    assert.deepEqual(layoutProblems(k, "あ".repeat(120)), [], k);
    assert.ok(layoutProblems(k, "あ".repeat(401)).length > 0, k);
  }
});
