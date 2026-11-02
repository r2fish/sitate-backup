import { test } from "node:test";
import assert from "node:assert/strict";
import { parseStdoutRecords } from "../../src/main/stdout-records.ts";

// 標準出力の 2 レコードの判定の順(spec 3.1.4)。契約のベクトルに加えて、順序の取り合いを照らす。
const WORDS = Array.from({ length: 24 }, (_, i) => `synth${String.fromCharCode(97 + i)}x`);
const secret = JSON.stringify({ schema: "sitate.secret-string/v1", kind: "recovery_phrase", words: WORDS, keyring_version: 1 });
const env = (extra: object = {}) => JSON.stringify({ schema: "sitate.out/v1", command: ["init"], ok: true, dry_run: false, run_id: null, result: {}, warnings: [], error: null, ...extra });
const lines = (...l: string[]) => l.join("\n") + "\n";

test("stdout: the order of the checks is SSH cut, then 3+ records, then the secret-schema rule, then the envelope shape", () => {
  assert.deepEqual(parseStdoutRecords(lines(secret, env({ phrase_emitted: true }), env()), "ssh_disconnected"), { ok: false, code: "SSH_DISCONNECTED" });
  assert.deepEqual(parseStdoutRecords(lines(secret, env({ phrase_emitted: true }), env())), { ok: false, code: "ENVELOPE_TOO_MANY_RECORDS" });
  assert.deepEqual(parseStdoutRecords(lines(env({ phrase_emitted: true }), JSON.stringify({ schema: "sitate.other/v1" }))), { ok: false, code: "ENVELOPE_SECRET_MISMATCH" });
  assert.deepEqual(parseStdoutRecords(lines(env(), JSON.stringify({ schema: "sitate.other/v1" }))), { ok: false, code: "ENVELOPE_INVALID" });
});

test("stdout: a good [secret, envelope] hands the secret separately and no discard result carries a word", () => {
  const good = parseStdoutRecords(lines(secret, env({ phrase_emitted: true })));
  assert.ok(good.ok && good.secret !== null && good.secret.words.length === 24);
  if (good.ok) assert.ok(!JSON.stringify(good.envelope).includes(WORDS[0]!));
  for (const out of [lines(secret), lines(env(), secret), lines(secret, secret), lines(secret, env()), secret.slice(0, 80)]) {
    const r = parseStdoutRecords(out);
    assert.equal(r.ok, false);
    assert.ok(!JSON.stringify(r).includes(WORDS[0]!));
    assert.deepEqual(Object.keys(r).sort(), ["code", "ok"]);
  }
});

test("stdout: a secret record with a wrong number of words, a wrong kind or an extra field is discarded as a mismatch", () => {
  const mutate = (f: (r: any) => void) => { const r = JSON.parse(secret); f(r); return JSON.stringify(r); };
  for (const bad of [mutate((r) => r.words.pop()), mutate((r) => (r.kind = "other")), mutate((r) => (r.extra = 1)), mutate((r) => (r.keyring_version = 0)), mutate((r) => (r.words[0] = "Bad1"))]) {
    assert.deepEqual(parseStdoutRecords(lines(bad, env({ phrase_emitted: true }))), { ok: false, code: "ENVELOPE_SECRET_MISMATCH" });
  }
});
