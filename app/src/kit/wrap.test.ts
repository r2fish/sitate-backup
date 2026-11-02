import { test } from "node:test";
import assert from "node:assert/strict";
import { HALF_EM, MARK_EM, MONO_PT, PROP_WIDE_EM, PT_MM, WIDTH_SAFETY } from "./metrics.ts";
import { CONTENT_W_MM } from "./measure.ts";
import { CONT_NOTE } from "./layout.ts";
import { charEm, textEm, wrapLine, type Row } from "./wrap.ts";
import { renderKit } from "./render.ts";
import { PHRASE, realFields } from "./paper-fixture.ts";

// 折り返しの試験(コマンドの行が語の途中で折れない・切れない・見積りが実際より少なくない)。

let seed = 12345;
const rnd = (n: number) => {
  seed = (seed * 1103515245 + 12345) % 2147483648;
  return seed % n;
};
const CHARS = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:$\"=mWw";
const word = (len: number) => Array.from({ length: len }, () => CHARS[rnd(CHARS.length)]).join("");

const INNER = CONTENT_W_MM - 1.6;
const capEm = (pt: number) => (INNER * WIDTH_SAFETY) / (pt * PT_MM);

function joined(rows: readonly Row[]): string {
  return rows.reduce((acc, r, i) => acc + (i === 0 ? "" : rows[i - 1].cont ? "" : " ") + r.text, "");
}

test("[u3b-impl] mono width estimate is not narrower than real monospace fonts (about 0.6 em) and wide proportional letters are not under-counted", () => {
  assert.ok(HALF_EM >= 0.6);
  assert.ok(PROP_WIDE_EM >= 0.9 && charEm("m", false) >= 0.9 && charEm("W", false) >= 0.9);
  assert.equal(charEm("核", true), 1);
});

test("[u3b-impl] wrapping: every row fits the estimated width, units are never split when they fit, and a split word is marked", () => {
  for (let n = 0; n < 300; n++) {
    const units = Array.from({ length: 1 + rnd(14) }, () => (rnd(2) === 0 ? word(1 + rnd(30)) : `--${word(2 + rnd(10))} ${word(1 + rnd(rnd(5) === 0 ? 120 : 40))}`));
    const rows = wrapLine({ t: units.join(" "), mono: true, units }, MONO_PT, INNER, 0);
    const cap = capEm(MONO_PT);
    for (const r of rows) assert.ok(textEm(r.text, true) + (r.cont ? MARK_EM : 0) <= cap + 1e-9, `row too wide: ${r.text}`);
    assert.equal(joined(rows), units.join(" "), "no character is lost or added");
    for (const u of units) if (textEm(u, true) <= cap) assert.ok(rows.some((r) => r.text.includes(u)), `unit split: ${u}`);
    assert.ok(rows.slice(0, -1).every((r) => r.cont || r.text.length > 0));
  }
});

test("[u3b-impl] wrapping: the row count is never less than a plain word wrap at the same widths (Japanese text and commands)", () => {
  const ref = (text: string, pt: number, mono: boolean) => {
    const cap = capEm(pt);
    let rows = 1;
    let used = 0;
    for (const w of text.split(" ")) {
      const e = textEm(w, mono);
      const sp = used === 0 ? 0 : textEm(" ", mono);
      if (used > 0 && used + sp + e > cap) { rows += 1; used = e; } else used += sp + e;
    }
    return rows;
  };
  for (let n = 0; n < 200; n++) {
    const mono = rnd(2) === 0;
    const t = Array.from({ length: 5 + rnd(60) }, () => word(1 + rnd(25))).join(" ");
    const pt = mono ? MONO_PT : 5.8;
    assert.ok(wrapLine({ t, mono }, pt, INNER, 0).length >= ref(t, pt, mono), t);
  }
});

test("[u3b-impl] wrapping: prose breaks between Japanese characters but keeps closing punctuation with the character before it", () => {
  const t = "あ".repeat(400) + "。" + "い".repeat(10);
  const rows = wrapLine({ t }, 5.8, INNER, 0);
  assert.ok(rows.length > 1);
  assert.equal(rows.map((r) => r.text).join(""), t);
  for (const r of rows.slice(1)) assert.ok(!"、。".includes(r.text[0]), r.text);
});

test("[u3b-impl] a command word longer than a line is folded with a visible mark and the sheet explains the mark", () => {
  const f = { ...realFields(), distribution_url: "https://dist." + "x".repeat(190) + ".example.invalid" };
  const r = renderKit({ fields: f, phrase: PHRASE, lang: "ja" });
  assert.ok(r.ok, JSON.stringify(r));
  const step = r.printable.html.match(/<section class="blk" data-kind="product"[^>]*>(.*?)<\/section>/s)?.[1] ?? "";
  assert.ok(step.includes('<i class="k">»</i>'));
  assert.ok(step.replace(/<[^>]*>/g, "").includes(CONT_NOTE));
});

test("[u3b-impl] realistic long prefixes still fit on one sheet, and fields too long for the sheet are refused instead of being cut", () => {
  for (const prefix of ["tokyo/office/server01/sitate/", "a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/"]) {
    assert.equal(renderKit({ fields: realFields({ prefix, bucket: "sitate-backup-office-tokyo-0001" }), phrase: PHRASE, lang: "ja" }).ok, true, prefix);
  }
  const r = renderKit({ fields: realFields({ prefix: "p".repeat(5000) }), phrase: PHRASE, lang: "ja" });
  assert.equal(r.ok, false);
});

test("[u3b-impl] does_not_fit names the field whose shrinking makes the sheet fit, not just the longest one", () => {
  const f = realFields();
  const r = renderKit({ fields: { ...f, distribution_url: "https://d.example.invalid/" + "a".repeat(9000), host_key_fp: "h".repeat(400) }, phrase: PHRASE, lang: "ja" });
  assert.deepEqual(r, { ok: false, reason: "does_not_fit", field: "distribution_url" });
});
