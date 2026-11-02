// フレーズのパネル・チャット欄の送信前の検出・引用の照合(完了条件 6〜8・22・26・29)。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { guardOutgoing } from "../../src/main/chat-guard.ts";
import { ingestCoreOutput } from "../../src/main/core-output.ts";
import { advancePanel } from "../../src/main/phrase-panel.ts";
import { QuoteRejections, checkQuotes } from "../../src/main/quote-check.ts";
import type { KitFields } from "../../src/kit/types.ts";

const WORDS = JSON.parse(readFileSync(new URL("../../src/data/bip39-english.json", import.meta.url), "utf8")) as string[];
const phrase24 = WORDS.slice(100, 124);
const secretLine = JSON.stringify({ schema: "sitate.secret-string/v1", kind: "recovery_phrase", words: phrase24, keyring_version: 1 });
const envLine = JSON.stringify({ schema: "sitate.out/v1", command: ["init"], ok: true, phrase_emitted: true, result: {} });

test("標準出力の 2 レコード: 秘密はパネルだけに入り、モデルへ渡す封筒・JSON 化・例外に語が現れない", () => {
  const r = ingestCoreOutput(`${secretLine}\n${envLine}\n`, ["init"]);
  assert.equal(r.kind, "envelope");
  if (r.kind !== "envelope" || r.panel === null) throw new Error("パネルが無い");
  const leaked = [JSON.stringify(r.envelope), JSON.stringify(r.panel), String(r.panel), `${r.panel.state}`];
  for (const s of leaked) for (const w of phrase24) assert.ok(!s.includes(`"${w}"`) && !s.includes(w + " "), w);
  assert.equal(r.panel.view().words.length, 24);
  assert.equal(r.panel.view().words[0]!.word, phrase24[0]);
});

test("破棄する出力では秘密を渡さず、原因コードの封筒だけを返す(SSH が切れた出力を含む)", () => {
  for (const [out, end, code] of [
    [`${secretLine}\n`, "complete", "ENVELOPE_SECRET_MISMATCH"], [`${envLine}\n${secretLine}\n`, "complete", "ENVELOPE_SECRET_MISMATCH"],
    [`${secretLine}\n${envLine}\n`, "ssh_disconnected", "SSH_DISCONNECTED"],
  ] as const) {
    const r = ingestCoreOutput(out, ["init"], end);
    assert.equal(r.kind, "discarded");
    assert.equal(r.kind === "discarded" && r.envelope.error.code, code);
    for (const w of phrase24) assert.ok(!JSON.stringify(r).includes(w));
  }
});

test("パネルは verify と kit confirm の両方が済むまで進ませず、閉じたら語を返さない。印刷は描画部へ語を渡し、HTML をファイルに書かない", () => {
  const r = ingestCoreOutput(`${secretLine}\n${envLine}\n`, ["init"]);
  if (r.kind !== "envelope" || r.panel === null) throw new Error("パネルが無い");
  const p = r.panel;
  assert.equal(advancePanel(p), false);
  p.markVerified();
  assert.equal(advancePanel(p), false);
  let got: readonly string[] = [];
  const out = p.render((i) => { got = i.phrase; return { ok: false, reason: "does_not_fit" }; }, {} as KitFields, "ja");
  assert.equal(out.ok, false);
  assert.deepEqual([...got], phrase24);
  p.markKitConfirmed();
  assert.equal(advancePanel(p), true);
  assert.equal(p.state, "closed");
  assert.throws(() => p.view());
  assert.throws(() => p.render(() => ({ ok: false, reason: "does_not_fit" }), {} as KitFields, "ja"));
});

test("送信前の検出: 6 種の秘密の形をすべて止め、固定文のキーだけを返し、通常の文は通す", () => {
  const cases: [string, string, readonly string[]][] = [
    ["phrase", WORDS.slice(0, 12).join(" "), []], ["phrase", WORDS.slice(0, 12).map((w, i) => `${i + 1}. ${w}`).join("\n"), []],
    ["phrase", WORDS.slice(5, 20).join("、"), []],
    ["private_key", "-----BEGIN OPENSSH PRIVATE KEY-----\nxx", []], ["private_key", "-----BEGIN PRIVATE KEY-----", []],
    ["llm_api_key", "key is sk-ant-api03-" + "a".repeat(30), []],
    ["storage_key", "AKIAABCDEFGHIJKLMNOP", []], ["storage_key", "id K001" + "A".repeat(27) + " end", []],
    ["webhook_url", "https://hooks.example.test/services/T0000/B0000/" + "x".repeat(24), []],
    ["keychain_value", "token is s3cr3t-value-123 ok", ["s3cr3t-value-123"]],
  ];
  for (const [kind, text, kc] of cases) {
    const d = guardOutgoing(text, kc);
    assert.equal(d.send, false, kind);
    assert.ok(d.noticeKeys.includes(`chat.secret.detected.${kind}`), kind);
    assert.ok(d.noticeKeys.includes("chat.secret.input_box_guide"));
    assert.ok(d.noticeKeys.every((k) => k.startsWith("chat.")), "通知は固定文のキーだけ(入力の一部を含まない)");
  }
  for (const ok of ["バックアップの状況を教えてください", "abandon ability able about", "https://example.test/", "短い 7chars", WORDS.slice(0, 11).join(" ")]) {
    assert.equal(guardOutgoing(ok, ["short"]).send, true, ok);
  }
});

