// チャット欄の送信前の秘密の検出(spec 3.7 の表、完了条件 8)。検出したら送信せず、該当した種類だけを返す。
// 値(入力の一部)は返さない。呼び出し側は入力欄を空にし、履歴・ログ・エージェントのプロセスへ渡さない。
import bip39 from "../data/bip39-english.json" with { type: "json" };

export type SecretKind = "keychain_value" | "phrase" | "private_key" | "llm_api_key" | "storage_key" | "webhook_url";

const WORDS: ReadonlySet<string> = new Set(bip39 as string[]);
const PHRASE_MIN_RUN = 12;
const KEYCHAIN_MIN_LEN = 8;

const PRIVATE_KEY = /-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----/;
const LLM_KEY = /sk-ant-[A-Za-z0-9_-]{20,}/;
const AWS_KEY = /(?:AKIA|ASIA)[A-Z0-9]{16}/;
const B2_KEY = /(?<![A-Za-z0-9+/])K[0-9]{3}[A-Za-z0-9+/]{27}(?![A-Za-z0-9+/])/;
const URL_RE = /https:\/\/[^\s"'<>]+/g;
const LONG_TOKEN = /[A-Za-z0-9_-]{20,}/;

function hasPhrase(text: string): boolean {
  // 区切り: 空白・改行・読点(、。，,)・番号(1. 2) など。語以外のものが挟まれば連続が切れる。
  const tokens = text.toLowerCase().split(/[\s、。,，;；:：]+|\d+[.)．）]?/).filter((x) => x !== "");
  let run = 0;
  for (const tok of tokens) {
    run = WORDS.has(tok) ? run + 1 : 0;
    if (run >= PHRASE_MIN_RUN) return true;
  }
  return false;
}

function hasWebhook(text: string): boolean {
  for (const m of text.matchAll(URL_RE)) {
    let u: URL;
    try { u = new URL(m[0]); } catch { continue; }
    if (LONG_TOKEN.test(u.pathname + u.search)) return true;
  }
  return false;
}

/** 検出した種類(複数可)。キーチェーンの値は呼び出し側が渡す(8 文字以上のものだけ見る)。 */
export function detectSecrets(input: string, keychainValues: readonly string[] = []): SecretKind[] {
  const found: SecretKind[] = [];
  if (keychainValues.some((v) => v.length >= KEYCHAIN_MIN_LEN && input.includes(v))) found.push("keychain_value");
  if (hasPhrase(input)) found.push("phrase");
  if (PRIVATE_KEY.test(input)) found.push("private_key");
  if (LLM_KEY.test(input)) found.push("llm_api_key");
  if (AWS_KEY.test(input) || B2_KEY.test(input)) found.push("storage_key");
  if (hasWebhook(input)) found.push("webhook_url");
  return found;
}

export interface SendDecision {
  readonly send: boolean;
  /** 検出した種類のカタログのキー(検出の固定文、入力ボックスの案内、失効・再発行の案内)。送るときは空。 */
  readonly noticeKeys: readonly string[];
}

const REVOKE_KEY: Record<SecretKind, string> = {
  keychain_value: "chat.secret.revoke_reissue.notify_credential",
  phrase: "chat.secret.revoke_reissue.phrase",
  private_key: "chat.secret.revoke_reissue.admin_key",
  llm_api_key: "chat.secret.revoke_reissue.llm_api_key",
  storage_key: "chat.secret.revoke_reissue.storage_key",
  webhook_url: "chat.secret.revoke_reissue.notify_credential",
};

export function guardOutgoing(input: string, keychainValues: readonly string[] = []): SendDecision {
  const kinds = detectSecrets(input, keychainValues);
  if (kinds.length === 0) return { send: true, noticeKeys: [] };
  const keys = kinds.flatMap((k) => [`chat.secret.detected.${k}`, REVOKE_KEY[k]]);
  return { send: false, noticeKeys: [...keys, "chat.secret.input_box_guide"] };
}
