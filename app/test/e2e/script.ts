// 台本(test/acceptance/scripts/A2/script.json)の型と読み込み、問いの見分け、人の役の値(環境変数・環境の接続情報)の組み立て。
// 人の役の値(SITATE_ROLE_*)はここで受け、ログにも記録にも出さない(記録の側にも秘密の検査がある)。名前に ANTHROPIC_API_KEY を使わない。
import { readFileSync } from "node:fs";

export interface QuestionScript {
  readonly match: readonly string[];
  readonly answer?: string;
  readonly answers?: Readonly<Record<string, string>>;
  readonly accept_proposal?: string;
}
export interface A2Script {
  readonly version: string;
  readonly unscripted_question_answer: string;
  readonly continue_input: string;
  readonly turn_limit_input: string;
  readonly questions: Readonly<Record<"Q1" | "Q2" | "Q3" | "Q4", QuestionScript>>;
}
export interface InjectionScript {
  readonly version: string;
  readonly chat: readonly { id: string; text: string }[];
  readonly source_file_names: readonly { id: string; name: string }[];
  readonly notification_bodies: readonly { id: string; text: string }[];
}

export type QId = "Q1" | "Q2" | "Q3" | "Q4";
export const Q_ORDER: readonly QId[] = ["Q1", "Q2", "Q3", "Q4"];

export const readJson = <T>(path: string): T => JSON.parse(readFileSync(path, "utf8")) as T;

/** 人の役の値と、環境の接続先。値は記録・ログへ出さない(secrets() が検索用の一覧を返す)。 */
export interface Answers {
  readonly llmKey: string;
  readonly adminKeyPem: string;
  /** 1 行目にキー ID、2 行目に鍵(保管先の事業者の管理用鍵。秘密の入力ボックス)。 */
  readonly storageAdmin: string;
  readonly storageEndpoint: string;
  readonly bucket: string;
  readonly hookUrl: string;
  readonly adminUser: string;
  readonly backupHost: string;
  readonly sourceHost: string;
  readonly destHost: string;
  readonly destPort: number;
  readonly hookRecordsFile: string;
  readonly containers: Readonly<Record<"backup" | "source" | "dest", string>>;
}

export const HOOK_NAME = "ops-hook";

/** 記録・ログに出してはならない値(そのままの文字列。検査は base64 の表記も作る)。 */
export function secretsOf(a: Answers): string[] {
  const [keyId, key] = a.storageAdmin.split("\n");
  return [a.llmKey, a.adminKeyPem, a.storageAdmin, keyId, key, a.hookUrl].filter((s): s is string => s !== undefined && s.length >= 8);
}

export function parseEnvFile(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const m = /^([A-Z0-9_]+)=(.*)$/.exec(line.trim());
    if (m !== null) out[m[1]!] = m[2]!;
  }
  return out;
}

const need = (e: Readonly<Record<string, string | undefined>>, k: string): string => {
  const v = e[k];
  if (v === undefined || v === "") throw new Error(`環境変数または接続情報が無い: ${k}`);
  return v;
};

/** B2 の保管先準備アダプタが受ける端点の形(adapters/provision/b2/request.go と同じ)。 */
export const B2_ENDPOINT_RE = /^https:\/\/s3\.[a-z0-9-]+\.backblazeb2\.com$/;

/**
 * real: 人の役の値は SITATE_ROLE_*(launch.md)。fake: LLM の代役の通しなので、鍵は使い捨ての偽の値、保管先は試験用の MinIO(`test/env/.run/<NAME>/env`)。
 * 接続先は受入試験の環境の接続情報(`test/env/.run/accept-<NAME>/env`)から読む。
 */
