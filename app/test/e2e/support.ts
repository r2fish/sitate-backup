// ドライバの入口の補助: 規則ファイルの読み込み、確認語の取得、環境の操作、保管先準備アダプタの代役の取り付けの待ち。
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readdirSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { readJson } from "./script.ts";

const DOUBLES_WAIT_S = 120;

export function loadCatalog(repo: string, lang: string): Record<string, string> {
  return readJson<Record<string, string>>(join(repo, "app", "src", "i18n", lang, "catalog.json"));
}

/** 試験ビルドの同梱物の置き場(`make sign-test` の成果物)。 */
export function assetsDir(repo: string): string {
  const base = join(repo, "dist", "test", "app-assets");
  const v = readdirSync(base).sort().at(-1);
  if (v === undefined) throw new Error("dist/test/app-assets が空。先に make build-test sign-test");
  return join(base, v);
}

/** 規則ファイル(署名済みの同梱物)の「問いの終わり方」の一覧。 */
export function questionEndings(repo: string): string[] {
  const md = readFileSync(join(assetsDir(repo), "rules", "quote-check-forms.md"), "utf8");
  const m = /```json\n([\s\S]*?)\n```/.exec(md);
  const j = JSON.parse(m![1]!) as { question_endings: string[] };
  return j.question_endings;
}

const sha256 = (path: string): string => createHash("sha256").update(readFileSync(path)).digest("hex");

/** 記録の頭: 変種・台本の版・アプリの試験ビルドの識別子・手順書の写しの sha256(核の `sitate version` の結果は道具の呼び出しの記録にある)。 */
export function headerFields(repo: string): Record<string, unknown> {
  const assets = assetsDir(repo);
  const orig = join(repo, "dist", "test", "install.md.orig.sha256");
  return {
    app_build: { version: readJson<{ version: string }>(join(repo, "app", "package.json")).version, main_js_sha256: sha256(join(repo, "app", "dist", "main.js")) },
    install_md_copy_sha256: sha256(join(assets, "install.md")),
    install_md_orig_sha256: existsSync(orig) ? readFileSync(orig, "utf8").trim().split(/\s+/)[0] : null,
  };
}

/** 通知の受け手の記録(JSON Lines)から、宛先ごとの最後の確認語。宛先が 1 つだけなら名前の違いは問わない。 */
export function hookWordFrom(recordsFile: string, recipient: string): string | null {
  if (!existsSync(recordsFile)) return null;
  const recs = readFileSync(recordsFile, "utf8").split("\n").filter((l) => l.trim() !== "").flatMap((l) => {
    try { return [JSON.parse(l) as { dest?: string; confirmation_word?: unknown }]; } catch { return []; }
  }).filter((r) => typeof r.confirmation_word === "string" && r.confirmation_word !== "");
  const own = recs.filter((r) => r.dest === recipient).at(-1);
  if (own !== undefined) return own.confirmation_word as string;
  return new Set(recs.map((r) => r.dest)).size === 1 ? (recs.at(-1)!.confirmation_word as string) : null;
}

/** 変種 add の「手順 4 の調査の後で手順 8 の前に足す」手順(accept-env.sh add-extra)。 */
export function addExtra(repo: string, name: string, kind: "mount" | "db"): void {
  const r = spawnSync("bash", [join(repo, "test/env/bin/accept-env.sh"), "add-extra", name, kind], { encoding: "utf8" });
  if (r.status !== 0) throw new Error(`add-extra ${kind} に失敗: ${(r.stderr || r.stdout).slice(-300)}`);
}

/** 保管先準備アダプタの代役(test/wiring/e2e.sh の見張りが root で置く)を、bootstrap の後に置いてもらう。 */
export async function placeDoubles(dir: string): Promise<void> {
  const need = join(dir, "need-doubles");
  const done = join(dir, "doubles-done");
  rmSync(done, { force: true });
  writeFileSync(need, "");
  for (let i = 0; i < DOUBLES_WAIT_S && !existsSync(done); i++) await new Promise((r) => setTimeout(r, 1000));
  if (!existsSync(done)) throw new Error("保管先準備アダプタの代役が置かれない");
}
