import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { PHRASE, realFields, vectorFields } from "./paper-fixture.ts";
import { renderKit } from "./render.ts";
import { LLM_KEY_LINE } from "./content-front.ts";
import type { KitBlock, KitFields, KitRenderSuccess, KitStepId, PhraseWords } from "./types.ts";
import { fileURLToPath } from "node:url";

// 復旧キットの描画部の試験(作業指示書 5.14b の完了条件 2・3)。画素は比べず、配置の構造と HTML の中身を照らす。
const ALL_STEPS: KitStepId[] = ["1", "2", "2-no-app", "3", "3-no-adapter", "4-journal", "4", "5", "6"];
const fullFields = realFields;

function ok(f: KitFields): KitRenderSuccess {
  const r = renderKit({ fields: f, phrase: PHRASE, lang: "ja" });
  assert.ok(r.ok, JSON.stringify(r));
  return r;
}

const kinds = (bs: readonly KitBlock[], page: string) => bs.filter((b) => b.page === page).map((b) => b.kind);

test("[u3b-impl] layout follows spec 4.7: front has the table fields in order, back has the two back-side fields", () => {
  const f = fullFields();
  const { layout } = ok(f).printable;
  assert.equal(layout.paper, "A4-portrait");
  assert.deepEqual(kinds(layout.blocks, "front"), ["title", "product", "installer", "signing_keys", "storage", ...ALL_STEPS.map(() => "restore_step"), "restore_proof", "explanation", "phrase_entry", "host_key", "custody"]);
  assert.deepEqual(kinds(layout.blocks, "back"), ["provider_notes", "production_restore_notes"]);
  assert.deepEqual(layout.blocks.filter((b) => b.kind === "restore_step").map((b) => b.step_id), ALL_STEPS);
  assert.ok(layout.blocks.filter((b) => b.kind !== "restore_step").every((b) => b.step_id === undefined));
});

test("[u3b-impl] layout: order is 0.. per page, boxes are stacked top to bottom, inside A4 and without overlap", () => {
  for (const f of [vectorFields(), fullFields()]) {
    const { layout } = ok(f).printable;
    for (const page of ["front", "back"]) {
      const bs = layout.blocks.filter((b) => b.page === page);
      assert.deepEqual(bs.map((b) => b.order), bs.map((_, i) => i));
      let bottom = 0;
      for (const b of bs) {
        assert.ok(b.box.x_mm >= 0 && b.box.x_mm + b.box.w_mm <= 210, `x ${page}:${b.order}`);
        assert.ok(b.box.y_mm >= bottom, `overlap ${page}:${b.order}`);
        bottom = b.box.y_mm + b.box.h_mm;
        assert.ok(bottom <= 297, `y ${page}:${b.order}`);
      }
    }
  }
});

test("[u3b-impl] phrase fields, storage, hashes and step numbers appear in the html in the spec's places", () => {
  const f = fullFields();
  const html = ok(f).printable.html;
  const section = (kind: string) => {
    const m = html.match(new RegExp(`<section class="blk" data-kind="${kind}"[^>]*>(.*?)</section>`, "s"));
    assert.ok(m, kind);
    return m[1];
  };
  const phrase = section("phrase_entry");
  let at = -1;
  PHRASE.forEach((w, i) => {
    const n = phrase.indexOf(`${i + 1}. ${w}<`);
    assert.ok(n > at, `word ${i + 1} in order`);
    at = n;
  });
  const product = section("product");
  for (const h of [f.core_sha256.linux_x86_64, f.core_sha256.linux_arm64, f.sbom_sha256, f.source_sha256, f.bin_path, f.distribution_url, f.core_version]) assert.ok(product.includes(h), h);
  const st = section("storage");
  for (const v of [f.storage.provider, f.storage.endpoint, f.storage.bucket, f.storage.prefix, f.storage.repo_id, f.storage.keyring_id, f.storage.key_ref]) assert.ok(st.includes(v), v);
  const keys = section("signing_keys");
  for (const k of f.signing_keys) assert.ok(keys.includes(k.public_key) && keys.includes(k.fp));
  assert.ok(keys.includes("sha256sum sitate") && keys.includes("minisign -Vm sitate -x sitate.sig -P"));
  assert.ok(section("host_key").includes(f.host_key_fp));
  assert.ok(section("title").includes(f.kit_version) && section("title").includes(f.issued_on));
  for (const id of ALL_STEPS) assert.ok(html.includes(`data-step="${id}"`), id);
  const steps = [...html.matchAll(/data-step="([^"]+)"/g)].map((m) => m[1]);
  assert.deepEqual(steps, ALL_STEPS);
  assert.match(section("custody"), /保管場所 1.*保管場所 2/s);
});

