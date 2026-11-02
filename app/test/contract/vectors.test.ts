import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { Ajv2020 } from "ajv/dist/2020.js";
import { contractSchemaDir, listFiles, readJson, repoRoot } from "./helpers.ts";
import { parseStdoutRecords } from "../../src/main/stdout-records.ts";

// 作業指示書 3.2 のアプリの行: 封筒・標準出力の 2 レコード・原因コードの試験ベクトルの扱い。
// ベクトルは internal/contract/testdata/ を読む(製品リポジトリの中のデータだけ)。アプリの実装の挙動は U3a の試験が照らし、ここはベクトルがアプリの入力として使えることを照らす。
const vectorDir = join(repoRoot, "internal", "contract", "testdata", "cli");
const keys = readJson(join(import.meta.dirname, "..", "..", "src/i18n/keys.json"));

function ajvWithCliSchemas(): Ajv2020 {
  const ajv = new Ajv2020({ strict: false, allErrors: true });
  for (const d of ["cli", "data"]) for (const f of listFiles(join(contractSchemaDir, d), ".json")) ajv.addSchema(readJson(join(contractSchemaDir, d, f)));
  return ajv;
}
const envelopeId = readJson(join(contractSchemaDir, "cli/envelope.json")).$id as string;

test("vectors: envelope vectors the core can emit validate, and the invalid ones are rejected", () => {
  const ajv = ajvWithCliSchemas();
  const validate = ajv.getSchema(envelopeId);
  assert.ok(validate, "envelope schema");
  const valid = listFiles(join(vectorDir, "valid"), ".json");
  const invalid = listFiles(join(vectorDir, "invalid"), ".json");
  assert.ok(valid.length >= 2 && invalid.length >= 3, "封筒の試験ベクトルが足りない");
  for (const f of valid) assert.ok(validate(readJson(join(vectorDir, "valid", f))), `${f}: ${JSON.stringify(validate.errors)}`);
  for (const f of invalid) assert.equal(validate(readJson(join(vectorDir, "invalid", f))), false, f);
});

test("vectors: every core error code has a detail sample that fits its detail schema, and a plain-language catalog key", () => {
  const ajv = ajvWithCliSchemas();
  const samples = readJson(join(vectorDir, "detail-samples.json"));
  const detailId = readJson(join(contractSchemaDir, "cli/detail.json")).$id as string;
  const codes: string[] = readJson(join(contractSchemaDir, "cli/codes.json")).$defs.code.enum;
  const detailDefs = Object.keys(readJson(join(contractSchemaDir, "cli/detail.json")).$defs);
  assert.deepEqual(Object.keys(samples).sort(), detailDefs.sort(), "sample と detail の定義の集合");
  for (const [code, detail] of Object.entries(samples)) {
    assert.ok(codes.includes(code), `${code} が原因コードの列挙に無い`);
    const v = ajv.getSchema(`${detailId}#/$defs/${code}`);
    assert.ok(v, code);
    assert.ok(v(detail), `${code}: ${JSON.stringify(v.errors)}`);
    assert.ok(`core.code.${code}` in keys, `カタログに core.code.${code} が無い`);
  }
});

// 標準出力の 2 レコード(spec 3.1.4): 先頭が秘密のレコード、次が封筒。ベクトル stdout-records.json(U0 の持ち物)を、U3a が実装する
// src/main/stdout-records.ts の parseStdoutRecords に通す。判定は結果の全体の一致(余分な欄 raw などを許さない)と、
// 破棄・受理のどの結果にも合成語が(Buffer・配列の中を含めて)1 語も残らないことで照らす。ok の結果で語を持つのは secret だけ。
type StdoutCase = { name: string; stdout?: any[]; raw?: string[]; end?: "complete" | "ssh_disconnected"; want: "ok" | "discard"; has_secret?: boolean; error?: string };
const stdoutCases: StdoutCase[] = readJson(join(vectorDir, "stdout-records.json")).cases;
const asStdout = (records: any[]) => records.map((r) => JSON.stringify(r)).join("\n") + "\n";
/** ベクトルの標準出力の文字列。raw(途中で切れた行を含む)は行を改行でつなぎ、最後の行に改行を付けない(切れた行で終わる)。 */
const outputOf = (c: StdoutCase) => (c.raw ? c.raw.join("\n") : asStdout(c.stdout!));
const recordsOf = (c: StdoutCase) => c.stdout!;
const SECRET_WORDS: string[] = stdoutCases.find((c) => c.name === "secret-then-envelope")!.stdout![0].words;
const hasWord = (text: string) => SECRET_WORDS.some((w) => text.includes(w));

