// 偽の LLM が作る定義の対象部分: 元サーバーの調査(`source survey` の結果)から決める。変種(base / lo / nocluster)ごとの固定の値は持たない。
// 規則(台本 A2 の Q4 の答えに合わせる):
//   - ファイルの対象は /srv/shopapp(人が言ったパス。実値のまま渡してよい)。
//   - DB の対象: 最も小さいポートのクラスタ(主のクラスタ)の DB すべて。ラージオブジェクトを持つ DB も対象にする(核が source check の uncovered に large_object として残す。対象から外すと db の候補が残り手順 8 が終わらない)。
//   - 主以外のクラスタ(nocluster の 5433)は、Q4 で人が断ったものとして `source.declined` の postgresql_cluster に書く。
// 調査の結果の DB 名は参照名(pr:…)で来る。そのまま渡せば主プロセスが実値に戻す(path-ref)。
export interface Survey {
  readonly databases: readonly { name: string; port: number; large_objects: boolean }[];
  readonly clusters: readonly { port: number }[];
}

const SURVEY_OPEN = '<sitate-data command="source survey">';
const SURVEY_CLOSE = "</sitate-data>";

const textOf = (c: unknown): string => {
  if (typeof c === "string") return c;
  if (Array.isArray(c)) return c.map((b) => (b as { text?: unknown }).text).filter((t): t is string => typeof t === "string").join("\n");
  return "";
};

/** 会話の履歴の道具の結果から、最後の `source survey` の結果を取り出す。無ければ undefined。 */
export function surveyFrom(messages: readonly unknown[]): Survey | undefined {
  let found: Survey | undefined;
  for (const m of messages) {
    const content = (m as { content?: unknown }).content;
    if (!Array.isArray(content)) continue;
    for (const b of content as { type?: string; content?: unknown }[]) {
      if (b.type !== "tool_result") continue;
      const t = textOf(b.content);
      const i = t.indexOf(SURVEY_OPEN);
      const j = t.indexOf(SURVEY_CLOSE, i);
      if (i < 0 || j < 0) continue;
      try {
        const env = JSON.parse(t.slice(i + SURVEY_OPEN.length, j)) as { ok?: boolean; result?: Partial<Survey> };
        if (env.ok === true && env.result?.databases !== undefined && env.result.clusters !== undefined) found = { databases: env.result.databases, clusters: env.result.clusters };
      } catch { /* 読めない結果は無いものとして扱う */ }
    }
  }
  return found;
}

/** 手順 8 の `source check` の足せる候補(actionable)のうち、人に問わず足す種別(mount・db)。 */
export interface Candidate { readonly kind: string; readonly path: string; readonly port?: number | null }

/** 履歴の `source check` の結果のうち、最初に actionable が空でないもの。mount と db の候補だけを返す(無ければ空)。 */
export function candidatesFrom(messages: readonly unknown[]): Candidate[] {
  for (const m of messages) {
    const content = (m as { content?: unknown }).content;
    if (!Array.isArray(content)) continue;
    for (const b of content as { type?: string; content?: unknown }[]) {
      if (b.type !== "tool_result") continue;
      const t = textOf(b.content);
      const i = t.indexOf('<sitate-data command="source check">');
      const j = t.indexOf(SURVEY_CLOSE, i);
      if (i < 0 || j < 0) continue;
      try {
        const env = JSON.parse(t.slice(t.indexOf("\n", i), j)) as { result?: { actionable?: Candidate[] } };
        const found = (env.result?.actionable ?? []).filter((c) => c.kind === "mount" || c.kind === "db");
        if (found.length > 0) return found;
      } catch { /* 読めない結果は無いものとして扱う */ }
    }
  }
  return [];
}

/** 筋「/srv を断る」(D-0189): `/srv` の下を選ばず、`source.declined` に `mount: /srv` を書く(spec 3.8 の Q4 の補足の「`parent` のマウントの下で対象を選ばなかったとき」)。 */
export const DECLINED_SRV_MOUNT = "/srv";

export function sourceTargets(survey: Survey | undefined, added: readonly Candidate[] = [], declineSrv = false): { targets: Record<string, unknown>[]; declined: Record<string, unknown>[] } {
  const targets: Record<string, unknown>[] = declineSrv ? [] : [{ id: "t1", files: "/srv/shopapp" }];
  const declined: Record<string, unknown>[] = declineSrv ? [{ id: "d1", mount: DECLINED_SRV_MOUNT }] : [];
  const addExtra = (): void => added.forEach((c) => targets.push({ id: `t${targets.length + 1}`, ...(c.kind === "mount" ? { files: c.path } : { postgresql: c.path, ...(c.port != null ? { port: c.port } : {}) }) }));
  if (survey === undefined) { addExtra(); return { targets, declined }; }
  const ports = survey.clusters.map((c) => c.port).sort((x, y) => x - y);
  const primary = ports[0];
  survey.databases.filter((d) => d.port === primary).forEach((d, i) => targets.push({ id: `t${targets.length + 1}`, postgresql: d.name, port: d.port }));
  ports.slice(1).forEach((p, i) => declined.push({ id: `d${declined.length + 1}`, postgresql_cluster: p }));
  addExtra(); // 手順 8 で足す分(手順 4 の調査の後に現れた候補)
  return { targets, declined };
}
