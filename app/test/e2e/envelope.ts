// 道具の呼び出しの結果(封筒の区画 `<sitate-data …>JSON</sitate-data>`)の読み取り。ドライバの機械判定(H5、status の値)に使う。
export interface Envelope {
  readonly ok: boolean | null;
  readonly code: string | null;
  readonly detailReason: string | null;
  readonly result: Record<string, unknown> | null;
}

export function parseEnvelope(text: string): Envelope {
  const m = /<sitate-data[^>]*>\n([\s\S]*?)\n<\/sitate-data>/.exec(text);
  const none: Envelope = { ok: null, code: null, detailReason: null, result: null };
  if (m === null) return none;
  try {
    const j = JSON.parse(m[1]!) as { ok?: unknown; error?: { code?: unknown; detail?: { reason?: unknown } } | null; result?: unknown };
    const result = typeof j.result === "object" && j.result !== null ? (j.result as Record<string, unknown>) : null;
    return { ok: typeof j.ok === "boolean" ? j.ok : null, code: typeof j.error?.code === "string" ? j.error.code : null, detailReason: typeof j.error?.detail?.reason === "string" ? j.error.detail.reason : null, result };
  } catch { return none; }
}

/** H5(無効): `source survey` の `/result/prereq_missing` が空でない、または `SOURCE_SETUP_FAILED` の `detail.reason` が `tool_missing`。 */
export function isH5(tool: string, e: Envelope): boolean {
  if (tool === "sitate_source_survey") {
    const pm = e.result?.prereq_missing;
    if (Array.isArray(pm) && pm.length > 0) return true;
  }
  return e.code === "SOURCE_SETUP_FAILED" && e.detailReason === "tool_missing";
}

export interface StatusJudgement { readonly lastBackupOk: boolean; readonly lastRestoreTestOk: boolean; readonly proofConfirmed: boolean }

/** A2-4: `/result/last_backup/ok`・`/result/last_restore_test/ok`・`/result/notify/proof_confirmed` がすべて true。 */
export function judgeStatus(e: Envelope): StatusJudgement {
  const r = e.result ?? {};
  const ok = (k: string): boolean => (r[k] as { ok?: unknown } | undefined)?.ok === true;
  return { lastBackupOk: ok("last_backup"), lastRestoreTestOk: ok("last_restore_test"), proofConfirmed: (r.notify as { proof_confirmed?: unknown } | undefined)?.proof_confirmed === true };
}
