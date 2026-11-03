// 同梱物(手順書・道具の定義・規則ファイル・メッセージカタログ・モデルの候補の一覧・製品の説明書)の署名の検証(完了条件 13・27、GB-23、T-56)。
// 1 つでも失敗したらアプリを起動せず、カタログの外にビルド時に埋め込んだ固定文(日英各 1 文)だけを出す。
import { createHash } from "node:crypto";
import { verifyMinisign } from "./minisign.ts";
import type { Placement, TrustedKey } from "./minisign.ts";

export const BUNDLE_REFUSAL = {
  ja: "同梱ファイルの署名を確認できないため、起動しません。アプリを入れ直してください。",
  en: "The bundled files could not be verified, so the app will not start. Please reinstall the app.",
} as const;

export interface BundleFile { readonly path: string; readonly bytes: Buffer; readonly sig: string }
export type BundleResult = { readonly ok: true } | { readonly ok: false; readonly path: string; readonly reason: string; readonly notice: typeof BUNDLE_REFUSAL };

/** 制御文字(改行・タブを除く)と不可視の Unicode を含む同梱物は読み込まない。 */
const FORBIDDEN_TEXT = /[\p{Cc}\p{Cf}\p{Co}\u{E0000}-\u{E007F}]/u;
export function hasHiddenText(text: string): boolean {
  return FORBIDDEN_TEXT.test(text.replace(/[\n\r\t]/g, ""));
}

export function verifyBundle(files: readonly BundleFile[], keys: readonly TrustedKey[], version: string): BundleResult {
  for (const f of files) {
    // 核とアダプタ(`<os>-<arch>/…`)の trusted comment は arch に <os>-<arch> を持ち、ほかの同梱物と価格表は any(spec 3.2.1)。
    const expected: Placement = { version, arch: /^(linux-[a-z0-9_]+)\//.exec(f.path)?.[1] ?? "any", path: f.path };
    const r = verifyMinisign(f.bytes, f.sig, keys, expected);
    if (!r.ok) return { ok: false, path: f.path, reason: r.reason, notice: BUNDLE_REFUSAL };
    if (/\.(md|json|txt)$/.test(f.path) && hasHiddenText(f.bytes.toString("utf8"))) return { ok: false, path: f.path, reason: "hidden_text", notice: BUNDLE_REFUSAL };
  }
  return { ok: true };
}

/**
 * アダプタの実行ファイルは署名せず、署名付きの adapter.json の `sha256` で照らす(spec 3.2.1、tools/sign.sh)。
 * 検証済みの adapter.json(`<…>/adapters/<kind>/<name>/adapter.json`)の宣言から、実行ファイルの相対パス → 宣言の sha256 を返す。
 * 呼ぶ側は、files に入れる adapter.json の署名を先に verifyBundle で確かめておく。
 */
export function declaredExes(files: readonly BundleFile[]): Map<string, string> {
  const out = new Map<string, string>();
  for (const f of files) {
    const m = /^(.*\/adapters\/[^/]+\/[^/]+)\/adapter\.json$/.exec(f.path);
    if (m === null) continue;
    try {
      const d = JSON.parse(f.bytes.toString("utf8")) as { exe?: unknown; sha256?: unknown };
      if (typeof d.exe === "string" && d.exe !== "" && !d.exe.includes("/") && d.exe !== "." && d.exe !== ".." && typeof d.sha256 === "string") out.set(`${m[1]}/${d.exe}`, d.sha256);
    } catch { /* 読めない宣言は実行ファイルを宣言しない */ }
  }
  return out;
}

export const sha256Hex = (b: Buffer): string => createHash("sha256").update(b).digest("hex");