const status = { schema: "sitate.out/v1", command: ["status"], ok: true, result: { last_backup: { at: "2026-10-18T01:00:00Z", ok: true, checks_count: 0 }, uncovered_count: 3 } };
const results = new Map<string, unknown>([["status", status]]);

test("引用の照合: 一致する印は通り、外れる印・印の外の値は外れる", () => {
  const good = "最後のバックアップは [[status:/result/last_backup/at=2026-10-18T01:00:00Z]] で、チェックは [[status:/result/last_backup/checks_count=0]] 件です。";
  assert.equal(checkQuotes({ text: good, results }).ok, true);
  const bad = checkQuotes({ text: "最後は [[status:/result/last_backup/at=2026-10-19T01:00:00Z]] です。", results });
  assert.deepEqual(bad, { ok: false, rejected: ["status:/result/last_backup/at"], missing: [], unquoted: 0 });
  assert.equal(checkQuotes({ text: "[[status:/result/nothing=1]]", results }).ok, false);
  assert.equal(checkQuotes({ text: "[[version:/result/x=1]]", results }).ok, false);
  assert.equal(checkQuotes({ text: "[[status:/result/last_backup/ok=true]]", results }).ok, true);
  assert.equal(checkQuotes({ text: "[[status:/result/uncovered_count=3.0]]", results }).ok, true);
  const out = checkQuotes({ text: "最後は 2026-10-18T01:00:00Z です。", results });
  assert.equal(out.ok, false);
  assert.equal(!out.ok && out.unquoted, 1);
});

test("引用の照合の範囲: 問い・人の入力の値・説明書の引用は外す。印の外に核の結果に無い値を主張する文が 1 つあれば全体を表示しない", () => {
  const text = [
    "2026-12-01 に実行しますか？", "ご指定の 2026-12-02 で進めます。", "説明書の手順では 30 GB の目安です。",
    "最後は [[status:/result/last_backup/at=2026-10-18T01:00:00Z]] です。",
  ].join("\n");
  const ok = checkQuotes({ text, results, humanTexts: ["2026-12-02 にしてください"], citedBodies: ["目安は 30 GB です"] });
  assert.equal(ok.ok, true);
  const ng = checkQuotes({ text: text + "\n容量は 500 GB です。", results, humanTexts: ["2026-12-02 にしてください"], citedBodies: ["目安は 30 GB です"] });
  assert.equal(ng.ok, false);
  assert.equal(!ng.ok && ng.unquoted, 1);
});

test("restore の dumps[] がある報告では dumps[].loaded の印が必須", () => {
  const r = new Map(results).set("restore", { command: ["restore"], result: { dumps: [{ loaded: true }, { loaded: false }] } });
  const v = checkQuotes({ text: "戻しました。[[restore:/result/dumps/0/loaded=true]]", results: r });
  assert.deepEqual(!v.ok && v.missing, ["restore:/result/dumps/1/loaded"]);
  assert.equal(checkQuotes({ text: "[[restore:/result/dumps/0/loaded=true]] [[restore:/result/dumps/1/loaded=false]]", results: r }).ok, true);
});

test("表示しなかった応答の区画は次の入力の前に 1 つだけ置かれ、取り出した時点で消費される", () => {
  const q = new QuoteRejections();
  assert.equal(q.take(), null);
  for (const t of ["[[status:/result/uncovered_count=9]]", "[[status:/result/last_backup/ok=false]]"]) {
    const v = checkQuotes({ text: t, results });
    if (!v.ok) q.record(v);
  }
  const block = q.take()!;
  assert.ok(block.startsWith('<sitate-data command="app quote-check">'));
  const env = JSON.parse(block.split("\n")[1]!) as { ok: boolean; error: { code: string; detail: { rejected: string[] } } };
  assert.equal(env.ok, false);
  assert.equal(env.error.code, "QUOTE_REJECTED");
  assert.deepEqual(env.error.detail.rejected, ["status:/result/uncovered_count", "status:/result/last_backup/ok"]);
  assert.equal(q.take(), null);
});