/** 値の中(文字列・配列・オブジェクト・Buffer・TypedArray・0〜255 の数の列・Map・Set。列挙できない欄を含む)に合成語が 1 語でもあるか。 */
function leaks(v: unknown, seen = new Set<unknown>()): boolean {
  if (typeof v === "string") return hasWord(v);
  if (v === null || typeof v !== "object" || seen.has(v)) return false;
  seen.add(v);
  if (ArrayBuffer.isView(v)) return hasWord(Buffer.from(v.buffer, v.byteOffset, v.byteLength).toString("latin1"));
  if (v instanceof ArrayBuffer) return hasWord(Buffer.from(v).toString("latin1"));
  if (Array.isArray(v) && v.length > 0 && v.every((x) => Number.isInteger(x) && x >= 0 && x < 256) && hasWord(Buffer.from(v).toString("latin1"))) return true;
  if (v instanceof Map || v instanceof Set) return [...v].some((e) => leaks(e, seen));
  return Reflect.ownKeys(v).some((k) => leaks(typeof k === "string" ? k : "", seen) || leaks((v as any)[k], seen));
}

/** 例外を投げずに結果を返すこと(例外は破棄の封筒にならない)。 */
function parse(stdout: string, label: string, end: "complete" | "ssh_disconnected" = "complete") {
  try {
    return parseStdoutRecords(stdout, end);
  } catch (e) {
    assert.fail(`${label}: 例外を投げた: ${String(e)}`);
  }
}

test("vectors: the stdout two-record vectors cover one record, two records, and every discarded shape", () => {
  assert.deepEqual(stdoutCases.map((c) => c.name).sort(), ([
    "emitted-without-secret", "envelope-only", "envelope-only-phrase-false", "envelope-then-other-schema", "envelope-then-secret", "null-only", "number-only", "secret-only", "secret-then-envelope",
    "secret-then-envelope-no-phrase-emitted", "secret-then-envelope-phrase-emitted-false", "secret-then-null", "secret-then-other-schema", "secret-then-secret", "string-only", "three-records",
    "ssh-disconnected-secret-visible", "ssh-disconnected-no-secret", "complete-truncated-envelope-secret-visible", "complete-truncated-envelope-no-secret",
  ]).sort());
  const byName = (n: string) => stdoutCases.find((c) => c.name === n)!;
  assert.deepEqual(recordsOf(byName("envelope-only")).map((r) => r.schema), ["sitate.out/v1"]);
  assert.deepEqual(recordsOf(byName("secret-then-envelope")).map((r) => r.schema), ["sitate.secret-string/v1", "sitate.out/v1"]);
  // 切れ方の 3 通り × 秘密の有無
  const ends = stdoutCases.filter((c) => c.raw).map((c) => `${c.end}:${c.has_secret}:${c.error}`).sort();
  assert.deepEqual(ends, ["complete:false:ENVELOPE_INVALID", "complete:true:ENVELOPE_SECRET_MISMATCH", "ssh_disconnected:false:SSH_DISCONNECTED", "ssh_disconnected:true:SSH_DISCONNECTED"]);
});

test("vectors: parseStdoutRecords returns exactly {ok, secret, envelope} for 1 or 2 records and exactly {ok:false, code} for every bad shape", () => {
  for (const c of stdoutCases) {
    const got = parse(outputOf(c), c.name, c.end);
    if (c.want === "ok") {
      const rs = recordsOf(c);
      const secret = c.has_secret ? rs[0] : null;
      assert.deepEqual(got, { ok: true, secret, envelope: rs[rs.length - 1] }, c.name);
      assert.ok(!leaks((got as any).envelope), `${c.name}: 封筒に秘密が混じる`);
      assert.equal(leaks({ ...(got as any), secret: null }), false, `${c.name}: secret の外に秘密が残る`);
    } else {
      assert.deepEqual(got, { ok: false, code: c.error }, c.name);
      assert.ok(!leaks(got), `${c.name}: 破棄の結果に秘密が残る`);
    }
  }
});

