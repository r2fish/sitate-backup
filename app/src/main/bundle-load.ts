// 署名つきの同梱物の読み込み(完了条件 13・27)。信頼の入力は「名前」と「署名」だけにする: 一覧は成果物の置き場を走査して作り、
// ビルドが書く manifest.json(署名を持たない)は信頼の入力にしない。署名の無いファイルがあれば全体を拒否し、必須の同梱物が欠けても拒否する。
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { BUNDLE_REFUSAL, verifyBundle } from "./bundle-verify.ts";
import type { BundleFile, BundleResult } from "./bundle-verify.ts";
import type { TrustedKey } from "./minisign.ts";

/** 必須の同梱物(名前で読む)。i18n は表示言語の 1 つ、tools と rules は 1 つ以上。 */
export const REQUIRED_FILES = ["manual.md", "install.md", "models.json"] as const;

const walk = (dir: string): string[] => readdirSync(dir).flatMap((f) => (statSync(join(dir, f)).isDirectory() ? walk(join(dir, f)) : [join(dir, f)]));

export type LoadedBundle =
  | { readonly ok: true; readonly files: readonly BundleFile[]; readonly byPath: (p: string) => string; readonly paths: (prefix: string) => string[] }
  | Extract<BundleResult, { ok: false }>;

const refuse = (path: string, reason: string): LoadedBundle => ({ ok: false, path, reason, notice: BUNDLE_REFUSAL });

export function loadBundle(dir: string, version: string, keys: readonly TrustedKey[], locale: string): LoadedBundle {
  const all = walk(dir).map((f) => relative(dir, f));
  const targets = all.filter((p) => !p.endsWith(".sig") && p !== "manifest.json");
  const unsigned = targets.find((p) => !all.includes(p + ".sig"));
  if (unsigned !== undefined) return refuse(unsigned, "unsigned");
  const files: BundleFile[] = targets.map((p) => ({ path: p, bytes: readFileSync(join(dir, p)), sig: readFileSync(join(dir, p + ".sig"), "utf8") }));
  const verified = verifyBundle(files, keys, version);
  if (!verified.ok) return verified;
  const needed = [...REQUIRED_FILES, `i18n/${locale}/catalog.json`];
  const missing = needed.find((p) => !targets.includes(p));
  if (missing !== undefined) return refuse(missing, "missing");
  if (!targets.some((p) => /^tools\/.+\.json$/.test(p))) return refuse("tools/", "missing");
  if (!targets.some((p) => p.startsWith("rules/"))) return refuse("rules/", "missing");
  return { ok: true, files, byPath: (p) => files.find((f) => f.path === p)!.bytes.toString("utf8"), paths: (prefix) => targets.filter((p) => p.startsWith(prefix)).sort() };
}
