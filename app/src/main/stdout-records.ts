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
  void stdout;
  void end;
  throw new Error("NOT_IMPLEMENTED: parseStdoutRecords (U3a。spec 3.1.4)");
}
