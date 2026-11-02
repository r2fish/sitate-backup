// ホスト鍵の指紋(spec 3.7 の管理用 SSH の方式)。固定し照らすのはサーバーの Ed25519 のホスト鍵だけで、形式は OpenSSH の `SHA256:<base64>`(末尾の = を除く)。
import { createHash } from "node:crypto";

const ED25519 = "ssh-ed25519";
const FP_RE = /^SHA256:[A-Za-z0-9+/]{43}$/;

/** SSH の公開鍵の blob(string 型名 + 鍵本体)が Ed25519 か。 */
export function isEd25519Blob(blob: Buffer): boolean {
  if (blob.length < 4 + ED25519.length) return false;
  const n = blob.readUInt32BE(0);
  return n === ED25519.length && blob.subarray(4, 4 + n).toString("latin1") === ED25519;
}

export function fingerprintOf(blob: Buffer): string {
  return "SHA256:" + createHash("sha256").update(blob).digest("base64").replace(/=+$/, "");
}

/** 人が入れる指紋(復旧キットの印字)の形。前後の空白だけ許す。 */
export function normalizeFingerprint(input: string): string | null {
  const t = input.trim();
  return FP_RE.test(t) ? t : null;
}

export type PinDecision = { readonly ok: true; readonly pin: boolean } | { readonly ok: false };

/**
 * 接続で見えたホスト鍵の判定。
 * - 固定値がある: 一致だけ通す(固定値は上書きしない)。
 * - 固定値が無く人が入れた指紋がある: 一致したときだけ固定する。
 * - どちらも無い: 初回接続の固定を許すと明示された呼び出し(最初の bootstrap・bootstrap-restore)だけ固定する。許さない呼び出しは拒否。
 * Ed25519 以外のホスト鍵は常に拒否する。
 */
export function decideHostKey(blob: Buffer, pinned: string | null, typedByHuman: string | null, allowFirstUse: boolean): PinDecision {
  if (!isEd25519Blob(blob)) return { ok: false };
  const fp = fingerprintOf(blob);
  if (pinned !== null) return fp === pinned ? { ok: true, pin: false } : { ok: false };
  if (typedByHuman !== null) return fp === typedByHuman ? { ok: true, pin: true } : { ok: false };
  return allowFirstUse ? { ok: true, pin: true } : { ok: false };
}