test("[u3b-impl] explanation: LLM API key line, globals.sql first, and the production-restore points on the back", () => {
  const html = ok(fullFields()).printable.html;
  assert.ok(html.includes("新しい端末ではアプリに LLM API キーを入れ直す"));
  assert.equal(LLM_KEY_LINE, "新しい端末ではアプリに LLM API キーを入れ直す。");
  assert.match(html, /globals\.sql を先に読み、LOGIN を持つロールのパスワードを設定し直す/);
  const back = html.slice(html.indexOf('data-page="back"'));
  for (const w of ["CREATE TABLESPACE", "SUPERUSER", "REPLICATION", "BYPASSRLS", "CREATEROLE", "sitate-ro", "pg_restore --no-tablespaces", "LOGIN"]) assert.ok(back.includes(w), w);
  assert.ok(back.includes(vectorFields().provider_hints.key_name));
  assert.ok(back.includes(vectorFields().provider_hints.bin_prefix));
});

test("[u3b-impl] an unknown provider kind gets a single sentence pointing to the appendix", () => {
  const f = { ...vectorFields(), provider_hints: { kind: "other-kind", key_name: "k", bin_prefix: "p/" } };
  const html = ok(f).printable.html;
  assert.match(html, /手順書の付録「事業者別」を見ること/);
});

test("[u3b-impl] the phrase entry and the hashes sit at the same place whatever the words and the language-independent fields are", () => {
  const f = fullFields();
  const a = ok(f).printable.layout;
  const other = Array.from({ length: 24 }, (_, i) => `zz${i}`) as unknown as PhraseWords;
  const r = renderKit({ fields: f, phrase: other, lang: "ja" });
  assert.ok(r.ok);
  assert.deepEqual(r.printable.layout, a);
  assert.ok(!JSON.stringify(a).includes("alpha1"));
});

