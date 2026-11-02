// 元サーバーの調査の結果の区画(spec 3.7 の状態と報告の表示、完了条件 33)。`source survey` の結果のディレクトリ・マウント・DB を、
// 実名(パスと DB 名)・種別・大きさ・v1 で取れるかで並べる。実名は主プロセスが核の結果から直接作り、この面にだけ描く。モデルへは送らない(送る欄の表は path_ref)。
import { sanitizeText } from "./shape-envelope.ts";

export interface SurveyRow { readonly name: string; readonly kind: string; readonly size: string; readonly v1: string }
export interface SurveyView { readonly heading: string; readonly rows: readonly SurveyRow[] }

type T = (key: string) => string;
const isRec = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const NAME_MAX = 4096;
const UNITS = ["B", "KiB", "MiB", "GiB", "TiB"] as const;

/** 大きさを単位付きで(2 進接頭辞。数え切れなかったときは unknown の文)。 */
export function sizeText(bytes: unknown, unknown: string, locale = "ja"): string {
  if (typeof bytes !== "number" || !Number.isFinite(bytes) || bytes < 0) return unknown;
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < UNITS.length - 1) { v /= 1024; i++; }
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: i === 0 ? 0 : 1 }).format(v)} ${UNITS[i]}`;
}

/** `source survey` の結果(核の封筒の result)から区画を作る。形が合わなければ null。 */
export function buildSurveyView(result: unknown, t: T, locale = "ja"): SurveyView | null {
  if (!isRec(result)) return null;
  const arr = (k: string): Record<string, unknown>[] => (Array.isArray(result[k]) ? (result[k] as unknown[]).filter(isRec) : []);
  const name = (v: unknown): string => (typeof v === "string" ? sanitizeText(v, NAME_MAX) : "");
  const yn = (ok: boolean): string => t(ok ? "status.survey.v1.yes" : "status.survey.v1.no");
  const size = (b: unknown): string => sizeText(b, t("status.survey.size.unknown"), locale);
  const rows: SurveyRow[] = [
    ...arr("dirs").map((d) => ({ name: name(d.path), kind: t("status.survey.kind.dir"), size: size(d.bytes), v1: yn(d.supported === true && d.db_data !== true) })),
    ...arr("mounts").map((m) => ({ name: name(m.path), kind: t("status.survey.kind.mount"), size: size(m.bytes), v1: yn(m.class === "candidate") })),
    ...arr("databases").map((d) => ({ name: name(d.name), kind: t("status.survey.kind.database"), size: size(d.bytes), v1: yn(d.large_objects !== true) })),
  ];
  return { heading: t("status.survey.result.heading"), rows };
}
