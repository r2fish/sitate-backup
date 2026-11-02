// 状態と報告の表示(spec 3.7、完了条件 10・23・32・35)。`status` の結果を読み直すたびに、前に読んだ値との差から「AI に見立てを聞く」を置く行を決め、
// 無音を判定し、健常性チェックの件数の固定文、アプリの情報の区画(鍵の種別・使用中のモデル・表示言語)を描く。モデルは呼ばない: 押された操作の固定文を返すだけ。
// ファイル名・パスを含む欄はこの面に直接描き、モデルを通さない。届かないときは最後に読めた値に「古い」印を付けて残す。
import { SILENCE_BACKUP_DAYS } from "../constants.ts";
import type { Translate } from "./approval-card.ts";

export const RESTORE_TEST_OVERDUE_DAYS = 35;
const DAY_MS = 24 * 60 * 60 * 1000;

export type ChangeKind = "warning_new" | "hold_active" | "last_ok_false" | "silence" | "uncovered_increase";
const KIND_KEYS: Readonly<Record<ChangeKind, string>> = {
  warning_new: "status.ask_ai.input.warning_new",
  hold_active: "status.ask_ai.input.hold_active",
  last_ok_false: "status.ask_ai.input.last_ok_false",
  silence: "status.ask_ai.input.silence",
  uncovered_increase: "status.ask_ai.input.uncovered_increase",
};

interface StatusResult {
  last_backup?: { at?: string | null; ok?: boolean; checks_count?: number } | null;
  last_restore_test?: { at?: string | null; ok?: boolean; days_since?: number | null } | null;
  hold?: { active?: boolean } | null;
  warnings?: { code?: string }[];
  uncovered_count?: number;
  pending_count?: number;
  restore_sessions?: unknown[];
}

export interface StatusRow { readonly label: string; readonly value: string }
export interface StatusViewModel {
  readonly rows: readonly StatusRow[];
  readonly askAi: readonly { readonly kind: ChangeKind; readonly label: string }[];
  readonly appInfo: readonly StatusRow[];
}

export interface StatusViewDeps {
  readonly t: Translate;
  readonly now: () => Date;
  /** エージェントが動いているか。動かない間は「AI に見立てを聞く」を出さない(状態の表示は P7 のまま)。 */
  readonly agentRunning: () => boolean;
  /** OS 通知(無音)。1 日に 1 回まで。 */
  readonly notify: (text: string) => void;
}

const isRec = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

export class StatusView {
  readonly #d: StatusViewDeps;
  #last: StatusResult | null = null;
  #readAt: Date | null = null;
  #stale = false;
  #changes = new Set<ChangeKind>();
  #silent = false;
  #lastSilenceNotice = 0;
  #model: { name: string; first: boolean } | null = null;
  #keyKinds = { admin: "ssh-ed25519 (software)", approval: "ssh-ed25519 (software)" };
  #locale = "ja";

  constructor(deps: StatusViewDeps) {
    this.#d = deps;
  }

  setModel(name: string | null, first: boolean): void {
    this.#model = name === null ? null : { name, first };
  }

  setLocale(locale: string): void {
    this.#locale = locale;
  }

  /** `status` の結果の封筒を読み込む。null はバックアップサーバーに届かなかった(最後の値を古い印つきで残す)。 */
  ingest(envelope: Record<string, unknown> | null): void {
    if (envelope === null || envelope.ok !== true || !isRec(envelope.result)) {
      this.#stale = this.#last !== null || envelope === null;
      return;
    }
    const cur = envelope.result as StatusResult;
    const prev = this.#last;
    this.#last = cur;
    this.#stale = false;
    this.#readAt = this.#d.now();
    this.#changes = prev === null ? new Set() : changesBetween(prev, cur, this.#silent, this.#isSilent(cur));
    const silent = this.#isSilent(cur);
    if (silent && (!this.#silent || this.#d.now().getTime() - this.#lastSilenceNotice >= DAY_MS)) {
      this.#d.notify(this.#d.t("status.ask_ai.input.silence"));
      this.#lastSilenceNotice = this.#d.now().getTime();
    }
    this.#silent = silent;
  }

  /** 無音: last_backup.at が SILENCE_BACKUP_DAYS 日より前、または last_restore_test.days_since が閾値を超えた。 */
  #isSilent(r: StatusResult): boolean {
    const at = r.last_backup?.at;
    const old = typeof at === "string" && this.#d.now().getTime() - Date.parse(at) > SILENCE_BACKUP_DAYS * DAY_MS;
    const d = r.last_restore_test?.days_since;
    return old || (typeof d === "number" && d > RESTORE_TEST_OVERDUE_DAYS);
  }