test("[u3b-impl] html is one self-contained document with the Japanese font embedded", () => {
  const html = ok(vectorFields()).printable.html;
  assert.match(html, /^<!doctype html>/);
  assert.match(html, /@font-face\{font-family:"SitateJP";src:url\(data:font\/woff2;base64,/);
  assert.doesNotMatch(html, /(src|href)="https?:/);
  assert.doesNotMatch(html, /<script/i);
});

test("[u3b-impl] fields too long for one sheet: failure with the field name, no html, no words in the failure", () => {
  const f = fullFields();
  const long = { ...f, distribution_url: "https://dist.example.invalid/" + "a".repeat(6000) };
  const r = renderKit({ fields: long, phrase: PHRASE, lang: "ja" });
  assert.equal(r.ok, false);
  assert.deepEqual(r, { ok: false, reason: "does_not_fit", field: "distribution_url" });
  const many = { ...f, steps: f.steps.map((x) => ({ ...x, argv: [...x.argv, ...x.argv, ...x.argv, ...x.argv, ...x.argv, ...x.argv] })) };
  const r2 = renderKit({ fields: many, phrase: PHRASE, lang: "ja" });
  assert.equal(r2.ok, false);
  assert.equal(r2.ok === false && r2.reason, "does_not_fit");
  for (const w of PHRASE) assert.ok(!JSON.stringify(r2).includes(w));
});

test("[u3b-impl] the sample fields and a realistic full-step kit both fit on one sheet", () => {
  assert.equal(renderKit({ fields: vectorFields(), phrase: PHRASE, lang: "ja" }).ok, true);
  assert.equal(renderKit({ fields: fullFields(), phrase: PHRASE, lang: "ja" }).ok, true);
});

test("[u3b-impl] English is not drawn in iteration 1, and a wrong word count is refused without printing the words", () => {
  const r = renderKit({ fields: vectorFields(), phrase: PHRASE, lang: "en" });
  assert.deepEqual(r, { ok: false, reason: "lang_unsupported" });
  assert.throws(() => renderKit({ fields: vectorFields(), phrase: PHRASE.slice(0, 5) as unknown as PhraseWords, lang: "ja" }), (e: Error) => !PHRASE.some((w) => e.message.includes(w)));
});

// 完了条件 3: 24 語をディスク・ログに書かず、印刷の一時ファイルを残さない。
// 試験の間だけ OS の一時ディレクトリを専用の空のディレクトリに向け(他の処理と混ざらない)、描画の前後で中身を比べる。
test("[u3b-impl] rendering leaves the OS temp dir unchanged and writes nothing to stdout/stderr", () => {
  const root = mkdtempSync(join(tmpdir(), "U3b-sandbox-"));
  const osTmp = join(root, "tmp");
  mkdirSync(osTmp);
  const snap = (d: string) => readdirSync(d, { recursive: true }).map(String).sort();
  const seen: string[] = [];
  const o = process.stdout.write.bind(process.stdout);
  const e = process.stderr.write.bind(process.stderr);
  const hook = (chunk: unknown) => (seen.push(String(chunk)), true);
  const prevTmp = process.env.TMPDIR;
  const origLog = console.log;
  process.env.TMPDIR = osTmp;
  process.stdout.write = hook as typeof process.stdout.write;
  process.stderr.write = hook as typeof process.stderr.write;
  console.log = (...a: unknown[]) => void seen.push(a.join(" "));
  try {
    ok(fullFields());
    renderKit({ fields: { ...fullFields(), distribution_url: "x".repeat(9000) }, phrase: PHRASE, lang: "ja" });
  } finally {
    process.stdout.write = o;
    process.stderr.write = e;
    console.log = origLog;
    if (prevTmp === undefined) delete process.env.TMPDIR;
    else process.env.TMPDIR = prevTmp;
  }
  try {
    assert.deepEqual(snap(osTmp), []);
    assert.deepEqual(seen, []);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

function sourceFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? sourceFiles(join(dir, e.name)).map((n) => join(e.name, n)) : [e.name]));
}

test("[u3b-impl] the renderer source imports no node module, has no way to write a file, a fd, a temp dir or the log, and does not depend on import.meta", () => {
  const dir = fileURLToPath(new URL(".", import.meta.url));
  const files = sourceFiles(dir).filter((n) => n.endsWith(".ts") && !n.endsWith(".test.ts") && n !== "types.ts" && n !== "paper-fixture.ts");
  assert.ok(files.length >= 10, files.join());
  const bad = /from "node:|require\(|import\(|import\.meta|__dirname|__filename|writeFile|writeSync|appendFile|createWriteStream|mkdtemp|mkdir|tmpdir|rename\(|copyFile|openSync|\bfs\b|\bopen\(|\bwrite\(|console\.|\bprocess\b|child_process|fetch\(|XMLHttpRequest|WebSocket|\bhttps?\.request/;
  for (const n of files.filter((x) => x.startsWith("font-data-"))) {
    const body = readFileSync(join(dir, n), "utf8").split("\n").filter((l) => !l.startsWith("//")).join("\n");
    const m = body.match(/^export const (FONT_\w+_WOFF2_B64) =\n {2}"([A-Za-z0-9+/=]+)";\n$/);
    assert.ok(m, n);
    assert.equal(Buffer.from(m[2], "base64").subarray(0, 4).toString("latin1"), "wOF2", n);
  }
  for (const n of files.filter((x) => !x.startsWith("font-data-"))) {
    const src = readFileSync(join(dir, n), "utf8").split("\n").filter((l) => !l.trimStart().startsWith("//")).join("\n");
    assert.doesNotMatch(src, bad, n);
  }
});
