// 静的検査の道具: 入口のファイルから import / export from / 動的 import / require をたどり、依存の木(自前のソースと node_modules の全体)を集める。
// 完了条件 1・2 の静的検査(spec 6.5 A6 (15))が使う。コメントの中の例は数えない。ビルドの成果物(.mjs / .js)と TypeScript のソースの両方を読む。
import { existsSync, readFileSync, statSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, resolve } from "node:path";

export interface Graph {
  /** たどったファイル(絶対パス)。 */
  readonly files: ReadonlySet<string>;
  /** 指定子(node:fs など)→ それを import したファイルの一覧。 */
  readonly specifiers: ReadonlyMap<string, string[]>;
}

const SPEC_PATTERN = /(?:\bimport|\bexport)\s[^'"`;]{0,400}?\bfrom\s*["']([^"']+)["']|\bimport\s*["']([^"']+)["']|\bimport\(\s*["']([^"']+)["']\s*\)|\brequire\(\s*["']([^"']+)["']\s*\)/g;
const EXTENSIONS = ["", ".ts", ".mjs", ".js", ".cjs", "/index.ts", "/index.mjs", "/index.js"];

export function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:"'`\\])\/\/.*$/gm, "$1");
}

export function specifiersOf(src: string): string[] {
  const out = new Set<string>();
  for (const m of stripComments(src).matchAll(SPEC_PATTERN)) out.add((m[1] ?? m[2] ?? m[3] ?? m[4])!);
  return [...out];
}

function resolveRelative(from: string, spec: string): string | null {
  const base = resolve(dirname(from), spec);
  for (const ext of EXTENSIONS) {
    const c = base + ext;
    if (existsSync(c) && statSync(c).isFile()) return c;
  }
  return null;
}

/** パッケージの指定子を、importer から見たときの実ファイルに解決する(require の条件。SDK は import と require の両方の構成が同じ import の木を持つ)。解決できなければ null。 */
function resolvePackage(importer: string, spec: string): string | null {
  try {
    return createRequire(importer).resolve(spec);
  } catch {
    return null;
  }
}

export function collectGraph(entries: readonly string[]): Graph {
  const files = new Set<string>();
  const specifiers = new Map<string, string[]>();
  const queue = [...entries];
  while (queue.length > 0) {
    const file = queue.pop()!;
    if (files.has(file) || !existsSync(file)) continue;
    files.add(file);
    if (!/\.(ts|mjs|js|cjs)$/.test(file)) continue;
    for (const spec of specifiersOf(readFileSync(file, "utf8"))) {
      const list = specifiers.get(spec) ?? [];
      list.push(file);
      specifiers.set(spec, list);
      if (spec.startsWith("node:")) continue;
      const next = spec.startsWith(".") ? resolveRelative(file, spec) : resolvePackage(file, spec);
      if (next !== null) queue.push(next);
    }
  }
  return { files, specifiers };
}
