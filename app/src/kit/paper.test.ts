import { test } from "node:test";
import assert from "node:assert/strict";
import { renderKit } from "./render.ts";
import { PHRASE, realFields } from "./paper-fixture.ts";
import type { KitRenderSuccess } from "./types.ts";

// 紙の中身の試験(spec 4.7 の字面)。画素は比べず、HTML の行と札を照らす。

function ok(f = realFields()): KitRenderSuccess {
  const r = renderKit({ fields: f, phrase: PHRASE, lang: "ja" });
  assert.ok(r.ok, JSON.stringify(r));
  return r;
}

const unesc = (s: string) => s.replaceAll("&quot;", '"').replaceAll("&lt;", "<").replaceAll("&gt;", ">").replaceAll("&amp;", "&");

const WIDE = /[\u2e80-\uffef]/;

/** 欄の中の文字。コマンドの行は空白で連ね(語の途中で折った行 » は間を空けずに続ける)、文の行は全角の字の境目で折れたものを間を空けずに続ける。 */
export function sectionText(html: string, kind: string, step?: string): string {
  const attr = step === undefined ? "" : `[^>]*data-step="${step}"`;
  const m = html.match(new RegExp(`<section class="blk" data-kind="${kind}"${attr}[^>]*>(.*?)</section>`, "s"));
  assert.ok(m, `${kind} ${step ?? ""}`);
  let out = "";
  for (const r of m[1].matchAll(/<div class="r (?:l|m)">(.*?)<\/div>/gs)) {
    const mono = r[0].startsWith('<div class="r m">') || r[1].includes('<span class="m">');
    const cont = r[1].endsWith('<i class="k">»</i>');
    const t = unesc(r[1].replace('<i class="k">»</i>', "").replace(/<[^>]*>/g, ""));
    const glue = out === "" || cont || (!mono && (WIDE.test(out.at(-1) ?? "") || WIDE.test(t[0] ?? ""))) ? "" : " ";
    out += glue + t;
    if (cont) out += "\u0001";
  }
  return out.replaceAll("\u0001", "").replace(/ +/g, " ").trim();
}

/** 文の照合は空白を無視する(折り返しで落ちる空白があるため)。 */
const has = (t: string, s: string) => t.replaceAll(" ", "").includes(s.replaceAll(" ", ""));

const CONN = "--endpoint https://s3.us-west-004.example.invalid --bucket bkt-main --prefix pfx/office/";
const REF = "--key-ref storage-ref --repo-id repo-77";
const PIPE = `printf '%s\\n' "$X" | sudo -u sitate sitate`;

test("[u3b-impl] step 3 prints the issue-read-key command with \"$RUN\" in double quotes, the RUN generator and the admin JSON form", () => {
  const { html } = ok().printable;
  const t = sectionText(html, "restore_step", "3");
  assert.ok(t.includes(`${PIPE} storage issue-read-key ${CONN} --adapter b2 --admin-ref admin ${REF} --run-id "$RUN" --secrets-stdin`), t);
  assert.ok(t.includes(`RUN=r-$(date -u +%Y%m%dT%H%M%SZ)-$(head -c 2 /dev/urandom | od -An -tx1 | tr -d ' \\n')`), t);
  assert.ok(t.includes('{"admin": <事業者の管理用鍵の値>}'));
  assert.ok(has(t, '--run-id "$RUN" は (3) にだけ付ける') && !has(t, "$RUN は (3) にだけ付け、ほかの手順には付けない"));
  assert.ok(!html.includes("'$RUN"), "single-quoted $RUN");
  assert.ok(!html.includes("&#"), "no numeric entities");
});

test("[u3b-impl] the secret-stdin lines (read -rs X, printf pipe, unset X) are printed", () => {
  const t = sectionText(ok().printable.html, "restore_step", "3");
  for (const s of ["read -rs X", `printf '%s\\n' "$X" | sudo -u sitate sitate …`, "unset X"]) assert.ok(t.includes(s), s);
});

