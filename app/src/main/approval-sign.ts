// 承認の正規化ハッシュと署名(spec 4.6、3.6)。承認の秘密鍵はこのファイルの鍵の入れ物の中だけにあり、署名の操作しか外へ出さない
// (レンダラ・エージェントのプロセスへ渡す経路を持たない)。署名する値はアプリが `pending --id` の記録から自分で計算した canonical_hash。
import { createHash, createPrivateKey, createPublicKey, generateKeyPairSync, sign as edSign, verify as edVerify } from "node:crypto";
import type { KeyObject } from "node:crypto";
import { jcs } from "./jcs.ts";

export const SIGN_DOMAIN = "sitate-approval/v1";

export type PendingRecord = Readonly<Record<string, unknown>> & { readonly canonical_hash?: unknown };

/** 承認の時刻 signed_at: 秒までの UTC(`YYYY-MM-DDTHH:MM:SSZ`。核の timeRe はミリ秒付きを受けない)。 */
export const signedAtOf = (d: Date): string => d.toISOString().replace(/\.\d+Z$/, "Z");

const VIEW_ONLY = ["canonical_hash", "state", "not_before"] as const;

/** canonical_hash 自身と表示用の欄(state・not_before)を除いた記録の JCS の SHA-256(`sha256:` と 16 進の小文字)。 */
export function canonicalHash(record: PendingRecord): string {
  const rest: Record<string, unknown> = { ...record };
  // `pending` は記録に表示用の state と not_before を足して返す。ハッシュの入力は核が計算した本体だけ。
  for (const k of VIEW_ONLY) delete rest[k];
  return "sha256:" + createHash("sha256").update(jcs(rest), "utf8").digest("hex");
}

/** software 鍵の署名の対象: `"sitate-approval/v1\n" + canonical_hash + "\n" + signed_at`(signed_at は文字列そのまま)。 */
export function signingInput(hash: string, signedAt: string): Buffer {
  return Buffer.from(`${SIGN_DOMAIN}\n${hash}\n${signedAt}`, "utf8");
}

export interface Approval {
  readonly changeId: string;
  readonly canonicalHash: string;
  readonly signedAt: string;
  /** Ed25519 の 64 バイトの署名の base64(標準の文字、`=` の埋めあり)。 */
  readonly signature: string;
}

export class SoftwareApprovalKey {
  readonly #priv: KeyObject;
  readonly publicKeyDer: Buffer;

  private constructor(priv: KeyObject) {
    this.#priv = priv;
    this.publicKeyDer = createPublicKey(priv).export({ type: "spki", format: "der" });
  }

  static generate(): SoftwareApprovalKey {
    return new SoftwareApprovalKey(generateKeyPairSync("ed25519").privateKey);
  }

  /** キーチェーンから取り出した PKCS#8 の DER から復元する(呼び出し側の主プロセスだけ)。 */
  static fromPkcs8(der: Buffer): SoftwareApprovalKey {
    return new SoftwareApprovalKey(createPrivateKey({ key: der, format: "der", type: "pkcs8" }));
  }

  /** `init --approval-pubkey` の値: ssh-ed25519 の公開鍵の wire 形式(種別の文字列と 32 バイトの鍵)の base64。種別の語は付けない(spec 3.1.2、字句規則 5)。 */
  sshPublicKeyBase64(): string {
    const type = Buffer.from("ssh-ed25519");
    const len = (n: number): Buffer => { const b = Buffer.alloc(4); b.writeUInt32BE(n); return b; };
    const raw = this.publicKeyDer.subarray(-32);
    return Buffer.concat([len(type.length), type, len(raw.length), raw]).toString("base64");
  }

  /** キーチェーンへ保存する値(主プロセスだけが呼ぶ)。 */
  exportPkcs8(): Buffer {
    return this.#priv.export({ type: "pkcs8", format: "der" });
  }

  /** 記録から自分で計算したハッシュに署名する。記録の canonical_hash が計算値と違うなら署名しない。 */
  signRecord(record: PendingRecord, signedAt: string): Approval {
    const hash = canonicalHash(record);
    if (typeof record.canonical_hash === "string" && record.canonical_hash !== hash) throw new Error("HASH_MISMATCH");
    const changeId = record.change_id;
    if (typeof changeId !== "string") throw new Error("記録に change_id が無い");
    const signature = edSign(null, signingInput(hash, signedAt), this.#priv).toString("base64");
    return { changeId, canonicalHash: hash, signedAt, signature };
  }

  toJSON(): string {
    return "[承認鍵]";
  }
}

export function verifyApproval(publicKeyDer: Buffer, a: Approval): boolean {
  const pub = createPublicKey({ key: publicKeyDer, format: "der", type: "spki" });
  return edVerify(null, signingInput(a.canonicalHash, a.signedAt), pub, Buffer.from(a.signature, "base64"));
}
