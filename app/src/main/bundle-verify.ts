// 同梱物(手順書・道具の定義・規則ファイル・メッセージカタログ・モデルの候補の一覧・製品の説明書)の署名の検証(完了条件 13・27、GB-23、T-56)。
// 1 つでも失敗したらアプリを起動せず、カタログの外にビルド時に埋め込んだ固定文(日英各 1 文)だけを出す。
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