test("[u3b-impl] steps 4 to 6 print their commands (argv from fields, --to with \"$RUN\", the empty directory made first)", () => {
  const { html } = ok().printable;
  assert.ok(sectionText(html, "restore_step", "4").includes(`${PIPE} list ${CONN} --keyring kr-5 ${REF} --phrase-stdin`));
  const five = sectionText(html, "restore_step", "5");
  assert.ok(five.includes(`sudo -u sitate install -d -m 0700 "/var/lib/sitate/restore/$RUN/to"`), five);
  assert.ok(five.includes(`${PIPE} restore ${CONN} --keyring kr-5 ${REF} --phrase-stdin --backup latest --to "/var/lib/sitate/restore/$RUN/to"`), five);
  assert.ok(sectionText(html, "restore_step", "6").includes(`${PIPE} storage revoke-read-key ${CONN} --adapter b2 --admin-ref admin --key-ref storage-ref --secrets-stdin`));
});

test("[u3b-impl] a restore argv without --to still gets --to (the paper never lacks it)", () => {
  const f = realFields();
  const steps = f.steps.map((s) => (s.id === "5" ? { ...s, argv: s.argv.slice(0, -2) } : s));
  const five = sectionText(ok({ ...f, steps }).printable.html, "restore_step", "5");
  assert.ok(five.includes('--backup latest --to "/var/lib/sitate/restore/$RUN/to"'), five);
});

test("[u3b-impl] cases the core does not put in steps (no app, no adapter, journal verify) are built from fields and always printed", () => {
  const { html, layout } = ok().printable;
  assert.deepEqual(layout.blocks.filter((b) => b.kind === "restore_step").map((b) => b.step_id), ["1", "2", "2-no-app", "3", "3-no-adapter", "4-journal", "4", "5", "6"]);
  const na = sectionText(html, "restore_step", "2-no-app");
  for (const s of ["useradd --system sitate", "install -d -o sitate -g sitate -m 0700 /var/lib/sitate", "runuser -u sitate --"]) assert.ok(na.includes(s), s);
  const ad = sectionText(html, "restore_step", "3-no-adapter");
  assert.ok(ad.includes(`${PIPE} secrets set storage-ref --role read --stdin`), ad);
  assert.ok(ad.includes('{"key_id": "<鍵の ID>", "secret": "<鍵の秘密>"}'));
  const j = sectionText(html, "restore_step", "4-journal");
  assert.ok(j.includes(`${PIPE} journal verify ${CONN} --keyring kr-5 ${REF} --phrase-stdin`), j);
});

test("[u3b-impl] the spec's fixed sentences are on the paper: version choice, trusted comment, anchor limit, two cases of (6)", () => {
  const { html } = ok().printable;
  const two = sectionText(html, "restore_step", "2");
  for (const s of ["SHA-256 がこの紙と合う版を使う", "署名がこの紙の公開鍵で合う最も新しい版", "version・arch・path", "合わなければその版を使わない", "保管先の bin/ と同じ照合を行う", "bundle-<version>.tar.gz"]) assert.ok(has(two, s), s);
  const four = sectionText(html, "restore_step", "4");
  for (const s of ["それより後に積まれた偽の世代は見つけない", "全対象 ok か", "stored_at", "--anchor <ファイル>", "anchor.rollback_suspected"]) assert.ok(has(four, s), s);
  const six = sectionText(html, "restore_step", "6");
  assert.ok(has(six, "アプリも配布元も無く、事業者の管理用鍵も手元に無いときは、(2) の鍵をコンソールで失効させる"), six);
  assert.ok(has(six, "保管先準備アダプタの無い事業者は、(3) で入れた鍵をコンソールで失効させる"), six);
  assert.ok(!has(six, "無いとき、保管先準備アダプタの無い事業者は"), "the two cases are not one");
  assert.ok(has(sectionText(html, "restore_step", "3"), "REPO_ID_MISMATCH"));
  assert.ok(has(sectionText(html, "restore_step", "1"), "root の権限を持つ初期資格情報"));
});

test("[u3b-impl] labels sit next to their own values (hashes of each arch, bucket and prefix, repo_id and keyring_id, both signing keys)", () => {
  const f = realFields();
  const { html } = ok(f).printable;
  const p = sectionText(html, "product");
  assert.ok(p.includes(`linux-x86_64 ${"a".repeat(64)}`) && p.includes(`linux-arm64 ${"b".repeat(64)}`), p);
  assert.ok(p.includes(`SBOM SHA-256 ${"c".repeat(64)}`) && p.includes(`ソース SHA-256 ${"d".repeat(64)}`));
  const s = sectionText(html, "storage");
  assert.ok(s.includes("バケット bkt-main プレフィクス pfx/office/"), s);
  assert.ok(s.includes("repo_id repo-77 keyring_id kr-5 key_ref storage-ref"), s);
  assert.ok(s.includes("エンドポイント https://s3.us-west-004.example.invalid"));
  const k = sectionText(html, "signing_keys");
  for (const key of f.signing_keys) assert.ok(k.includes(`公開鍵 ${key.public_key}`) && k.includes(`指紋 ${key.fp}`), key.fp);
});

