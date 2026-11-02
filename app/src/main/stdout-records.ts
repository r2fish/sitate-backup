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
 * 破棄の原因コード(設計の監督の決定。どの破棄でも見えた秘密はパネルへ渡さず、結果・ログ・会話履歴に残さない):
 * - end が ssh_disconnected: 秘密が見えていても見えていなくても SSH_DISCONNECTED(次の一手は status の確認。秘密が見えていたときの戻り道は仕様に明記が無い)。
 * - ENVELOPE_INVALID は、先頭に秘密のレコードが見えていない出力に限る(封筒でない出力、先頭が封筒で次が秘密、封筒が切れた出力で秘密が無い、など)。
 * - 先頭に秘密のレコードが見えている出力は、封筒が無い・封筒でない([秘密]・[秘密, 秘密]・[秘密, 別の schema]・[秘密, null])・封筒が途中で切れた(end が complete)・
 *   phrase_emitted が true でない、のどれも ENVELOPE_SECRET_MISMATCH。schema が sitate.secret-string/v1 なのに正しく読めない秘密も同じ。
 * - phrase_emitted が true なのに秘密のレコードが無い出力は ENVELOPE_SECRET_MISMATCH。3 レコード以上は ENVELOPE_TOO_MANY_RECORDS。
 * schema が別のレコードは秘密ではなく、先頭なら ENVELOPE_INVALID。破棄の結果は {ok:false, code} だけで、秘密の値を含まない。
 */
export function parseStdoutRecords(stdout: string, end: StreamEnd = "complete"): StdoutRecords {
  void stdout;
  void end;
  throw new Error("NOT_IMPLEMENTED: parseStdoutRecords (U3a。spec 3.1.4)");
}