  get silent(): boolean {
    return this.#silent;
  }

  view(): StatusViewModel {
    const t = this.#d.t;
    const r = this.#last;
    const mark = (ok: boolean | undefined): string => (ok === undefined ? "" : ok ? "✓" : "✗");
    const date = (iso: string | null | undefined): string => (typeof iso === "string" ? new Intl.DateTimeFormat(this.#locale, { dateStyle: "short", timeStyle: "short" }).format(new Date(iso)) : "—");
    const stale = this.#stale ? ` ${t("status.stale_mark")}` : "";
    const rows: StatusRow[] = [];
    if (this.#stale) rows.push({ label: t("status.unreachable.fixed"), value: "" });
    if (r !== null) {
      rows.push({ label: t("status.row.last_backup"), value: `${date(r.last_backup?.at)} ${mark(r.last_backup?.ok)}${stale}`.trim() });
      // 健常性チェックの件数が 0 のときだけ固定文(判定は checks_count = 0 だけ。版では決めない)。
      if (r.last_backup?.checks_count === 0) rows.push({ label: t("status.row.checks_count_zero"), value: "" });
      rows.push({ label: t("status.row.last_restore_test"), value: `${date(r.last_restore_test?.at)} ${mark(r.last_restore_test?.ok)}${stale}`.trim() });
      rows.push({ label: t("status.row.hold"), value: `${r.hold?.active === true ? "●" : "—"}${stale}` });
      if (typeof r.uncovered_count === "number") rows.push({ label: t("status.row.uncovered"), value: `${r.uncovered_count}${stale}` });
      if (typeof r.pending_count === "number") rows.push({ label: t("status.row.pending_changes"), value: `${r.pending_count}${stale}` });
      if (Array.isArray(r.restore_sessions)) rows.push({ label: t("status.row.restore_sessions"), value: `${r.restore_sessions.length}${stale}` });
    } else if (!this.#stale) {
      rows.push({ label: t("status.row.last_backup"), value: "—" });
    }
    // 変化があった行に操作を置く。エージェントが動かない間は出さない。
    const askAi = this.#d.agentRunning() ? [...this.#changes].map((kind) => ({ kind, label: t("status.ask_ai.operation") })) : [];
    const appInfo: StatusRow[] = [
      { label: t("status.app_info.heading"), value: "" },
      { label: "SSH", value: this.#keyKinds.admin },
      { label: "approval", value: this.#keyKinds.approval },
      this.#model === null ? { label: t("status.llm.model_missing"), value: "" } : { label: t("status.model_in_use", { model: this.#model.name }), value: this.#model.first ? "" : t("status.model_not_first_mark") },
    ];
    return { rows, askAi, appInfo };
  }

  /** 押された操作の固定文(値を含まない)。出ていない種類・エージェントが動かないときは null。モデルの呼び出しはここでは起きない。 */
  press(kind: string): string | null {
    if (!this.#d.agentRunning() || !this.#changes.has(kind as ChangeKind)) return null;
    this.#changes.delete(kind as ChangeKind);
    return this.#d.t(KIND_KEYS[kind as ChangeKind]);
  }
}

function changesBetween(prev: StatusResult, cur: StatusResult, wasSilent: boolean, isSilent: boolean): Set<ChangeKind> {
  const out = new Set<ChangeKind>();
  const codes = new Set((prev.warnings ?? []).map((w) => w.code));
  if ((cur.warnings ?? []).some((w) => typeof w.code === "string" && !codes.has(w.code))) out.add("warning_new");
  if (cur.hold?.active === true && prev.hold?.active !== true) out.add("hold_active");
  if ((cur.last_backup?.ok === false && prev.last_backup?.ok !== false) || (cur.last_restore_test?.ok === false && prev.last_restore_test?.ok !== false)) out.add("last_ok_false");
  if (isSilent && !wasSilent) out.add("silence");
  if (typeof cur.uncovered_count === "number" && cur.uncovered_count > (prev.uncovered_count ?? 0)) out.add("uncovered_increase");
  return out;
}
