// コードが使う文のキーが日英のカタログに全てある(H6)。製品の描画は厳格(キーが無ければ例外)なので、欠けは画面にキー名を出す代わりに描画の例外になる。
// 画面の固定の面(renderer)の確認(H5・M1)は Electron の実機でしか確かめられない部分があり、ここでは構造を静的に検査する。
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";

const root = join(import.meta.dirname, "..", "..");
const json = (p: string): Record<string, unknown> => JSON.parse(readFileSync(join(root, p), "utf8")) as Record<string, unknown>;
const keys = json("src/i18n/keys.json");
const ja = json("src/i18n/ja/catalog.json") as Record<string, string>;
const en = json("src/i18n/en/catalog.json") as Record<string, string>;

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f);
    return statSync(p).isDirectory() ? sources(p) : p.endsWith(".ts") ? [p] : [];
  });
}
const files = ["src/main", "src/renderer", "src/testing"].flatMap((d) => sources(join(root, d)));
const NS = "(?:phrase|status|chat|card|app|core|ui)";

test("catalog-use: every literal catalog key written in the app code exists in keys.json, ja and en", () => {
  const used = new Set<string>();
  for (const f of files) {
    const text = readFileSync(f, "utf8");
    for (const m of text.matchAll(new RegExp(`"(${NS}\\.[a-z0-9_.-]+)"`, "g"))) used.add(m[1]!);
  }
  assert.ok(used.size > 60, `見つけたキーが少ない ${used.size}`);
  const missing = [...used].filter((k) => !Object.hasOwn(keys, k) || !Object.hasOwn(ja, k) || !Object.hasOwn(en, k));
  assert.deepEqual(missing, []);
});

test("catalog-use: the keys built from a variable have a catalog entry for every value the code can produce", () => {
  const need = [
    ...["host", "port", "fingerprint", "exists", "none"].map((r) => `status.target.invalid.${r}`),
    ...["yes", "no"].map((r) => `status.survey.v1.${r}`),
    ...["dir", "mount", "database"].map((r) => `status.survey.kind.${r}`),
    ...["print", "verify", "confirm", "cancel", "handwritten"].map((r) => `phrase.action.${r}`),
    ...["unavailable", "does_not_fit", "failed"].map((r) => `phrase.print.${r}`),
    "phrase.confirm.needs_print", "phrase.verify.input_label", "phrase.photo_caution", "phrase.storage_check",
    "chat.send.label", "chat.secret.submit.label", "chat.secret.label.generic", "chat.secret.label.admin", "chat.secret.label.llm", "status.panel.heading", "card.confirm_word.label",
    ...["ssh.unreachable", "ssh.auth_denied", "ssh.hostkey_mismatch", "ssh.disconnected", "envelope.invalid", "envelope.secret_mismatch", "envelope.too_many_records", "envelope.exit_unknown"].map((r) => `app.${r}`),
  ];
  const missing = need.filter((k) => !Object.hasOwn(keys, k) || !Object.hasOwn(ja, k) || !Object.hasOwn(en, k));
  assert.deepEqual(missing, []);
  for (const k of need) {
    assert.ok(!ja[k]!.startsWith("[仮]"), k);
    if (!k.startsWith("app.") && !k.startsWith("card.") && k !== "phrase.photo_caution" && k !== "phrase.storage_check") assert.ok(!en[k]!.startsWith("[tmp]"), `${k} の英文が仮のまま`);
  }
});

test("catalog-use: the product path has no key-name fallback translator", () => {
  assert.ok(!files.some((f) => readFileSync(f, "utf8").includes("tolerantTranslator")));
  assert.ok(!readFileSync(join(root, "src/i18n/format.ts"), "utf8").includes("tolerantTranslator"));
});

test("renderer (static): the card closes on its own decision and reports Esc as close, the phrase panel turns Esc into cancel, and the empty buttons get their text from the main process", () => {
  const r = readFileSync(join(root, "src/renderer/renderer.ts"), "utf8");
  assert.match(r, /dlg\.close\(\)/, "決定でカードを閉じる");
  assert.match(r, /dlg\.addEventListener\("close", \(\) => decide\(\{ kind: "close" \}\)/, "Esc は未決定の close として送る");
  assert.match(r, /\$\("phrase-panel"\)\.addEventListener\("cancel", \(e\) => \{ e\.preventDefault\(\); void sitate\.phraseAction\("cancel"\)/, "パネルの Esc は取り消しの操作");
  for (const id of ["chat-send", "secret-submit", "status-heading"]) assert.ok(r.includes(`$("${id}").textContent`), `${id} の文を主プロセスから受ける`);
  assert.match(r, /proofLabels/, "確認語の欄の横の文");
  const html = readFileSync(join(root, "src/renderer/index.html"), "utf8");
  for (const id of ["chat-send", "secret-submit", "status-heading"]) assert.match(html, new RegExp(`id="${id}"[^>]*></`), `${id} は空(文は主プロセスが送る)`);
  assert.ok(!/<(button|h1)[^>]*>[^<]+</.test(html), "固定の文を HTML に書かない");
});
