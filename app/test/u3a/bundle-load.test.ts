// 同梱物の読み込み: 一覧は走査で作り、署名を持たない manifest.json を信頼の入力にしない(完了条件 13・27)。
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { createHash } from "node:crypto";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { loadBundle } from "../../src/main/bundle-load.ts";
import { parsePublicKey } from "../../src/main/minisign.ts";
import { newSigner } from "./minisign-signer.ts";

const V = "0.1.0-dev";
function build(skip: string[] = [], extra: Record<string, string> = {}) {
  const dir = mkdtempSync(join(tmpdir(), "u3a-bl-"));
  const s = newSigner();
  const put = (p: string, body: string, sign = true): void => { mkdirSync(dirname(join(dir, p)), { recursive: true }); writeFileSync(join(dir, p), body); if (sign) writeFileSync(join(dir, p + ".sig"), s.signFile(Buffer.from(body), p, V, "any")); };
  const base: Record<string, string> = { "manual.md": "m", "install.md": "i", "models.json": "{}", "i18n/ja/catalog.json": "{}", "tools/sitate_version.json": "{}", "rules/r.md": "r", ...extra };
  for (const [p, b] of Object.entries(base)) if (!skip.includes(p)) put(p, b);
  return { dir, s, put, keys: [parsePublicKey(s.pub)] };
}

test("bundle load: signed files load by name; a forged manifest.json is ignored and cannot add or hide files", () => {
  const b = build();
  try {
    writeFileSync(join(b.dir, "manifest.json"), JSON.stringify({ version: V, files: ["evil.md"] }));
    const r = loadBundle(b.dir, V, b.keys, "ja");
    assert.equal(r.ok, true);
    if (r.ok) { assert.equal(r.byPath("manual.md"), "m"); assert.deepEqual(r.paths("tools/"), ["tools/sitate_version.json"]); assert.ok(!r.files.some((f) => f.path === "evil.md")); }
  } finally { rmSync(b.dir, { recursive: true, force: true }); }
});

test("bundle load: an unsigned file, a tampered file and a missing required file are each refused with the fixed notice", () => {
  const a = build();
  try {
    writeFileSync(join(a.dir, "extra.md"), "unsigned");
    const r = loadBundle(a.dir, V, a.keys, "ja");
    assert.equal(r.ok, false);
    if (!r.ok) { assert.equal(r.reason, "unsigned"); assert.match(r.notice.ja, /署名を確認できない/); }
  } finally { rmSync(a.dir, { recursive: true, force: true }); }
  const b = build();
  try {
    writeFileSync(join(b.dir, "manual.md"), "tampered");
    assert.equal(loadBundle(b.dir, V, b.keys, "ja").ok, false);
  } finally { rmSync(b.dir, { recursive: true, force: true }); }
  for (const skip of ["install.md", "i18n/ja/catalog.json", "tools/sitate_version.json", "rules/r.md"]) {
    const c = build([skip]);
    try {
      const r = loadBundle(c.dir, V, c.keys, "ja");
      assert.equal(r.ok, false, skip);
    } finally { rmSync(c.dir, { recursive: true, force: true }); }
  }
});

// C2(spec 3.2.1): アダプタの実行ファイルは署名せず、署名付きの adapter.json の sha256 で照らす。宣言の無い署名なしのファイルと、sha256 の違う実行ファイルは拒否する。
const ARCH = "linux-x86_64";
const sha = (b: string): string => createHash("sha256").update(b).digest("hex");
function withAdapter(exeBody: string, declared: string) {
  const b = build();
  const dir = `${ARCH}/adapters/provision/b2`;
  const decl = JSON.stringify({ kind: "provision", name: "b2", exe: "b2", sha256: sha(declared) });
  mkdirSync(join(b.dir, dir), { recursive: true });
  writeFileSync(join(b.dir, dir, "adapter.json"), decl);
  writeFileSync(join(b.dir, dir, "adapter.json.sig"), b.s.signFile(Buffer.from(decl), `${dir}/adapter.json`, V, ARCH));
  writeFileSync(join(b.dir, dir, "b2"), exeBody); // 署名なし
  return b;
}

test("bundle load: an unsigned adapter executable is accepted only when the signed adapter.json pins its sha256", () => {
  const ok = withAdapter("EXE", "EXE");
  try {
    const r = loadBundle(ok.dir, V, ok.keys, "ja");
    assert.equal(r.ok, true);
    if (r.ok) assert.equal(r.byPath(`${ARCH}/adapters/provision/b2/b2`), "EXE");
  } finally { rmSync(ok.dir, { recursive: true, force: true }); }
  const bad = withAdapter("TAMPERED", "EXE");
  try {
    const r = loadBundle(bad.dir, V, bad.keys, "ja");
    assert.equal(r.ok, false);
    if (!r.ok) assert.equal(r.reason, "sha256");
  } finally { rmSync(bad.dir, { recursive: true, force: true }); }
  const other = withAdapter("EXE", "EXE");
  try {
    writeFileSync(join(other.dir, ARCH, "adapters", "provision", "b2", "other"), "x"); // 宣言に無い署名なしのファイル
    const r = loadBundle(other.dir, V, other.keys, "ja");
    assert.equal(r.ok, false);
    if (!r.ok) assert.equal(r.reason, "unsigned");
  } finally { rmSync(other.dir, { recursive: true, force: true }); }
});