test("[u3b-impl] the html names no outside resource: every url() is a data URI, no @import, no src/href attribute, no outside host in tags or css", () => {
  const { html } = ok().printable;
  const noData = html.replace(/data:[a-z/0-9+.-]+;base64,[A-Za-z0-9+/=]+/g, "DATA");
  const style = noData.match(/<style>(.*?)<\/style>/s)?.[1] ?? "";
  assert.ok(style.length > 100);
  for (const u of style.matchAll(/url\(([^)]*)\)/g)) assert.equal(u[1], "DATA", u[0]);
  assert.doesNotMatch(style, /@import|\/\/|https?:/i);
  for (const tag of noData.matchAll(/<[^>!]+>/g)) {
    assert.doesNotMatch(tag[0], /\b(src|href|srcset|action|poster|data)=|https?:|\/\/(?!\s)/i, tag[0]);
    assert.doesNotMatch(tag[0], /\burl\(/i);
  }
  assert.doesNotMatch(noData, /<(script|img|link|iframe|object|embed|video|audio|form|base)\b/i);
  assert.match(html, /<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; font-src data:">/);
});

test("[u3b-impl] values are html-escaped (a value with markup characters stays text)", () => {
  const f = realFields({ bucket: 'b"><img src=//evil.invalid/x>&' });
  const { html } = ok(f).printable;
  assert.doesNotMatch(html, /<img/i);
  assert.ok(sectionText(html, "storage").includes('b"><img src=//evil.invalid/x>&'));
});

test("[u3b-impl] the sheet is A4 portrait, the pages are 297mm high, the phrase grid is shown, and the heading and mono fonts fall back to the bundled Japanese font", () => {
  const { html } = ok().printable;
  assert.ok(html.includes("@page{size:A4 portrait;margin:0}"));
  assert.match(html, /\.page\{position:relative;width:210mm;height:297mm;/);
  assert.match(html, /\.g\{display:grid;/);
  assert.match(html, /\.m\{font-family:[^;]*"SitateJP"[^;]*monospace;/);
  assert.match(html, /\.c\{font-family:[^;]*"SitateJP"/);
  assert.match(html, /body\{font-family:"SitateJP"/);
});

test("[u3b-impl] a value and the next label are never run together, and a step body does not repeat the number of its heading", () => {
  const f = realFields();
  const { html } = ok(f).printable;
  const title = sectionText(html, "title");
  assert.ok(title.includes(`${f.kit_version} 発行日 ${f.issued_on}`), title);
  assert.ok(sectionText(html, "product").includes(`核の版 ${f.core_version} 形式の版 ${f.format_version}`));
  assert.ok(sectionText(html, "storage").includes("事業者 b2 国 (不明)"));
  assert.ok(sectionText(html, "storage").includes("バケット bkt-main プレフィクス pfx/office/"));
  // 値と札の間は 2 つ以上の空白(HTML の生の行で見る)
  assert.match(html, /バケット bkt-main {3}プレフィクス pfx\/office\//);
  assert.match(html, new RegExp(`${f.kit_version} {4}発行日 `));
  for (const [step, head] of [["1", "(1)"], ["2", "(2)"], ["2-no-app", "(2) アプリが無いとき"], ["3", "(3)"], ["3-no-adapter", "(3) 保管先準備アダプタの無い事業者と、アプリも配布元も無いとき"], ["4-journal", "(4) の記録"], ["4", "(4)"], ["5", "(5)"], ["6", "(6)"]] as const) {
    const t = sectionText(html, "restore_step", step);
    assert.ok(t.startsWith(`復元の手順 ${head}`), t.slice(0, 40));
    const body = t.slice(`復元の手順 ${head}`.length).trimStart();
    assert.doesNotMatch(body, /^\(\d\)/, `${step}: ${body.slice(0, 30)}`);
  }
});