export function answersFor(mode: "real" | "fake", name: string, repo: string, env: Readonly<Record<string, string | undefined>>, needStorage = true, b2 = false): Answers {
  const acc = parseEnvFile(readFileSync(`${repo}/test/env/.run/accept-${name}/env`, "utf8"));
  const base = {
    adminKeyPem: readFileSync(need(acc, "SITATE_ACCEPT_ADMIN_KEY_FILE"), "utf8"),
    hookUrl: `${need(acc, "SITATE_ACCEPT_HOOK_URL")}/hooks/${HOOK_NAME}`,
    adminUser: need(acc, "SITATE_ACCEPT_ADMIN_USER"),
    backupHost: need(acc, "SITATE_ACCEPT_BACKUP_HOST"), sourceHost: need(acc, "SITATE_ACCEPT_SOURCE_HOST"),
    destHost: need(acc, "SITATE_ACCEPT_DEST_HOST"), destPort: Number(need(acc, "SITATE_ACCEPT_DEST_PORT")),
    hookRecordsFile: need(acc, "SITATE_ACCEPT_HOOK_RECORDS_FILE"),
    containers: { backup: `sitate-acc-${name}-backup`, source: `sitate-acc-${name}-source`, dest: `sitate-acc-${name}-dest` },
  };
  if (mode === "real" || b2) {
    // b2(予行 make test-b2-preflight): LLM は偽の口、保管先だけ実物の B2(SITATE_ROLE_B2_*)。
    // A6 の起動は LLM API キーと環境の名前だけを受ける(launch.md)。保管先の値は A2 だけが要る。
    const st = (k: string): string => (needStorage ? need(env, k) : (env[k] ?? ""));
    if (needStorage && !B2_ENDPOINT_RE.test(st("SITATE_ROLE_B2_ENDPOINT"))) throw new Error("SITATE_ROLE_B2_ENDPOINT は https://s3.<リージョン>.backblazeb2.com の形(https:// から。末尾の / や経路は付けない)");
    const llmKey = b2 && mode === "fake" ? `sk-ant-fake-${Math.random().toString(36).slice(2, 12)}${Math.random().toString(36).slice(2, 12)}` : need(env, "SITATE_ROLE_LLM_KEY");
    return { ...base, llmKey, storageEndpoint: st("SITATE_ROLE_B2_ENDPOINT"), bucket: st("SITATE_ROLE_B2_BUCKET"),
      storageAdmin: `${st("SITATE_ROLE_B2_ADMIN_KEY_ID")}\n${st("SITATE_ROLE_B2_ADMIN_KEY")}` };
  }
  const s3 = parseEnvFile(readFileSync(`${repo}/test/env/.run/${name}/env`, "utf8"));
  const rnd = (): string => Math.random().toString(36).slice(2, 12);
  return { ...base, llmKey: `sk-ant-fake-${rnd()}${rnd()}`, storageEndpoint: need(s3, "SITATE_TEST_S3_ENDPOINT"), bucket: need(s3, "SITATE_TEST_S3_BUCKET"),
    storageAdmin: `fakeid${rnd()}\nfakesecret${rnd()}${rnd()}` };
}

/**
 * 開始時の入力の 2 つ目(手順書の「開始前に用意するもの」の所在と管理アカウント。問いにも人の作業にも数えない)。
 * 1 つ目の指示は「始める」のボタン(spec 3.7 の最初の一歩。D-0169)が送るので、ドライバは固定の文を打たない。
 */
export function locationMessage(a: Answers): string {
  return `バックアップサーバー: ${a.backupHost}(SSH 22 番)、管理アカウント: ${a.adminUser}。\n元サーバー: ${a.sourceHost}(SSH 22 番)、管理アカウント: ${a.adminUser}。管理アカウントの秘密鍵は、秘密の入力ボックスに入れます。`;
}

/** 問いの見分け方(spec 3.8): 応答の最後の文が疑問符で終わるか、規則ファイルの「問いの終わり方」で終わる。 */
export function isQuestion(reply: string, endings: readonly string[]): boolean {
  const text = reply.trim();
  const sentences = text.split(/(?<=[。.!！?？\n])/).map((x) => x.trim()).filter((x) => x !== "");
  const last = (sentences.at(-1) ?? "").replace(/[\s。.]+$/u, "");
  return /[?？]$/u.test(last) || endings.some((e) => last.endsWith(e));
}

/** 台本の Q の語に当たる数が最も多い Q(同数は番号の早い方)。どれにも当たらなければ null(台本に無い問い)。 */
export function matchQuestion(reply: string, s: A2Script): QId | null {
  let best: QId | null = null;
  let bestN = 0;
  for (const q of Q_ORDER) {
    const n = s.questions[q].match.filter((w) => reply.includes(w)).length;
    if (n > bestN) { best = q; bestN = n; }
  }
  return best;
}
