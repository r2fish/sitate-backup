// 標準出力の 2 レコードの読み取り(spec 3.1.4。作業指示書 5.14 の完了条件 6)。関数の名前と型は U0-core が固定し、中身は WP U3a。
// 骨格は NOT_IMPLEMENTED を投げる。app/test/contract/vectors.test.ts が internal/contract/testdata/cli/stdout-records.json を通して照らす。

/** 先頭のレコード(秘密)。アプリは schema だけで見分け、フレーズのパネルへ直接渡してメモリから消す。 */
export interface SecretStringRecord {
  readonly schema: "sitate.secret-string/v1";
  readonly kind: string;
  readonly words: readonly string[];
  readonly keyring_version: number;
}

/** 封筒(sitate.out/v1)。モデルの入力と会話履歴に渡してよいのはこちらだけ。 */
export interface OutEnvelope {
  readonly schema: "sitate.out/v1";
  readonly phrase_emitted?: boolean;
  readonly [field: string]: unknown;
}

/** アプリが破棄して失敗として扱うときの原因コード(spec 3.1.4、3.7 のアプリの原因コード)。 */
export type StdoutDiscardCode = "ENVELOPE_INVALID" | "ENVELOPE_SECRET_MISMATCH" | "ENVELOPE_TOO_MANY_RECORDS" | "SSH_DISCONNECTED";

/** 出力の終わり方(呼び出し側が接続の状態から決める)。complete: 接続は正常に終わった。ssh_disconnected: 出力の途中で SSH が切れた。 */
export type StreamEnd = "complete" | "ssh_disconnected";

export type StdoutRecords =
  | { readonly ok: true; readonly secret: SecretStringRecord | null; readonly envelope: OutEnvelope }
  | { readonly ok: false; readonly code: StdoutDiscardCode };

/**
 * 改行区切りの標準出力を読む。1 レコード(封筒)か、2 レコード(先頭が秘密、次が封筒)を受ける。end は出力の終わり方(既定 complete)。
 * 破棄の原因コードは spec 3.1.4 の判定の順で、最初に当たったものとする(どの破棄でも見えた秘密はパネルへ渡さず、結果・ログ・会話履歴に残さない):
 * 1. end が ssh_disconnected: 秘密が見えていても見えていなくても SSH_DISCONNECTED(秘密が見えていたときの戻り道は ENVELOPE_SECRET_MISMATCH と同じ)。
 * 2. 3 レコード以上: ENVELOPE_TOO_MANY_RECORDS。
 * 3. どれかのレコードの schema が sitate.secret-string/v1 で、正しい形の [秘密, 封筒](封筒の phrase_emitted が true)でない出力(秘密だけ、[封筒, 秘密]、[秘密, 秘密]、[秘密, 別の schema]、
 *    形の不正な秘密、封筒が途中で切れた出力)か、phrase_emitted が true で秘密のレコードが無い出力: ENVELOPE_SECRET_MISMATCH。
 * 4. それ以外で封筒 1 レコードの形でない出力([封筒, 別の schema] を含む): ENVELOPE_INVALID。2 番目のレコードをモデルへ渡さない。
 * 破棄の結果は {ok:false, code} だけで、秘密の値を含まない。
 */
export function parseStdoutRecords(stdout: string, end: StreamEnd = "complete"): StdoutRecords {
  const discard = (code: StdoutDiscardCode): StdoutRecords => ({ ok: false, code });
  if (end === "ssh_disconnected") return discard("SSH_DISCONNECTED");
  const records = splitRecords(stdout);
  if (records.length >= 3) return discard("ENVELOPE_TOO_MANY_RECORDS");
  const secretSchema = records.some((r) => r.schema === SECRET_SCHEMA);
  if (secretSchema) {
    const [first, second] = records;
    const shaped = records.length === 2 && first!.schema === SECRET_SCHEMA && isSecretRecord(first!.value) && isEnvelope(second!.value) && second!.value.phrase_emitted === true;
    return shaped ? { ok: true, secret: first!.value as SecretStringRecord, envelope: second!.value as OutEnvelope } : discard("ENVELOPE_SECRET_MISMATCH");
  }
  if (records.some((r) => isEnvelope(r.value) && r.value.phrase_emitted === true)) return discard("ENVELOPE_SECRET_MISMATCH");
  const only = records.length === 1 ? records[0]! : null;
  if (only !== null && isEnvelope(only.value)) return { ok: true, secret: null, envelope: only.value as OutEnvelope };
  return discard("ENVELOPE_INVALID");
}

const SECRET_SCHEMA = "sitate.secret-string/v1";
const ENVELOPE_SCHEMA = "sitate.out/v1";
const WORD_PATTERN = /^[a-z]{3,8}$/;
const PHRASE_WORDS = 24;
/** 解析できない行でも、秘密の schema の印が見えていれば秘密のレコードとして扱う(途中で切れた秘密を、秘密を含む出力として破棄するため)。 */
const SECRET_SCHEMA_MARK = /"schema"\s*:\s*"sitate\.secret-string\/v1"/;

interface RawRecord {
  /** 解析できた JSON の値。解析できなければ undefined。 */
  readonly value: unknown;
  readonly schema: string | null;
}

function splitRecords(stdout: string): RawRecord[] {
  const out: RawRecord[] = [];
  for (const line of stdout.split("\n")) {
    if (line.trim() === "") continue;
    try {
      const value: unknown = JSON.parse(line);
      out.push({ value, schema: isObject(value) && typeof value.schema === "string" ? value.schema : null });
    } catch {
      out.push({ value: undefined, schema: SECRET_SCHEMA_MARK.test(line) ? SECRET_SCHEMA : null });
    }
  }
  return out;
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function isEnvelope(v: unknown): v is Record<string, unknown> & { phrase_emitted?: unknown } {
  return isObject(v) && v.schema === ENVELOPE_SCHEMA && Array.isArray(v.command) && typeof v.ok === "boolean";
}

function isSecretRecord(v: unknown): boolean {
  if (!isObject(v)) return false;
  const keys = Object.keys(v).sort().join(",");
  if (keys !== "keyring_version,kind,schema,words") return false;
  const words = v.words;
  return v.kind === "recovery_phrase" && Number.isInteger(v.keyring_version) && (v.keyring_version as number) >= 1
    && Array.isArray(words) && words.length === PHRASE_WORDS && words.every((w) => typeof w === "string" && WORD_PATTERN.test(w));
}