test("vectors: parseStdoutRecords treats output that is not an envelope as ENVELOPE_INVALID and never throws", () => {
  for (const bad of ["", "\n", "not json\n", "[1]\n", "null\n", asStdout([{ schema: "sitate.other/v1" }])]) {
    assert.deepEqual(parse(bad, JSON.stringify(bad)), { ok: false, code: "ENVELOPE_INVALID" }, JSON.stringify(bad));
  }
});

// 途中で切れた出力(spec 3.1.4「秘密のレコードだけで封筒の無い出力(途中切断を含む)」)。フレーズを表示せず、結果に秘密を残さない。
test("vectors: truncated output after the secret record is discarded without the secret", () => {
  const secretLine = JSON.stringify(stdoutCases.find((c) => c.name === "secret-only")!.stdout![0]);
  const envLine = JSON.stringify(stdoutCases.find((c) => c.name === "secret-then-envelope")!.stdout![1]);
  for (const [name, out] of [["secret without newline", secretLine], ["secret then half of the envelope", secretLine + "\n" + envLine.slice(0, 30)], ["half of the secret", secretLine.slice(0, 60) + "\n"]] as const) {
    const got = parse(out, name);
    assert.equal(got.ok, false, name);
    assert.ok(!leaks(got), `${name}: 結果に秘密が残る`);
  }
  // 接続が切れたと分かるとき: 秘密の有無に関わらず SSH_DISCONNECTED(秘密は残さない)
  for (const out of [secretLine + "\n" + envLine.slice(0, 30), envLine.slice(0, 30), secretLine, ""]) {
    const got = parse(out, "ssh", "ssh_disconnected");
    assert.deepEqual(got, { ok: false, code: "SSH_DISCONNECTED" }, JSON.stringify(out));
    assert.ok(!leaks(got));
  }
  assert.deepEqual(parse(secretLine, "secret only"), { ok: false, code: "ENVELOPE_SECRET_MISMATCH" });
  assert.deepEqual(parse(secretLine + "\n" + envLine.slice(0, 30), "secret then half"), { ok: false, code: "ENVELOPE_SECRET_MISMATCH" });
});

// 決定(設計の監督): schema が sitate.secret-string/v1 なのに正しく読めない秘密のレコードは「正しい秘密のレコードが無い」と扱い、
// ENVELOPE_SECRET_MISMATCH で破棄する(フレーズを表示せず、結果に秘密を残さない)。schema が別のものは秘密のレコードではなく、単独なら ENVELOPE_INVALID、
// phrase_emitted が true の封筒と並べば「秘密のレコードが無い」ので ENVELOPE_SECRET_MISMATCH(spec 3.1.4 の判定の順)。
test("vectors: a malformed secret record is discarded as ENVELOPE_SECRET_MISMATCH, one with another schema by the spec 3.1.4 order", () => {
  const invalid: { name: string; record: any }[] = readJson(join(vectorDir, "stdout-records.json")).secret_record_invalid;
  assert.ok(invalid.some((v) => v.name === "wrong-kind"));
  const envelope = stdoutCases.find((c) => c.name === "secret-then-envelope")!.stdout![1];
  for (const v of invalid) {
    for (const records of [[v.record, envelope], [v.record]]) {
      const code = v.record.schema === "sitate.secret-string/v1" || records.length === 2 ? "ENVELOPE_SECRET_MISMATCH" : "ENVELOPE_INVALID";
      const got = parse(asStdout(records), `${v.name} (${records.length})`);
      assert.deepEqual(got, { ok: false, code }, `${v.name} (${records.length})`);
      assert.ok(!leaks(got), `${v.name}: 結果に秘密が残る`);
    }
  }
  assert.ok(invalid.some((v) => v.record.schema !== "sitate.secret-string/v1") && invalid.some((v) => v.record.schema === "sitate.secret-string/v1"));
});

// 検査そのものの確認: 語を残す実装(raw・Buffer・配列の複製)を leaks が見つける。
test("vectors: the leak check finds a word kept in a string, a Buffer, a byte array or a hidden field", () => {
  const w = SECRET_WORDS[0]!;
  const hidden = { ok: false, code: "ENVELOPE_INVALID" };
  Object.defineProperty(hidden, "raw", { value: w, enumerable: false });
  for (const v of [{ raw: `x${w}` }, { raw: Buffer.from(w) }, { raw: [...Buffer.from(w)] }, { words: [w] }, new Map([["k", w]]), hidden]) assert.ok(leaks(v));
  assert.equal(leaks({ ok: false, code: "ENVELOPE_INVALID" }), false);
});
